//go:build linux

package apps

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	proxyservice "github.com/Oswald-Hao/Osverse/internal/proxy"
)

func TestBuiltInCatalogIsPinnedAndComplete(t *testing.T) {
	catalog, err := builtInCatalog()
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"opencode-desktop", "cc-switch", "cockpit-tools"} {
		item, ok := catalog[id]
		if !ok || item.DownloadBytes <= 80_000_000 || len(item.SHA256) != 64 || item.Architecture != "amd64" {
			t.Fatalf("catalog[%q] = %#v", id, item)
		}
	}
}

func TestPlanIsSingleUseAndListsOnlyUserPaths(t *testing.T) {
	home := t.TempDir()
	manager := testManager(home, artifact{ID: "cc-switch", Name: "CC Switch", Command: "cc-switch", DesktopFile: "cc-switch.desktop", Version: "1.2.3", Architecture: runtime.GOARCH})
	plan, err := manager.CreatePlan(context.Background(), "cc-switch")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(plan.ID, "app-") || len(plan.Changes) != 5 {
		t.Fatalf("plan = %#v", plan)
	}
	for _, change := range plan.Changes {
		if change.Kind != "download" && !within(home, change.Path) {
			t.Fatalf("change escaped home: %#v", change)
		}
	}
	if _, err := manager.consumePlan(plan.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.consumePlan(plan.ID); err != ErrPlanUnavailable {
		t.Fatalf("second consume = %v", err)
	}
}

func TestExecuteInstallsVerifiedImageAndPreservesExternalEntries(t *testing.T) {
	payload := append([]byte("\x7fELF"), []byte(strings.Repeat("safe", 128))...)
	hash := sha256.Sum256(payload)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Length", strings.TrimSpace(strings.Repeat("", 0))+itoa(len(payload)))
		_, _ = writer.Write(payload)
	}))
	defer server.Close()
	home := t.TempDir()
	item := artifact{
		ID: "cc-switch", Name: "CC Switch", Command: "cc-switch", DesktopFile: "cc-switch.desktop",
		Version: "1.2.3", Architecture: runtime.GOARCH, URL: server.URL, SHA256: hex.EncodeToString(hash[:]), DownloadBytes: int64(len(payload)),
	}
	manager := testManager(home, item)
	manager.client = func(proxyservice.Protocol, int) (*http.Client, error) { return server.Client(), nil }
	if err := manager.execute(context.Background(), item, "", 0, func(progressUpdate) {}); err != nil {
		t.Fatal(err)
	}
	image := filepath.Join(home, ".local/share/osverse/apps/cc-switch/1.2.3/application.AppImage")
	content, err := os.ReadFile(image)
	if err != nil || string(content) != string(payload) {
		t.Fatalf("installed image = %d bytes, %v", len(content), err)
	}
	info, _ := os.Stat(image)
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("mode = %o", info.Mode().Perm())
	}
	launcher, err := filepath.EvalSymlinks(filepath.Join(home, ".local/bin/cc-switch"))
	if err != nil || launcher != image {
		t.Fatalf("launcher = %q, %v", launcher, err)
	}
	desktop, _ := os.ReadFile(filepath.Join(home, ".local/share/applications/cc-switch.desktop"))
	if !strings.Contains(string(desktop), "X-Osverse-Managed=true") {
		t.Fatalf("desktop = %q", desktop)
	}

	externalHome := t.TempDir()
	external := filepath.Join(externalHome, ".local/bin/cc-switch")
	if err := os.MkdirAll(filepath.Dir(external), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(external, []byte("external"), 0o755); err != nil {
		t.Fatal(err)
	}
	externalManager := testManager(externalHome, item)
	externalManager.client = manager.client
	if err := externalManager.execute(context.Background(), item, "", 0, func(progressUpdate) {}); err != ErrExternalEntry {
		t.Fatalf("collision = %v", err)
	}
	unchanged, _ := os.ReadFile(external)
	if string(unchanged) != "external" {
		t.Fatalf("external entry changed: %q", unchanged)
	}
}

func TestDownloadCancellationCannotCommit(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		close(started)
		writer.Header().Set("Content-Length", "100000")
		_, _ = io.CopyN(writer, strings.NewReader(strings.Repeat("x", 100000)), 100000)
	}))
	defer server.Close()
	home := t.TempDir()
	item := artifact{ID: "cc-switch", Name: "CC Switch", Command: "cc-switch", DesktopFile: "cc-switch.desktop", Version: "1.2.3", Architecture: runtime.GOARCH, URL: server.URL, SHA256: strings.Repeat("0", 64), DownloadBytes: 100000}
	manager := testManager(home, item)
	manager.client = func(proxyservice.Protocol, int) (*http.Client, error) { return server.Client(), nil }
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := manager.execute(ctx, item, "", 0, func(progressUpdate) {}); err != context.Canceled {
		t.Fatalf("execute = %v", err)
	}
	select {
	case <-started:
		t.Fatal("canceled install reached network")
	default:
	}
	if _, err := os.Lstat(filepath.Join(home, ".local/bin/cc-switch")); !os.IsNotExist(err) {
		t.Fatalf("launcher exists: %v", err)
	}
}

