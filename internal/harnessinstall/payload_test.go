package harnessinstall

import (
	"bytes"
	"context"
	"crypto/sha512"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestDownloadPackagePinsURLBodyAndIntegrity(t *testing.T) {
	body := []byte("fixed package")
	digest := sha512.Sum512(body)
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Host != registryHost || request.Header.Get("Accept-Encoding") != "identity" {
			t.Fatalf("unexpected request %#v", request)
		}
		return &http.Response{
			StatusCode: http.StatusOK, ContentLength: int64(len(body)),
			Body: io.NopCloser(bytes.NewReader(body)), Header: make(http.Header),
		}, nil
	})}
	destination := filepath.Join(t.TempDir(), "package.tgz")
	written, err := downloadPackage(context.Background(), client, lockedPackage{
		URL: "https://registry.npmjs.org/example/-/example-1.0.0.tgz", Integrity: digest[:],
	}, destination)
	if err != nil || written != int64(len(body)) {
		t.Fatalf("written=%d err=%v", written, err)
	}

	badDestination := filepath.Join(t.TempDir(), "package.tgz")
	if _, err := downloadPackage(context.Background(), client, lockedPackage{
		URL: "https://registry.npmjs.org/example/-/example-1.0.0.tgz", Integrity: make([]byte, sha512.Size),
	}, badDestination); err != errHashMismatch {
		t.Fatalf("bad integrity error = %v", err)
	}
}

func TestFinalizeNativeAssetsInstallsPinnedLinuxPTY(t *testing.T) {
	payload := t.TempDir()
	if err := finalizeNativeAssets("linux", "amd64", payload); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(payload, "app", "node_modules", "node-pty", "build", "Release", "pty.node")
	content, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(content, linuxX64PTY) {
		t.Fatalf("pty content mismatch: %v", err)
	}
}
