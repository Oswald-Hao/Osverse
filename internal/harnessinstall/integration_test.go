package harnessinstall

import (
	"bytes"
	"context"
	"encoding/hex"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
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
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	webContext, stopWeb := context.WithCancel(ctx)
	defer stopWeb()
	command = exec.CommandContext(webContext, managedCommand, "web", "--port", strconv.Itoa(port))
	var webOutput bytes.Buffer
	command.Stdout, command.Stderr = &webOutput, &webOutput
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		stopWeb()
		_ = command.Wait()
	}()
	endpoint := "http://127.0.0.1:" + strconv.Itoa(port)
	webDeadline := time.Now().Add(15 * time.Second)
	for {
		response, requestErr := http.Get(endpoint)
		if requestErr == nil {
			body, readErr := io.ReadAll(io.LimitReader(response.Body, 1024*1024))
			_ = response.Body.Close()
			if readErr == nil && response.StatusCode == http.StatusOK && bytes.Contains(body, []byte("DeepSeek Harness")) {
				break
			}
		}
		if time.Now().After(webDeadline) {
			t.Fatalf("Harness web did not start: %s", webOutput.String())
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func TestBuildOtherPlatformPayloadsFromVerifiedCache(t *testing.T) {
	targets := []struct {
		name, goos, goarch, environment string
	}{
		{"windows-amd64", "windows", "amd64", "OSVERSE_HARNESS_WINDOWS_NODE_ARCHIVE"},
		{"darwin-amd64", "darwin", "amd64", "OSVERSE_HARNESS_DARWIN_X64_NODE_ARCHIVE"},
		{"darwin-arm64", "darwin", "arm64", "OSVERSE_HARNESS_DARWIN_ARM64_NODE_ARCHIVE"},
	}
	for _, target := range targets {
		t.Run(target.name, func(t *testing.T) {
			nodeArchive := os.Getenv(target.environment)
			if nodeArchive == "" {
				t.Skip("set " + target.environment + " to the pinned Node archive")
			}
			client := cachedArtifactClient(t, nodeArchive)
			root := t.TempDir()
			staging, payload := filepath.Join(root, "staging"), filepath.Join(root, "payload")
			if err := os.Mkdir(staging, 0o700); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			if err := buildPayload(ctx, client, target.goos, target.goarch, staging, payload, func(int, int, string) {}); err != nil {
				t.Fatal(err)
			}
			if err := writeHarnessWrapper(payload, target.goos, filepath.Join(root, "final")); err != nil {
				t.Fatal(err)
			}
			node := filepath.Join(payload, "runtime", "bin", "node")
			wrapper := filepath.Join(payload, "bin", "dsh")
			if target.goos == "windows" {
				node, wrapper = filepath.Join(payload, "runtime", "node.exe"), filepath.Join(payload, "bin", "dsh.cmd")
			}
			for _, path := range []string{node, wrapper, filepath.Join(payload, "app", "node_modules", "@deepseek-ai", "dsh", "lib", "bin.js")} {
				if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
					t.Fatalf("missing payload file %s: %v", path, err)
				}
			}
			if target.goos == "darwin" {
				npmArch := map[string]string{"amd64": "x64", "arm64": "arm64"}[target.goarch]
				helper := filepath.Join(payload, "app", "node_modules", "node-pty", "prebuilds", "darwin-"+npmArch, "spawn-helper")
				info, err := os.Stat(helper)
				if err != nil || info.Mode().Perm()&0o100 == 0 {
					t.Fatalf("spawn-helper is not executable: %v, %v", info, err)
				}
			}
			if target.goos == "windows" && runtime.GOOS == "windows" {
				assertWindowsHarnessWebStarts(t, ctx, node, filepath.Join(payload, "app", "node_modules", "@deepseek-ai", "dsh", "lib", "bin.js"), root)
				assertWindowsManagedHarnessShimStarts(t, ctx, payload, root)
			}
		})
	}
}

func assertWindowsManagedHarnessShimStarts(t *testing.T, ctx context.Context, payload, root string) {
	t.Helper()
	home := filepath.Join(root, "managed-home")
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatal(err)
	}
	paths := managedPathsFor(home, "windows", harnessVer)
	for _, directory := range []string{paths.root, paths.stagingRoot, paths.toolRoot, paths.binRoot} {
		if err := ensureManagedDirectory(home, directory); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Remove(filepath.Join(payload, "bin", "dsh.cmd")); err != nil {
		t.Fatal(err)
	}
	if err := writeHarnessWrapper(payload, "windows", paths.finalRoot); err != nil {
		t.Fatal(err)
	}
	created, err := commitHarnessPayload(payload, paths.finalRoot, "windows")
	if err != nil || !created {
		t.Fatalf("commitHarnessPayload() = (%t, %v)", created, err)
	}
	if err := activateHarnessCommand(home, paths, "windows"); err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(ctx, paths.shimPath, "--version")
	command.Env = append(os.Environ(), "DSH_HOME="+filepath.Join(root, "managed-dsh-home"), "NO_COLOR=1")
	output, err := command.CombinedOutput()
	if err != nil || strings.TrimSpace(string(output)) != harnessVer {
		t.Fatalf("managed dsh version output=%q err=%v", output, err)
	}
	assertWindowsHarnessCommandWebStarts(t, ctx, paths.shimPath, root, "web")
}

func assertWindowsHarnessWebStarts(t *testing.T, ctx context.Context, node, script, root string) {
	t.Helper()
	assertWindowsHarnessCommandWebStarts(t, ctx, node, root, script, "web")
}

func assertWindowsHarnessCommandWebStarts(t *testing.T, ctx context.Context, executable, root string, prefixArgs ...string) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()

	dshHome := filepath.Join(root, "dsh-home")
	commandContext, stop := context.WithCancel(ctx)
	args := append(append([]string(nil), prefixArgs...), "--port", strconv.Itoa(port))
	command := exec.CommandContext(commandContext, executable, args...)
	command.Env = append(os.Environ(), "DSH_HOME="+dshHome, "NO_COLOR=1")
	var output bytes.Buffer
	command.Stdout, command.Stderr = &output, &output
	if err := command.Start(); err != nil {
		stop()
		t.Fatal(err)
	}
	pid := command.Process.Pid
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	waited := false
	defer func() {
		if runtime.GOOS == "windows" {
			_ = exec.Command("taskkill", "/PID", strconv.Itoa(pid), "/T", "/F").Run()
		}
		stop()
		if !waited {
			select {
			case <-done:
			case <-time.After(10 * time.Second):
				t.Errorf("Harness web process tree did not exit after cleanup")
			}
		}
	}()

	endpoint := "http://127.0.0.1:" + strconv.Itoa(port)
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		response, requestErr := http.Get(endpoint)
		if requestErr == nil {
			body, readErr := io.ReadAll(io.LimitReader(response.Body, 1024*1024))
			_ = response.Body.Close()
			if readErr == nil && response.StatusCode == http.StatusOK && bytes.Contains(body, []byte("DeepSeek Harness")) {
				return
			}
		}
		select {
		case err := <-done:
			waited = true
			t.Fatalf("Harness web exited before startup: %v\n%s", err, output.String())
		case <-time.After(100 * time.Millisecond):
		}
	}
	stop()
	<-done
	waited = true
	t.Fatalf("Harness web did not start: %s", output.String())
}

func cachedArtifactClient(t *testing.T, nodeArchive string) *http.Client {
	t.Helper()
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
	return &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
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
}
