//go:build linux

package copilotinstall

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	proxyservice "github.com/Oswald-Hao/Osverse/internal/proxy"
)

func TestOfficialLinuxStandaloneArchiveFromVerifiedCache(t *testing.T) {
	archive := os.Getenv("OSVERSE_COPILOT_LINUX_ARCHIVE")
	if archive == "" {
		t.Skip("set OSVERSE_COPILOT_LINUX_ARCHIVE to the pinned official archive")
	}
	item, err := artifactForTarget("linux/amd64")
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(archive)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil || hex.EncodeToString(hash.Sum(nil)) != item.SHA256 {
		t.Fatalf("archive checksum mismatch: %v", err)
	}
	file, err = os.Open(archive)
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "copilot")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := extractCopilotTar(ctx, file, destination); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(ctx, destination, "--no-auto-update", "--version")
	command.Env = append(os.Environ(), "CI=1", "NO_COLOR=1")
	output, err := command.CombinedOutput()
	if err != nil || !strings.HasPrefix(string(output), "GitHub Copilot CLI "+copilotVersion+".") {
		t.Fatalf("copilot --version = %q, %v", output, err)
	}
}

func TestOfficialWindowsStandaloneArchiveFromVerifiedCache(t *testing.T) {
	archive := os.Getenv("OSVERSE_COPILOT_WINDOWS_ARCHIVE")
	if archive == "" {
		t.Skip("set OSVERSE_COPILOT_WINDOWS_ARCHIVE to the pinned official archive")
	}
	item, err := artifactForTarget("windows/amd64")
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(archive)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil || hex.EncodeToString(hash.Sum(nil)) != item.SHA256 {
		t.Fatalf("archive checksum mismatch: %v", err)
	}
	file, err = os.Open(archive)
	if err != nil {
		t.Fatal(err)
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "copilot.exe")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := extractCopilotZip(ctx, file, info.Size(), destination); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	extracted, err := os.Open(destination)
	if err != nil {
		t.Fatal(err)
	}
	defer extracted.Close()
	magic := make([]byte, 2)
	if _, err := io.ReadFull(extracted, magic); err != nil || string(magic) != "MZ" {
		t.Fatalf("extracted Windows executable magic = %q, err=%v", magic, err)
	}
}

func TestManagerInstallsAndRunsOfficialLinuxArchive(t *testing.T) {
	archive := os.Getenv("OSVERSE_COPILOT_LINUX_ARCHIVE")
	if archive == "" {
		t.Skip("set OSVERSE_COPILOT_LINUX_ARCHIVE to the pinned official archive")
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
	waitForPhase(t, manager, task.ID, "completed")
	shim := managedPathsFor(home, "linux", copilotVersion).shimPath
	command := exec.Command(shim, "--version")
	command.Env = append(os.Environ(), "CI=1", "NO_COLOR=1")
	output, err := command.CombinedOutput()
	if err != nil || !strings.HasPrefix(string(output), "GitHub Copilot CLI "+copilotVersion+".") {
		t.Fatalf("installed copilot --version = %q, %v", output, err)
	}
}

type integrationTransport func(*http.Request) (*http.Response, error)

func (transport integrationTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}
