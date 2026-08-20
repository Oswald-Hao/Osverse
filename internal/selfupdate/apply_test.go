//go:build linux

package selfupdate

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
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
		if !strings.HasSuffix(path, ".exe") {
			t.Fatalf("staged installer lost executable suffix: %q", path)
		}
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

func TestPrivateUpdateDirectoryRejectsLinkAndUsesPrivateMode(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "updates")
	if err := ensurePrivateDirectory(root); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(root)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Fatalf("update directory mode = %#o, want 0700", got)
	}

	outside := t.TempDir()
	link := filepath.Join(t.TempDir(), "updates-link")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	if err := ensurePrivateDirectory(link); err == nil {
		t.Fatal("symlink update directory accepted")
	}
}

func TestExecutableFileRejectsDirectoryAndLink(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	regular := filepath.Join(directory, "osverse")
	if err := os.WriteFile(regular, []byte("binary"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := executableFile(regular); err != nil {
		t.Fatalf("regular executable rejected: %v", err)
	}
	if _, err := executableFile(directory); err == nil {
		t.Fatal("directory accepted as executable")
	}
	link := filepath.Join(directory, "osverse-link")
	if err := os.Symlink(regular, link); err != nil {
		t.Fatal(err)
	}
	if _, err := executableFile(link); err == nil {
		t.Fatal("symlink accepted as executable")
	}
}

func TestLinuxUpdateLockSerializesProcessesAndAllowsStaleFileReuse(t *testing.T) {
	directory := t.TempDir()
	first, err := acquireUpdateLock(directory)
	if err != nil {
		t.Fatal(err)
	}
	if second, err := acquireUpdateLock(directory); !errors.Is(err, ErrUpdateInProgress) {
		if second != nil {
			_ = second.Close()
		}
		t.Fatalf("second acquire = (%v, %v), want ErrUpdateInProgress", second, err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	third, err := acquireUpdateLock(directory)
	if err != nil {
		t.Fatalf("stale lock file blocked later update: %v", err)
	}
	if err := third.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestLinuxUpdateLockReleasesAfterHolderProcessTerminates(t *testing.T) {
	directory := t.TempDir()
	marker := filepath.Join(directory, "holder-ready")
	command := exec.Command(os.Args[0], "-test.run=^TestLinuxUpdateLockSubprocessHelper$")
	command.Env = append(os.Environ(), "OSVERSE_UPDATE_LOCK_HELPER="+directory, "OSVERSE_UPDATE_LOCK_MARKER="+marker)
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if command.Process != nil {
			_ = command.Process.Kill()
		}
		_ = command.Wait()
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("lock holder did not become ready")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if lock, err := acquireUpdateLock(directory); !errors.Is(err, ErrUpdateInProgress) {
		if lock != nil {
			_ = lock.Close()
		}
		t.Fatalf("acquire while child holds lock = (%v, %v)", lock, err)
	}
	if err := command.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = command.Wait()
	command.Process = nil
	lock, err := acquireUpdateLock(directory)
	if err != nil {
		t.Fatalf("terminated holder left a blocking lock: %v", err)
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestLinuxUpdateLockSubprocessHelper(t *testing.T) {
	directory := os.Getenv("OSVERSE_UPDATE_LOCK_HELPER")
	if directory == "" {
		return
	}
	lock, err := acquireUpdateLock(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := os.WriteFile(os.Getenv("OSVERSE_UPDATE_LOCK_MARKER"), []byte("ready"), 0o600); err != nil {
		t.Fatal(err)
	}
	time.Sleep(30 * time.Second)
}

func TestReplaceAndRestartDoesNotTouchTargetWhenAnotherProcessUpdates(t *testing.T) {
	directory := t.TempDir()
	target := filepath.Join(directory, "osverse")
	source := filepath.Join(directory, "next")
	if err := os.WriteFile(target, []byte("current"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte("next"), 0o700); err != nil {
		t.Fatal(err)
	}
	lock, err := acquireUpdateLock(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if _, err := replaceAndRestart(source, target); !errors.Is(err, ErrUpdateInProgress) {
		t.Fatalf("replaceAndRestart() error = %v, want ErrUpdateInProgress", err)
	}
	raw, err := os.ReadFile(target)
	if err != nil || string(raw) != "current" {
		t.Fatalf("target changed = (%q, %v)", raw, err)
	}
	if _, err := os.Lstat(target + ".osverse-previous"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("backup was touched: %v", err)
	}
}

func TestLinuxUpdateLockRejectsUnsafeEvidence(t *testing.T) {
	tests := map[string]func(*testing.T, string){
		"symlink": func(t *testing.T, path string) {
			target := filepath.Join(t.TempDir(), "outside")
			if err := os.WriteFile(target, []byte("outside"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, path); err != nil {
				t.Fatal(err)
			}
		},
		"hard link": func(t *testing.T, path string) {
			target := filepath.Join(t.TempDir(), "outside")
			if err := os.WriteFile(target, []byte("outside"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Link(target, path); err != nil {
				t.Fatal(err)
			}
		},
		"permissive": func(t *testing.T, path string) {
			if err := os.WriteFile(path, []byte("lock"), 0o666); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, prepare := range tests {
		t.Run(name, func(t *testing.T) {
			directory := t.TempDir()
			prepare(t, filepath.Join(directory, linuxUpdateLockName))
			if lock, err := acquireUpdateLock(directory); err == nil {
				_ = lock.Close()
				t.Fatal("unsafe update lock accepted")
			}
		})
	}
}
