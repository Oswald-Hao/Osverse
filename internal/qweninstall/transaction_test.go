//go:build !windows

package qweninstall

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDownloadArtifactPinsBodyAndAllowsOnlyOfficialGitHubRedirect(t *testing.T) {
	body := []byte("verified-qwen-archive")
	digest := sha256.Sum256(body)
	item := artifact{URL: "https://github.com/QwenLM/qwen-code/releases/download/v1/archive", Size: int64(len(body)), SHA256: hex.EncodeToString(digest[:])}
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Header.Get("Accept-Encoding") != "identity" {
			t.Fatal("identity encoding was not requested")
		}
		return &http.Response{StatusCode: http.StatusOK, ContentLength: int64(len(body)), Body: io.NopCloser(bytes.NewReader(body)), Header: make(http.Header)}, nil
	})}
	path := filepath.Join(t.TempDir(), "archive")
	if err := downloadArtifact(context.Background(), client, item, path, nil); err != nil {
		t.Fatal(err)
	}
	trustedRedirect := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Host == "github.com" {
			return &http.Response{StatusCode: http.StatusFound, Body: io.NopCloser(strings.NewReader("")), Header: http.Header{"Location": []string{"https://release-assets.githubusercontent.com/github-production-release-asset/1/archive?sig=fixed"}}, Request: request}, nil
		}
		return &http.Response{StatusCode: http.StatusOK, ContentLength: int64(len(body)), Body: io.NopCloser(bytes.NewReader(body)), Header: make(http.Header), Request: request}, nil
	})}
	if err := downloadArtifact(context.Background(), trustedRedirect, item, filepath.Join(t.TempDir(), "archive"), nil); err != nil {
		t.Fatalf("official redirect failed: %v", err)
	}

	redirecting := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusFound, Body: io.NopCloser(strings.NewReader("")), Header: http.Header{"Location": []string{"https://example.invalid/escape"}}}, nil
	})}
	if err := downloadArtifact(context.Background(), redirecting, item, filepath.Join(t.TempDir(), "archive"), nil); !errors.Is(err, errDownload) {
		t.Fatalf("redirect error = %v", err)
	}
}

func TestCommitPayloadRejectsDamagedOrSymlinkedExistingRuntime(t *testing.T) {
	for _, mode := range []string{"damaged", "symlink"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			payload, installed := filepath.Join(root, "payload"), filepath.Join(root, "installed")
			writeQwenFixture(t, payload, []byte("verified"))
			writeQwenFixture(t, installed, []byte("verified"))
			node := filepath.Join(installed, "qwen-code", "node", "bin", "node")
			if mode == "damaged" {
				if err := os.WriteFile(node, []byte("damaged"), 0o700); err != nil {
					t.Fatal(err)
				}
			} else {
				outside := filepath.Join(root, "outside")
				if err := os.WriteFile(outside, []byte("verified"), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(node); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, node); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := commitPayload(payload, installed, "linux"); !errors.Is(err, errVersion) {
				t.Fatalf("commitPayload() error = %v", err)
			}
		})
	}
}

func TestActivationFailureRemovesOnlyNewQwenRuntime(t *testing.T) {
	home := t.TempDir()
	paths := managedPathsFor(home, "linux", qwenVersion)
	if err := os.MkdirAll(paths.toolRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	marker := []byte("fixed marker")
	activationErr := errors.New("injected activation failure")
	payload := filepath.Join(home, "payload")
	writeQwenFixture(t, payload, marker)
	if err := commitAndActivatePayload(home, payload, paths, marker, "linux", func(string, managedPaths, string) error { return activationErr }); !errors.Is(err, activationErr) {
		t.Fatalf("commitAndActivatePayload() error = %v", err)
	}
	if _, err := os.Lstat(paths.finalRoot); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("new runtime remains after activation failure: %v", err)
	}

	payload = filepath.Join(home, "payload-existing")
	writeQwenFixture(t, payload, marker)
	writeQwenFixture(t, paths.finalRoot, marker)
	if err := commitAndActivatePayload(home, payload, paths, marker, "linux", func(string, managedPaths, string) error { return activationErr }); !errors.Is(err, activationErr) {
		t.Fatalf("existing commitAndActivatePayload() error = %v", err)
	}
	if _, err := os.Stat(paths.finalRoot); err != nil {
		t.Fatalf("pre-existing runtime was removed: %v", err)
	}
}

func TestRollbackFailureMessageDoesNotClaimCleanRollback(t *testing.T) {
	message := publicFailure(errors.Join(errRollback, errVersion))
	if !strings.Contains(message, "回滚失败") || strings.Contains(message, "未安装") {
		t.Fatalf("publicFailure() = %q", message)
	}
}

func TestQwenWrappersPreserveUpstreamRuntimeLayout(t *testing.T) {
	t.Run("windows", func(t *testing.T) {
		payload := t.TempDir()
		if err := writeQwenWrapper(payload, `C:\Users\test\ignored`, "windows"); err != nil {
			t.Fatal(err)
		}
		raw, err := os.ReadFile(filepath.Join(payload, "bin", "qwen.cmd"))
		if err != nil {
			t.Fatal(err)
		}
		want := "@echo off\r\ncall \"%~dp0..\\qwen-code\\bin\\qwen.cmd\" %*\r\nexit /b %ERRORLEVEL%\r\n"
		if string(raw) != want {
			t.Fatalf("Windows wrapper = %q", raw)
		}
	})

	t.Run("unix safely quotes final path", func(t *testing.T) {
		payload := t.TempDir()
		finalRoot := filepath.Join(t.TempDir(), "user's runtime")
		if err := writeQwenWrapper(payload, finalRoot, "linux"); err != nil {
			t.Fatal(err)
		}
		raw, err := os.ReadFile(filepath.Join(payload, "bin", "qwen"))
		if err != nil {
			t.Fatal(err)
		}
		wantTarget := shellQuote(filepath.Join(finalRoot, "qwen-code", "bin", "qwen"))
		if !strings.Contains(string(raw), "exec "+wantTarget+" \"$@\"") {
			t.Fatalf("Unix wrapper = %q", raw)
		}
	})
}

func TestUnixActivationCreatesOwnedCommandAndProfile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix activation")
	}
	t.Setenv("SHELL", "/bin/sh")
	home := t.TempDir()
	paths := managedPathsFor(home, "linux", qwenVersion)
	for _, directory := range []string{paths.toolRoot, paths.finalRoot, paths.binRoot, filepath.Dir(paths.wrapperPath)} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(paths.wrapperPath, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := activateCommand(home, paths, "linux"); err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(paths.shimPath)
	if err != nil || resolved != paths.wrapperPath {
		t.Fatalf("resolved=%q err=%v", resolved, err)
	}
	profile, err := os.ReadFile(filepath.Join(home, ".profile"))
	if err != nil || !strings.Contains(string(profile), pathProfileStart) {
		t.Fatalf("profile=%q err=%v", profile, err)
	}
}

func writeQwenFixture(t *testing.T, root string, content []byte) {
	t.Helper()
	files := append([]string{".osverse-qwen-runtime"}, criticalPaths("linux")...)
	for _, relative := range files {
		path := filepath.Join(root, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		value := content
		if relative == ".osverse-qwen-runtime" {
			value = []byte("fixed marker")
		}
		if err := os.WriteFile(path, value, 0o700); err != nil {
			t.Fatal(err)
		}
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}
