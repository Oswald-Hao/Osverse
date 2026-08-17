package qweninstall

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

func TestExtractTarAcceptsFixedQwenRootAndRejectsUnsafeEntries(t *testing.T) {
	safe := qwenTar(t, []tar.Header{
		{Name: "qwen-code/", Typeflag: tar.TypeDir, Mode: 0o755},
		{Name: "qwen-code/bin/qwen", Typeflag: tar.TypeReg, Mode: 0o755, Size: 4},
	}, [][]byte{nil, []byte("qwen")})
	destination := t.TempDir()
	if err := extractQwenTar(context.Background(), bytes.NewReader(safe), destination); err != nil {
		t.Fatal(err)
	}
	if content, err := os.ReadFile(filepath.Join(destination, "qwen-code", "bin", "qwen")); err != nil || string(content) != "qwen" {
		t.Fatalf("content=%q err=%v", content, err)
	}

	for _, header := range []tar.Header{
		{Name: "qwen-code/../escape", Typeflag: tar.TypeReg, Mode: 0o644, Size: 1},
		{Name: "qwen-code/link", Typeflag: tar.TypeSymlink, Linkname: "/tmp/out"},
		{Name: "other/bin/qwen", Typeflag: tar.TypeReg, Mode: 0o755, Size: 1},
		{Name: "qwen-code/CON.txt", Typeflag: tar.TypeReg, Mode: 0o644, Size: 1},
	} {
		t.Run(header.Name, func(t *testing.T) {
			body := []byte(nil)
			if header.Size > 0 {
				body = []byte("x")
			}
			archive := qwenTar(t, []tar.Header{header}, [][]byte{body})
			if err := extractQwenTar(context.Background(), bytes.NewReader(archive), t.TempDir()); err == nil {
				t.Fatal("unsafe archive entry accepted")
			}
		})
	}
}

func TestExtractZipRejectsTraversalAndSymlinks(t *testing.T) {
	for _, tc := range []struct {
		name string
		mode os.FileMode
	}{
		{name: "qwen-code/../escape", mode: 0o600},
		{name: "qwen-code/link", mode: os.ModeSymlink | 0o777},
		{name: "other/bin/qwen.cmd", mode: 0o600},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var value bytes.Buffer
			writer := zip.NewWriter(&value)
			header := &zip.FileHeader{Name: tc.name, Method: zip.Store}
			header.SetMode(tc.mode)
			entry, err := writer.CreateHeader(header)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := entry.Write([]byte("x")); err != nil {
				t.Fatal(err)
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			reader := bytes.NewReader(value.Bytes())
			if err := extractQwenZip(context.Background(), reader, int64(reader.Len()), t.TempDir()); err == nil {
				t.Fatal("unsafe zip entry accepted")
			}
		})
	}
}

func qwenTar(t *testing.T, headers []tar.Header, bodies [][]byte) []byte {
	t.Helper()
	var value bytes.Buffer
	gz := gzip.NewWriter(&value)
	writer := tar.NewWriter(gz)
	for index := range headers {
		header := headers[index]
		if err := writer.WriteHeader(&header); err != nil {
			t.Fatal(err)
		}
		if len(bodies[index]) > 0 {
			if _, err := writer.Write(bodies[index]); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return value.Bytes()
}
