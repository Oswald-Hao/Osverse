package harnessinstall

import (
	"context"
	"encoding/hex"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	proxyservice "github.com/Oswald-Hao/Osverse/internal/proxy"
)

// This opt-in test exercises every embedded npm package against npm's
// content-addressed cache and a checksum-verified Node archive. CI keeps the
// hermetic unit suite; release validation enables this test explicitly.
func TestBuildRealLinuxPayloadFromVerifiedCache(t *testing.T) {
	nodeArchive := os.Getenv("OSVERSE_HARNESS_NODE_ARCHIVE")
	if nodeArchive == "" {
		t.Skip("set OSVERSE_HARNESS_NODE_ARCHIVE to the pinned Node archive")
	}
	lock, err := builtInLock()
	if err != nil {
		t.Fatal(err)
	}
	byURL := make(map[string]lockedPackage, len(lock.Packages))
	for _, item := range lock.Packages {
		byURL[item.URL] = item
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		path := nodeArchive
		if request.URL.Host == registryHost {
			item := byURL[request.URL.String()]
			digest := hex.EncodeToString(item.Integrity)
			path = filepath.Join(home, ".npm", "_cacache", "content-v2", "sha512", digest[:2], digest[2:4], digest[4:])
		}
		file, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		info, err := file.Stat()
		if err != nil {
			_ = file.Close()
			return nil, err
		}
		return &http.Response{StatusCode: http.StatusOK, ContentLength: info.Size(), Body: file, Header: make(http.Header)}, nil
	})}
	root := t.TempDir()
	staging := filepath.Join(root, "staging")
	payload := filepath.Join(root, "payload")
	if err := os.Mkdir(staging, 0o700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := buildPayload(ctx, client, "linux", "amd64", staging, payload, func(int, int, string) {}); err != nil {
		t.Fatal(err)
	}
	node := filepath.Join(payload, "runtime", "bin", "node")
	dsh := filepath.Join(payload, "app", "node_modules", "@deepseek-ai", "dsh", "lib", "bin.js")
	command := exec.CommandContext(ctx, node, dsh, "--version")
	output, err := command.CombinedOutput()
	if err != nil || strings.TrimSpace(string(output)) != harnessVer {
		t.Fatalf("dsh version output=%q err=%v", output, err)
	}
	if _, err := os.Stat(filepath.Join(payload, "app", "node_modules", "node-pty", "build", "Release", "pty.node")); err != nil {
		t.Fatal(err)
	}

	installHome := filepath.Join(root, "home")
	if err := os.Mkdir(installHome, 0o700); err != nil {
		t.Fatal(err)
	}
	manager := &Manager{
		home: installHome, goos: "linux", goarch: "amd64",
		client: func(proxyservice.Protocol, int) (*http.Client, error) { return client, nil },
	}
	if err := manager.execute(ctx, storedPlan{}, "", 0, func(string, int, string) {}); err != nil {
		t.Fatal(err)
	}
	managedCommand := filepath.Join(installHome, ".local", "bin", "dsh")
	command = exec.CommandContext(ctx, managedCommand, "--version")
	output, err = command.CombinedOutput()
	if err != nil || strings.TrimSpace(string(output)) != harnessVer {
		t.Fatalf("managed dsh output=%q err=%v", output, err)
	}
}
