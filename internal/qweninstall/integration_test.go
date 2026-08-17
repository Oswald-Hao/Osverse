//go:build linux

package qweninstall

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Oswald-Hao/Osverse/internal/profiles"
	proxyservice "github.com/Oswald-Hao/Osverse/internal/proxy"
)

func TestOfficialStandaloneArchiveFromVerifiedCache(t *testing.T) {
	archive := os.Getenv("OSVERSE_QWEN_LINUX_ARCHIVE")
	if archive == "" {
		t.Skip("set OSVERSE_QWEN_LINUX_ARCHIVE to the pinned official archive")
	}
	item, err := artifactForTarget("linux/amd64")
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(archive)
	if err != nil {
		t.Fatal(err)
	}
	digest, hashErr := streamSHA256(file)
	closeErr := file.Close()
	if hashErr != nil || closeErr != nil || hex.EncodeToString(digest[:]) != item.SHA256 {
		t.Fatalf("archive checksum mismatch: hash=%v close=%v", hashErr, closeErr)
	}
	destination := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := extractArtifact(ctx, item, archive, destination); err != nil {
		t.Fatal(err)
	}
	if err := verifyQwenPayload(ctx, destination, "linux", "amd64"); err != nil {
		t.Fatal(err)
	}
}

func TestOfficialQwenUsesAppliedProfileWithoutChangingModelID(t *testing.T) {
	archive := os.Getenv("OSVERSE_QWEN_LINUX_ARCHIVE")
	if archive == "" || runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("requires the pinned Linux x64 archive on Linux x64")
	}
	requests := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/chat/completions" {
			http.NotFound(writer, request)
			return
		}
		body, err := io.ReadAll(io.LimitReader(request.Body, 2*1024*1024))
		if err != nil {
			t.Errorf("read request: %v", err)
			return
		}
		var decoded map[string]any
		if err := json.Unmarshal(body, &decoded); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		select {
		case requests <- decoded:
		default:
		}
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(writer, "data: {\"id\":\"osverse-test\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"OSVERSE_QWEN_OK\"},\"finish_reason\":null}]}\n\n")
		_, _ = io.WriteString(writer, "data: {\"id\":\"osverse-test\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n")
		_, _ = io.WriteString(writer, "data: [DONE]\n\n")
	}))
	defer server.Close()

	home := t.TempDir()
	adapters, err := profiles.NewAdapterSet(home)
	if err != nil {
		t.Fatal(err)
	}
	const modelID = "deepseek/deepseek-v4-flash"
	if _, err := adapters.Apply(context.Background(), profiles.TargetQwen, profiles.Input{
		Name: "Qwen integration", APIKey: "test-only-key", BaseURL: server.URL,
		Model: modelID, AllowPrivateNetwork: true,
	}); err != nil {
		t.Fatal(err)
	}
	destination := t.TempDir()
	item, _ := artifactForTarget("linux/amd64")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := extractArtifact(ctx, item, archive, destination); err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(ctx, filepath.Join(destination, "qwen-code", "bin", "qwen"),
		"--safe-mode", "--prompt", "Reply with the supplied test token.", "--output-format", "json")
	command.Env = append(os.Environ(), "HOME="+home, "CI=1", "NO_COLOR=1", "NO_PROXY=127.0.0.1,localhost")
	output, err := command.CombinedOutput()
	if err != nil || !strings.Contains(string(output), "OSVERSE_QWEN_OK") {
		t.Fatalf("Qwen profile run failed: %v\n%s", err, output)
	}
	select {
	case request := <-requests:
		if request["model"] != modelID {
			t.Fatalf("request model = %#v, want exact %q", request["model"], modelID)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Qwen did not call the configured OpenAI Chat endpoint")
	}
}

func TestManagerInstallsAndRunsOfficialLinuxArchiveFromVerifiedCache(t *testing.T) {
	archive := os.Getenv("OSVERSE_QWEN_LINUX_ARCHIVE")
	if archive == "" || runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("requires the pinned Linux x64 archive on Linux x64")
	}
	home := t.TempDir()
	manager, err := NewManager(home)
	if err != nil {
		t.Fatal(err)
	}
	manager.client = func(proxyservice.Protocol, int) (*http.Client, error) {
		return &http.Client{Transport: integrationTransport(func(*http.Request) (*http.Response, error) {
			file, err := os.Open(archive)
			if err != nil {
				return nil, err
			}
			info, err := file.Stat()
			if err != nil {
				_ = file.Close()
				return nil, err
			}
			return &http.Response{StatusCode: http.StatusOK, ContentLength: info.Size(), Body: file, Header: make(http.Header)}, nil
		})}, nil
	}
	t.Setenv("SHELL", "/bin/sh")
	plan, err := manager.CreatePlan(context.Background(), componentID)
	if err != nil {
		t.Fatal(err)
	}
	task, err := manager.Start(context.Background(), plan.ID, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	waitForQwenPhase(t, manager, task.ID, "completed")
	shim := managedPathsFor(home, "linux", qwenVersion).shimPath
	command := exec.Command(shim, "--version")
	command.Env = append(os.Environ(), "CI=1", "NO_COLOR=1")
	output, err := command.CombinedOutput()
	if err != nil || strings.TrimSpace(string(output)) != qwenVersion {
		t.Fatalf("installed qwen --version = %q, %v", output, err)
	}
}

func TestOfficialWindowsArchiveFromVerifiedCache(t *testing.T) {
	archive := os.Getenv("OSVERSE_QWEN_WINDOWS_ARCHIVE")
	if archive == "" {
		t.Skip("set OSVERSE_QWEN_WINDOWS_ARCHIVE to the pinned official archive")
	}
	item, err := artifactForTarget("windows/amd64")
	if err != nil {
		t.Fatal(err)
	}
	destination := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := extractArtifact(ctx, item, archive, destination); err != nil {
		t.Fatal(err)
	}
	if err := writeQwenWrapper(destination, filepath.Join(destination, "installed"), "windows"); err != nil {
		t.Fatal(err)
	}
	for _, relative := range criticalPaths("windows") {
		info, err := os.Lstat(filepath.Join(destination, filepath.FromSlash(relative)))
		if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			t.Fatalf("invalid Windows payload path %s: %v", relative, err)
		}
	}
}

type integrationTransport func(*http.Request) (*http.Response, error)

func (transport integrationTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}
