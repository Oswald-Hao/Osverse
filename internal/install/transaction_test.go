//go:build linux

package install

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Oswald-Hao/Osverse/internal/platform"
	proxyservice "github.com/Oswald-Hao/Osverse/internal/proxy"
)

func TestTransactionInstallsVerifiedVersionAndManagedLinks(t *testing.T) {
	archive := testArchive(t, []tarEntry{
		{name: "package/", kind: tar.TypeDir},
		{name: "package/bin/", kind: tar.TypeDir},
		{name: "package/bin/tool", body: []byte("verified binary")},
		{name: "package/resources/data", body: []byte("resource")},
	})
	manager, item := transactionManager(t, archive)
	plan, err := manager.CreatePlan(context.Background(), item.ID)
	if err != nil {
		t.Fatal(err)
	}

	task, err := manager.Start(context.Background(), plan.ID, proxyservice.ProtocolHTTPSConnect, 7890)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	finished := awaitTask(t, manager, task.ID)
	if finished.Phase != "completed" || finished.Progress != 100 || finished.ErrorCode != "" {
		t.Fatalf("finished task = %#v", finished)
	}
	toolRoot := filepath.Join(manager.home, ".local", "share", "osverse", "tools", item.ID)
	current, err := os.Readlink(filepath.Join(toolRoot, "current"))
	if err != nil || current != item.Version {
		t.Fatalf("current = (%q, %v)", current, err)
	}
	shim := filepath.Join(manager.home, ".local", "bin", item.Command)
	shimTarget, err := os.Readlink(shim)
	if err != nil || shimTarget != filepath.Join(toolRoot, "current", filepath.FromSlash(item.BinaryPath)) {
		t.Fatalf("shim = (%q, %v)", shimTarget, err)
	}
	binary := filepath.Join(toolRoot, item.Version, filepath.FromSlash(item.BinaryPath))
	info, err := os.Stat(binary)
	if err != nil || info.Mode()&0o100 == 0 {
		t.Fatalf("installed binary = (%v, %v)", info, err)
	}
	if _, err := os.Stat(filepath.Join(toolRoot, item.Version, "package", "resources", "data")); err != nil {
		t.Fatalf("resource missing: %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(manager.home, ".local", "share", "osverse", "staging"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("staging entries = (%v, %v)", entries, err)
	}
}

func TestTransactionRejectsExternalCommandBeforeDownload(t *testing.T) {
	archive := testArchive(t, []tarEntry{{name: "package/bin/tool", body: []byte("binary")}})
	manager, item := transactionManager(t, archive)
	bin := filepath.Join(manager.home, ".local", "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	external := filepath.Join(bin, item.Command)
	if err := os.WriteFile(external, []byte("external"), 0o755); err != nil {
		t.Fatal(err)
	}
	var downloaded atomic.Bool
	manager.client = func(proxyservice.Protocol, int) (*http.Client, error) {
		downloaded.Store(true)
		return testHTTPClient(archive), nil
	}
	plan, _ := manager.CreatePlan(context.Background(), item.ID)
	task, err := manager.Start(context.Background(), plan.ID, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	finished := awaitTask(t, manager, task.ID)
	if finished.Phase != "failed" || !bytes.Contains([]byte(finished.Message), []byte("占用")) {
		t.Fatalf("task = %#v", finished)
	}
	if downloaded.Load() {
		t.Fatal("download started before external-command preflight")
	}
	content, _ := os.ReadFile(external)
	if string(content) != "external" {
		t.Fatalf("external command changed to %q", content)
	}
	info, _ := os.Stat(bin)
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("existing bin mode changed to %o", info.Mode().Perm())
	}
}

func TestTransactionRejectsHashMismatchAndUnsafeArchive(t *testing.T) {
	tests := []struct {
		name    string
		entries []tarEntry
		mutate  func(*artifact)
	}{
		{name: "hash mismatch", entries: []tarEntry{{name: "package/bin/tool", body: []byte("binary")}}, mutate: func(item *artifact) { item.SHA256 = hex.EncodeToString(make([]byte, sha256.Size)) }},
		{name: "traversal", entries: []tarEntry{{name: "package/bin/tool", body: []byte("binary")}, {name: "package/../../escaped", body: []byte("escape")}}},
		{name: "symlink", entries: []tarEntry{{name: "package/bin/tool", body: []byte("binary")}, {name: "package/link", kind: tar.TypeSymlink, link: "/etc/passwd"}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			archive := testArchive(t, test.entries)
			manager, item := transactionManager(t, archive)
			if test.mutate != nil {
				test.mutate(&item)
				manager.catalog[item.ID] = item
			}
			plan, _ := manager.CreatePlan(context.Background(), item.ID)
			task, err := manager.Start(context.Background(), plan.ID, "", 0)
			if err != nil {
				t.Fatal(err)
			}
			finished := awaitTask(t, manager, task.ID)
			if finished.Phase != "failed" {
				t.Fatalf("task = %#v", finished)
			}
			toolRoot := filepath.Join(manager.home, ".local", "share", "osverse", "tools", item.ID)
			if _, err := os.Lstat(filepath.Join(toolRoot, "current")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("current exists after failure: %v", err)
			}
			if _, err := os.Stat(filepath.Join(manager.home, "escaped")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("archive escaped: %v", err)
			}
		})
	}
}

func TestTransactionRestoresCurrentWhenShimCommitFails(t *testing.T) {
	archive := testArchive(t, []tarEntry{{name: "package/bin/tool", body: []byte("binary")}})
	manager, item := transactionManager(t, archive)
	toolRoot := filepath.Join(manager.home, ".local", "share", "osverse", "tools", item.ID)
	if err := os.MkdirAll(filepath.Join(toolRoot, "old"), 0o700); err != nil {
		t.Fatal(err)
	}
	currentPath := filepath.Join(toolRoot, "current")
	if err := os.Symlink("old", currentPath); err != nil {
		t.Fatal(err)
	}
	binRoot := filepath.Join(manager.home, ".local", "bin")
	if err := os.MkdirAll(binRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	shimPath := filepath.Join(binRoot, item.Command)
	oldShim := filepath.Join(toolRoot, "old", "package", "bin", "tool")
	if err := os.Symlink(oldShim, shimPath); err != nil {
		t.Fatal(err)
	}
	var calls int
	manager.replaceLink = func(linkPath, target string) error {
		calls++
		if calls == 2 {
			return errors.New("injected shim failure")
		}
		return replaceSymlink(linkPath, target)
	}
	plan, _ := manager.CreatePlan(context.Background(), item.ID)
	task, _ := manager.Start(context.Background(), plan.ID, "", 0)
	finished := awaitTask(t, manager, task.ID)
	if finished.Phase != "failed" {
		t.Fatalf("task = %#v", finished)
	}
	current, _ := os.Readlink(currentPath)
	shim, _ := os.Readlink(shimPath)
	if current != "old" || shim != oldShim {
		t.Fatalf("rollback = current %q, shim %q", current, shim)
	}
}

func TestCancelDuringVersionVerificationLeavesLinksUnchanged(t *testing.T) {
	archive := testArchive(t, []tarEntry{{name: "package/bin/tool", body: []byte("binary")}})
	manager, item := transactionManager(t, archive)
	started := make(chan struct{})
	manager.runner = fakeInstallRunner{run: func(ctx context.Context, _ platform.CommandRequest) (platform.CommandResult, error) {
		close(started)
		<-ctx.Done()
		return platform.CommandResult{}, ctx.Err()
	}}
	plan, _ := manager.CreatePlan(context.Background(), item.ID)
	task, _ := manager.Start(context.Background(), plan.ID, "", 0)
	<-started
	if err := manager.Cancel(task.ID); err != nil {
		t.Fatal(err)
	}
	finished := awaitTask(t, manager, task.ID)
	if finished.Phase != "canceled" || finished.ErrorCode != "INSTALL_CANCELED" {
		t.Fatalf("task = %#v", finished)
	}
	toolRoot := filepath.Join(manager.home, ".local", "share", "osverse", "tools", item.ID)
	if _, err := os.Lstat(filepath.Join(toolRoot, "current")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("current exists after cancellation: %v", err)
	}
}

func TestProfileFailureRollsBackLinksAndEarlierProfile(t *testing.T) {
	archive := testArchive(t, []tarEntry{{name: "package/bin/tool", body: []byte("binary")}})
	manager, item := transactionManager(t, archive)
	good := filepath.Join(manager.home, ".profile")
	bad := filepath.Join(manager.home, ".bashrc")
	if err := os.WriteFile(good, []byte("export EDITOR=vim\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(manager.home, "other")
	if err := os.WriteFile(target, []byte("other"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, bad); err != nil {
		t.Fatal(err)
	}
	manager.profiles = []string{good, bad}
	plan, _ := manager.CreatePlan(context.Background(), item.ID)
	task, _ := manager.Start(context.Background(), plan.ID, "", 0)
	finished := awaitTask(t, manager, task.ID)
	if finished.Phase != "failed" {
		t.Fatalf("task = %#v", finished)
	}
	content, _ := os.ReadFile(good)
	if string(content) != "export EDITOR=vim\n" {
		t.Fatalf("earlier profile not restored: %q", content)
	}
	toolRoot := filepath.Join(manager.home, ".local", "share", "osverse", "tools", item.ID)
	if _, err := os.Lstat(filepath.Join(toolRoot, "current")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("current remains after profile rollback: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(manager.home, ".local", "bin", item.Command)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("shim remains after profile rollback: %v", err)
	}
}

type tarEntry struct {
	name string
	body []byte
	kind byte
	link string
}

func testArchive(t *testing.T, entries []tarEntry) []byte {
	t.Helper()
	var output bytes.Buffer
	gzipWriter := gzip.NewWriter(&output)
	tarWriter := tar.NewWriter(gzipWriter)
	for _, entry := range entries {
		kind := entry.kind
		if kind == 0 {
			kind = tar.TypeReg
		}
		size := int64(len(entry.body))
		if kind != tar.TypeReg {
			size = 0
		}
		header := &tar.Header{Name: entry.name, Typeflag: kind, Size: size, Mode: 0o777, Linkname: entry.link}
		if err := tarWriter.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if size > 0 {
			if _, err := tarWriter.Write(entry.body); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func transactionManager(t *testing.T, archive []byte) (*Manager, artifact) {
	t.Helper()
	hash := sha256.Sum256(archive)
	item := artifact{
		ID: "test-cli", Name: "Test CLI", Command: "testcli", Version: "1.2.3", Architecture: "amd64",
		URL: "https://registry.npmjs.org/test-cli/-/test-cli-1.2.3.tgz", SHA256: hex.EncodeToString(hash[:]),
		DownloadBytes: int64(len(archive)), ExpandedBytesLimit: 1024 * 1024,
		BinaryPath: "package/bin/tool", VersionArgs: []string{"--version"},
	}
	var next atomic.Uint64
	manager, err := newManager(t.TempDir(), "amd64", map[string]artifact{item.ID: item}, time.Now, func() (string, error) {
		return "id-" + strconv.FormatUint(next.Add(1), 10), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	manager.runner = fakeInstallRunner{run: func(_ context.Context, request platform.CommandRequest) (platform.CommandResult, error) {
		if request.Path == "" || len(request.Args) != 1 || request.Args[0] != "--version" {
			t.Errorf("version request = %#v", request)
		}
		return platform.CommandResult{ExitCode: 0, Stdout: "testcli 1.2.3"}, nil
	}}
	manager.client = func(protocol proxyservice.Protocol, port int) (*http.Client, error) {
		if protocol != "" && (protocol != proxyservice.ProtocolHTTPSConnect || port != 7890) {
			t.Errorf("network = (%q, %d)", protocol, port)
		}
		return testHTTPClient(archive), nil
	}
	return manager, item
}

func testHTTPClient(body []byte) *http.Client {
	return &http.Client{Transport: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode:    http.StatusOK,
			Header:        http.Header{"Content-Type": []string{"application/octet-stream"}},
			Body:          io.NopCloser(bytes.NewReader(body)),
			ContentLength: int64(len(body)),
			Request:       request,
		}, nil
	})}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (function roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

type fakeInstallRunner struct {
	run func(context.Context, platform.CommandRequest) (platform.CommandResult, error)
}

func (runner fakeInstallRunner) Run(ctx context.Context, request platform.CommandRequest) (platform.CommandResult, error) {
	return runner.run(ctx, request)
}

func awaitTask(t *testing.T, manager *Manager, id string) Task {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		task, err := manager.Task(id)
		if err != nil {
			t.Fatal(err)
		}
		if terminalPhase(task.Phase) {
			return task
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("task did not reach terminal state")
	return Task{}
}
