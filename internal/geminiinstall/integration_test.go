package geminiinstall

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	proxyservice "github.com/Oswald-Hao/Osverse/internal/proxy"
)

func TestTransactionalInstallRunsManagedGeminiCommand(t *testing.T) {
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runtimeBytes, runtimeItem := integrationRuntimeArchive(t)
	packageBytes := integrationPackageArchive(t)
	packHash := sha256.Sum256(packageBytes)
	pack := packageArtifact{
		URL:    "https://registry.npmjs.org/@google/gemini-cli/-/gemini-cli-0.57.0.tgz",
		SHA256: hex.EncodeToString(packHash[:]), Size: int64(len(packageBytes)),
	}
	manager := &Manager{home: home, goos: runtimeItem.GOOS, goarch: runtimeItem.GOARCH}
	manager.client = func(_ proxyservice.Protocol, _ int) (*http.Client, error) {
		return &http.Client{Transport: integrationTransport(func(request *http.Request) (*http.Response, error) {
			var body []byte
			switch request.URL.String() {
			case runtimeItem.URL:
				body = runtimeBytes
			case pack.URL:
				body = packageBytes
			default:
				t.Fatalf("unexpected download URL %s", request.URL)
			}
			return &http.Response{StatusCode: http.StatusOK, ContentLength: int64(len(body)), Body: io.NopCloser(bytes.NewReader(body)), Header: make(http.Header)}, nil
		})}, nil
	}
	manager.executeFn = manager.execute
	var lastProgress int
	err = manager.execute(context.Background(), storedPlan{runtime: runtimeItem, pack: pack}, "", 0, func(phase string, progress int, _ string) {
		if !strings.Contains("downloading verifying committing", phase) || progress < lastProgress {
			t.Fatalf("invalid progress update %q %d after %d", phase, progress, lastProgress)
		}
		lastProgress = progress
	})
	if err != nil {
		t.Fatal(err)
	}
	if lastProgress != 99 {
		t.Fatalf("last progress = %d", lastProgress)
	}
	commandPath, output, err := runInstalledCommand(home)
	if err != nil || strings.TrimSpace(string(output)) != geminiVersion {
		t.Fatalf("run %s = (%q, %v)", commandPath, output, err)
	}
	paths := managedPathsFor(home, runtimeItem.GOOS, geminiVersion)
	if marker, err := os.ReadFile(filepath.Join(paths.finalRoot, ".osverse-gemini-runtime")); err != nil || !bytes.Contains(marker, []byte("component=gemini-cli\nversion=0.57.0\n")) {
		t.Fatalf("runtime marker = %q, %v", marker, err)
	}
}

func integrationPackageArchive(t *testing.T) []byte {
	t.Helper()
	archive := filepath.Join(t.TempDir(), "gemini.tgz")
	writeTarGz(t, archive, map[string][]byte{
		"package/package.json":     []byte(`{"name":"@google/gemini-cli","version":"0.57.0","license":"Apache-2.0","bin":{"gemini":"bundle/gemini.js"},"engines":{"node":">=20"}}`),
		"package/bundle/gemini.js": []byte("integration fixture"),
		"package/LICENSE":          []byte("Apache-2.0 integration fixture"),
	})
	raw, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

type integrationTransport func(*http.Request) (*http.Response, error)

func (transport integrationTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}
