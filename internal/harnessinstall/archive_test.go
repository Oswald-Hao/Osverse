package harnessinstall

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

func TestExtractPackageRejectsTraversalAndLinks(t *testing.T) {
	for _, header := range []tar.Header{
		{Name: "package/../escape", Mode: 0o644, Size: 1, Typeflag: tar.TypeReg},
		{Name: "package/link", Linkname: "/tmp/out", Typeflag: tar.TypeSymlink},
	} {
		t.Run(header.Name, func(t *testing.T) {
			body := []byte(nil)
			if header.Size > 0 {
				body = []byte("x")
			}
			archive := tarGzip(t, []tarEntry{{header: header, body: body}})
			if err := extractNPMPackage(context.Background(), bytes.NewReader(archive), t.TempDir(), 1024); err == nil {
				t.Fatal("unsafe package accepted")
			}
		})
	}
}

func TestExtractPackageWritesOnlyPackagePayload(t *testing.T) {
	archive := tarGzip(t, []tarEntry{
		{header: tar.Header{Name: "package/lib/bin.js", Mode: 0o755, Size: 2, Typeflag: tar.TypeReg}, body: []byte("ok")},
		{header: tar.Header{Name: "package/empty", Mode: 0o755, Typeflag: tar.TypeDir}},
	})
	destination := t.TempDir()
	if err := extractNPMPackage(context.Background(), bytes.NewReader(archive), destination, 1024); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(destination, "lib", "bin.js"))
	if err != nil || string(content) != "ok" {
		t.Fatalf("content = %q, err = %v", content, err)
	}
	info, err := os.Stat(filepath.Join(destination, "lib", "bin.js"))
	if err != nil || info.Mode().Perm()&0o100 == 0 {
		t.Fatalf("executable mode was not preserved: %v, %v", info, err)
	}
}

func TestExtractNodeRuntimeFromTarAndZip(t *testing.T) {
	tarBytes := tarGzip(t, []tarEntry{{
		header: tar.Header{Name: "node-v/bin/node", Mode: 0o755, Size: 4, Typeflag: tar.TypeReg}, body: []byte("node"),
	}})
	destination := filepath.Join(t.TempDir(), "node")
	if err := extractNodeTar(context.Background(), bytes.NewReader(tarBytes), "node-v/bin/node", destination, 16); err != nil {
		t.Fatal(err)
	}

	var value bytes.Buffer
	writer := zip.NewWriter(&value)
	entry, err := writer.Create("node-v/node.exe")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = entry.Write([]byte("exe"))
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	destination = filepath.Join(t.TempDir(), "node.exe")
	if err := extractNodeZip(context.Background(), bytes.NewReader(value.Bytes()), int64(value.Len()), "node-v/node.exe", destination, 16); err != nil {
		t.Fatal(err)
	}
}

type tarEntry struct {
	header tar.Header
	body   []byte
}

func tarGzip(t *testing.T, entries []tarEntry) []byte {
	t.Helper()
	var value bytes.Buffer
	gz := gzip.NewWriter(&value)
	w := tar.NewWriter(gz)
	for _, entry := range entries {
		header := entry.header
		if err := w.WriteHeader(&header); err != nil {
			t.Fatal(err)
		}
		if len(entry.body) > 0 {
			if _, err := w.Write(entry.body); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return value.Bytes()
}
