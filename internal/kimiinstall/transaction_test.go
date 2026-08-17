//go:build !windows

package kimiinstall

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
	"strings"
	"testing"
)

func TestWriteWrapperDisablesUpstreamAutoUpdateWithoutChangingArguments(t *testing.T) {
	for _, goos := range []string{"linux", "windows"} {
		t.Run(goos, func(t *testing.T) {
			payload := t.TempDir()
			if err := os.MkdirAll(filepath.Join(payload, "bin"), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := writeWrapper(payload, filepath.Join(payload, "final"), goos); err != nil {
				t.Fatal(err)
			}
			name := "kimi"
			if goos == "windows" {
				name += ".cmd"
			}
			raw, err := os.ReadFile(filepath.Join(payload, "bin", name))
			if err != nil {
				t.Fatal(err)
			}
			content := string(raw)
			if !strings.Contains(content, "KIMI_CODE_NO_AUTO_UPDATE=1") || !strings.Contains(content, "%*") && !strings.Contains(content, "\"$@\"") {
				t.Fatalf("wrapper = %q", content)
			}
		})
	}
}

func TestDownloadArtifactPinsBodyAndAllowsOnlyOfficialGitHubRedirect(t *testing.T) {
	body := []byte("verified-kimi-archive")
	digest := sha256.Sum256(body)
	item := artifact{URL: "https://github.com/MoonshotAI/kimi-code/releases/download/%40moonshot-ai/kimi-code%400.36.1/archive", Size: int64(len(body)), SHA256: hex.EncodeToString(digest[:])}
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Header.Get("Accept-Encoding") != "identity" {
			t.Fatal("identity encoding was not requested")
		}
		return &http.Response{StatusCode: http.StatusOK, ContentLength: int64(len(body)), Body: io.NopCloser(bytes.NewReader(body)), Header: make(http.Header)}, nil
	})}
	if err := downloadArtifact(context.Background(), client, item, filepath.Join(t.TempDir(), "archive"), nil); err != nil {
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

func TestCommitPayloadRejectsDamagedExistingRuntime(t *testing.T) {
	root := t.TempDir()
	payload, installed := filepath.Join(root, "payload"), filepath.Join(root, "installed")
	marker := []byte("fixed marker")
	writeKimiFixture(t, payload, marker)
	writeKimiFixture(t, installed, marker)
	if err := os.WriteFile(filepath.Join(installed, "bin", "kimi.real"), []byte("damaged"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := commitPayload(payload, installed, marker, "linux"); !errors.Is(err, errVersion) {
		t.Fatalf("commitPayload() error = %v", err)
	}
}

func TestNewCommittedPayloadCanBeRolledBackWithoutRemovingExistingRuntime(t *testing.T) {
	home := t.TempDir()
	marker := []byte("fixed marker")
	newPayload := filepath.Join(home, "new-payload")
	newInstalled := filepath.Join(home, "managed", "new")
	writeKimiFixture(t, newPayload, marker)
	if err := os.MkdirAll(filepath.Dir(newInstalled), 0o700); err != nil {
		t.Fatal(err)
	}
	created, err := commitPayload(newPayload, newInstalled, marker, "linux")
	if err != nil || !created {
		t.Fatalf("commitPayload() = (%v, %v)", created, err)
	}
	if err := removeCommittedPayload(home, newInstalled, marker); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(newInstalled); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("new runtime remains after rollback: %v", err)
	}

	existingPayload := filepath.Join(home, "existing-payload")
	existingInstalled := filepath.Join(home, "managed", "existing")
	writeKimiFixture(t, existingPayload, marker)
	writeKimiFixture(t, existingInstalled, marker)
	created, err = commitPayload(existingPayload, existingInstalled, marker, "linux")
	if err != nil || created {
		t.Fatalf("existing commitPayload() = (%v, %v)", created, err)
	}
	if _, err := os.Stat(existingInstalled); err != nil {
		t.Fatalf("existing runtime changed: %v", err)
	}
}

func TestVerifyKimiRejectsUnexpectedVersionOutput(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "kimi")
	content := "#!/bin/sh\nprintf '%s\\n' 'Kimi Code " + kimiVersion + ".' 'unexpected line'\n"
	if err := os.WriteFile(binary, []byte(content), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := verifyKimi(context.Background(), binary); !errors.Is(err, errVersion) {
		t.Fatalf("verifyKimi() error = %v", err)
	}
}

func TestUnixActivationCreatesOwnedCommandAndProfile(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	home := t.TempDir()
	paths := managedPathsFor(home, "linux", kimiVersion)
	for _, directory := range []string{paths.toolRoot, paths.finalRoot, paths.binRoot, filepath.Dir(paths.wrapperPath)} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(paths.wrapperPath, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := activateCommand(home, paths); err != nil {
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

func writeKimiFixture(t *testing.T, root string, marker []byte) {
	t.Helper()
	for _, relative := range append([]string{".osverse-kimi-runtime"}, criticalPaths("linux")...) {
		path := filepath.Join(root, relative)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		content := []byte(relative)
		if relative == ".osverse-kimi-runtime" {
			content = marker
		}
		if err := os.WriteFile(path, content, 0o700); err != nil {
			t.Fatal(err)
		}
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}