func TestDesktopDownloadAllowsOnlyOfficialGitHubHandoff(t *testing.T) {
	body := []byte("verified desktop artifact")
	digest := sha256.Sum256(body)
	item := artifact{
		ID: "cc-switch", URL: "https://github.com/owner/repo/releases/download/v1/app.AppImage",
		SHA256: hex.EncodeToString(digest[:]), DownloadBytes: int64(len(body)),
	}
	manager := testManager(t.TempDir(), item)
	manager.client = func(proxyservice.Protocol, int) (*http.Client, error) {
		return &http.Client{Transport: desktopRoundTrip(func(request *http.Request) (*http.Response, error) {
			if request.URL.Host == "github.com" {
				return &http.Response{StatusCode: http.StatusFound, Body: io.NopCloser(strings.NewReader("")), Header: http.Header{"Location": []string{"https://release-assets.githubusercontent.com/github-production-release-asset/1/app?sig=fixed"}}, Request: request}, nil
			}
			return &http.Response{StatusCode: http.StatusOK, ContentLength: int64(len(body)), Body: io.NopCloser(bytes.NewReader(body)), Header: make(http.Header), Request: request}, nil
		})}, nil
	}
	if err := manager.download(context.Background(), item, "", 0, filepath.Join(t.TempDir(), "artifact"), func(progressUpdate) {}); err != nil {
		t.Fatalf("official handoff failed: %v", err)
	}
	manager.client = func(proxyservice.Protocol, int) (*http.Client, error) {
		return &http.Client{Transport: desktopRoundTrip(func(request *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusFound, Body: io.NopCloser(strings.NewReader("")), Header: http.Header{"Location": []string{"https://example.invalid/escape"}}, Request: request}, nil
		})}, nil
	}
	if err := manager.download(context.Background(), item, "", 0, filepath.Join(t.TempDir(), "artifact"), func(progressUpdate) {}); !errors.Is(err, errDownload) {
		t.Fatalf("untrusted redirect error = %v", err)
	}
}

type fakeLauncher struct {
	path  string
	calls atomic.Int32
}

func (launcher *fakeLauncher) Start(path string) error {
	launcher.path = path
	launcher.calls.Add(1)
	return nil
}

func TestLaunchAcceptsOnlyManagedCurrentImage(t *testing.T) {
	home := t.TempDir()
	imageContent := []byte("\x7fELF")
	imageHash := sha256.Sum256(imageContent)
	item := artifact{ID: "cc-switch", Name: "CC Switch", Command: "cc-switch", DesktopFile: "cc-switch.desktop", Version: "1.2.3", Architecture: runtime.GOARCH, SHA256: hex.EncodeToString(imageHash[:])}
	manager := testManager(home, item)
	launcher := &fakeLauncher{}
	manager.launcher = launcher
	versionDir := filepath.Join(home, ".local/share/osverse/apps/cc-switch/1.2.3")
	if err := os.MkdirAll(versionDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(versionDir, "application.AppImage"), imageContent, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(versionDir, ".osverse-artifact-sha256"), []byte(item.SHA256+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("1.2.3", filepath.Join(filepath.Dir(versionDir), "current")); err != nil {
		t.Fatal(err)
	}
	if err := manager.Launch("cc-switch"); err != nil {
		t.Fatal(err)
	}
	if launcher.calls.Load() != 1 || launcher.path != filepath.Join(versionDir, "application.AppImage") {
		t.Fatalf("launch = %d %q", launcher.calls.Load(), launcher.path)
	}
	if err := os.WriteFile(filepath.Join(versionDir, "application.AppImage"), []byte("tampered"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := manager.Launch("cc-switch"); err == nil || launcher.calls.Load() != 1 {
		t.Fatalf("tampered launch = %v, calls = %d", err, launcher.calls.Load())
	}
	if err := manager.Launch("unknown"); err != ErrUnknownComponent {
		t.Fatalf("unknown = %v", err)
	}
}

func testManager(home string, item artifact) *Manager {
	sequence := 0
	return newManager(home, runtime.GOARCH, map[string]artifact{item.ID: item}, func() time.Time { return time.Unix(1000, 0) }, func() (string, error) { sequence++; return "app-test-" + itoa(sequence), nil })
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	var data [20]byte
	index := len(data)
	for value > 0 {
		index--
		data[index] = byte('0' + value%10)
		value /= 10
	}
	return string(data[index:])
}

type desktopRoundTrip func(*http.Request) (*http.Response, error)

func (roundTrip desktopRoundTrip) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTrip(request)
}
