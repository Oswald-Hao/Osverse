//go:build linux

package selfupdate

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	proxyservice "github.com/Oswald-Hao/Osverse/internal/proxy"
)

func TestApplyDownloadsVerifiesAndUsesOpaquePlan(t *testing.T) {
	t.Parallel()
	payload := []byte("verified installer bytes")
	digest := sha256.Sum256(payload)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Length", "24")
		_, _ = writer.Write(payload)
	}))
	defer server.Close()
	service := NewService(t.TempDir(), "1.0.0")
	service.client = func(proxyservice.Protocol, int) (*http.Client, error) {
		return &http.Client{Transport: rewriteTransport{destination: server.URL, base: http.DefaultTransport}}, nil
	}
	applied := false
	service.applier = func(path string, artifact Artifact) (ApplyResult, error) {
		got, err := os.ReadFile(path)
		if err != nil {
			return ApplyResult{}, err
		}
		if !bytes.Equal(got, payload) {
			t.Fatalf("staged payload = %q", got)
		}
		applied = true
		return ApplyResult{Started: true, Message: "ok"}, nil
	}
	service.plans["opaque"] = plan{expires: time.Now().Add(time.Minute), artifact: Artifact{
		URL:      "https://github.com/Oswald-Hao/Osverse/releases/download/v1.0.1/osverse-1.0.1-windows-amd64-setup.exe",
		Filename: "osverse-1.0.1-windows-amd64-setup.exe", Format: "nsis", Size: int64(len(payload)), SHA256: hex.EncodeToString(digest[:]),
	}}
	result, err := service.Apply(context.Background(), "opaque", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Started || !applied {
		t.Fatalf("result=%+v applied=%v", result, applied)
	}
	if _, err := service.Apply(context.Background(), "opaque", "", 0); !errorsIs(err, ErrNoPlan) {
		t.Fatalf("plan reused: %v", err)
	}
}

func TestApplyRejectsChecksumMismatchBeforeInstaller(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) { _, _ = writer.Write([]byte("bad")) }))
	defer server.Close()
	service := NewService(t.TempDir(), "1.0.0")
	service.client = func(proxyservice.Protocol, int) (*http.Client, error) { return server.Client(), nil }
	service.applier = func(string, Artifact) (ApplyResult, error) { t.Fatal("installer called"); return ApplyResult{}, nil }
	service.plans["bad"] = plan{expires: time.Now().Add(time.Minute), artifact: Artifact{URL: server.URL, Size: 3, SHA256: strings.Repeat("a", 64)}}
	if _, err := service.Apply(context.Background(), "bad", "", 0); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("error=%v", err)
	}
}

func errorsIs(got, want error) bool { return got != nil && got.Error() == want.Error() }

func portableArchive(t *testing.T, name string, body []byte) string {
	t.Helper()
	path := t.TempDir() + "/portable.tar.gz"
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(file)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestExtractPortableBinaryUsesOnlyPackagedRoot(t *testing.T) {
	t.Parallel()
	archive := portableArchive(t, "osverse-1.2.3-linux-amd64-ubuntu22.04/osverse", []byte("binary"))
	info, _ := os.Stat(archive)
	path, err := extractPortableBinary(archive, info.Size())
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(path)
	got, _ := os.ReadFile(path)
	if string(got) != "binary" {
		t.Fatalf("payload=%q", got)
	}
}

func TestExtractPortableBinaryRejectsTraversal(t *testing.T) {
	t.Parallel()
	archive := portableArchive(t, "../osverse", []byte("bad"))
	info, _ := os.Stat(archive)
	if _, err := extractPortableBinary(archive, info.Size()); err == nil {
		t.Fatal("traversal accepted")
	}
}
