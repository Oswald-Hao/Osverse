//go:build linux

package kimiinstall

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
	archive := os.Getenv("OSVERSE_KIMI_LINUX_ARCHIVE")
	if archive == "" {
		t.Skip("set OSVERSE_KIMI_LINUX_ARCHIVE to the pinned official archive")
	}
	item, err := artifactForTarget("linux/amd64")
	if err != nil {
		t.Fatal(err)
	}
	verifyArchiveDigest(t, archive, item.SHA256)
	file, err := os.Open(archive)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "kimi")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := extractKimiZip(ctx, file, info.Size(), destination, "kimi"); err != nil {
		t.Fatal(err)
	}
	if err := verifyKimi(ctx, destination); err != nil {
		t.Fatal(err)
	}
}

func TestOfficialWindowsStandaloneArchiveFromVerifiedCache(t *testing.T) {
	archive := os.Getenv("OSVERSE_KIMI_WINDOWS_ARCHIVE")
	if archive == "" {
		t.Skip("set OSVERSE_KIMI_WINDOWS_ARCHIVE to the pinned official archive")
	}
	item, err := artifactForTarget("windows/amd64")
	if err != nil {
		t.Fatal(err)
	}
	verifyArchiveDigest(t, archive, item.SHA256)
	file, err := os.Open(archive)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "kimi.exe")
	if err := extractKimiZip(context.Background(), file, info.Size(), destination, "kimi.exe"); err != nil {
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
	archive := os.Getenv("OSVERSE_KIMI_LINUX_ARCHIVE")
	if archive == "" {
		t.Skip("set OSVERSE_KIMI_LINUX_ARCHIVE to the pinned official archive")
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
	shim := managedPathsFor(home, "linux", kimiVersion).shimPath
	output, err := exec.Command(shim, "--version").CombinedOutput()
	if err != nil || strings.TrimSpace(string(output)) != kimiVersion {
		t.Fatalf("installed kimi --version = %q, %v", output, err)
	}
}

func verifyArchiveDigest(t *testing.T, path, expected string) {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.New()
	_, copyErr := io.Copy(hash, file)
	closeErr := file.Close()
	if copyErr != nil || closeErr != nil || hex.EncodeToString(hash.Sum(nil)) != expected {
		t.Fatalf("archive checksum mismatch: copy=%v close=%v", copyErr, closeErr)
	}
}

type integrationTransport func(*http.Request) (*http.Response, error)

func (transport integrationTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}
