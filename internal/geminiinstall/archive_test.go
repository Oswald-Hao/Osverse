package geminiinstall

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestExtractRuntimeAndPackageRejectTraversal(t *testing.T) {
	runtimeArchive := filepath.Join(t.TempDir(), "node.tgz")
	writeTarGz(t, runtimeArchive, map[string][]byte{
		"node-v22.23.2-linux-x64/bin/node": []byte("node"),
	})
	destination := filepath.Join(t.TempDir(), "node")
	if err := extractRuntime(context.Background(), runtimes["linux/amd64"], runtimeArchive, destination); err != nil {
		t.Fatal(err)
	}
	if raw, err := os.ReadFile(destination); err != nil || string(raw) != "node" {
		t.Fatalf("node output = %q, %v", raw, err)
	}

	packageArchive := filepath.Join(t.TempDir(), "package.tgz")
	writeTarGz(t, packageArchive, map[string][]byte{
		"package/package.json":     []byte(`{"name":"@google/gemini-cli"}`),
		"package/bundle/gemini.js": []byte("script"),
		"package/LICENSE":          []byte("license"),
	})
	packageRoot := t.TempDir()
	if err := extractPackage(context.Background(), packageArchive, packageRoot); err != nil {
		t.Fatal(err)
	}
	if raw, err := os.ReadFile(filepath.Join(packageRoot, "package", "bundle", "gemini.js")); err != nil || string(raw) != "script" {
		t.Fatalf("package output = %q, %v", raw, err)
	}

	unsafeArchive := filepath.Join(t.TempDir(), "unsafe.tgz")
	writeTarGz(t, unsafeArchive, map[string][]byte{
		"package/package.json": []byte("{}"),
		"package/../escape":    []byte("escape"),
		"package/a":            []byte("a"),
	})
	if err := extractPackage(context.Background(), unsafeArchive, t.TempDir()); err == nil {
		t.Fatal("traversal archive was accepted")
	}
}

func TestExtractWindowsNodeFromZip(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "node.zip")
	file, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	entry, err := writer.Create("node-v22.23.2-win-x64/node.exe")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = entry.Write([]byte("node-exe"))
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "node.exe")
	if err := extractRuntime(context.Background(), runtimes["windows/amd64"], archive, destination); err != nil {
		t.Fatal(err)
	}
	if raw, _ := os.ReadFile(destination); string(raw) != "node-exe" {
		t.Fatalf("node output = %q", raw)
	}
}

func writeTarGz(t *testing.T, destination string, entries map[string][]byte) {
	t.Helper()
	file, err := os.Create(destination)
	if err != nil {
		t.Fatal(err)
	}
	gzipWriter := gzip.NewWriter(file)
	tw := tar.NewWriter(gzipWriter)
	for name, content := range entries {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(content)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := bytes.NewReader(content).WriteTo(tw); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}
