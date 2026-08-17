package copilotinstall

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestExtractTarAcceptsOnlyOneCopilotBinary(t *testing.T) {
	valid := tarFixture(t, []tar.Header{{Name: "copilot", Typeflag: tar.TypeReg, Mode: 0o755, Size: 6}}, [][]byte{[]byte("binary")})
	destination := filepath.Join(t.TempDir(), "copilot")
	if err := extractCopilotTar(context.Background(), bytes.NewReader(valid), destination); err != nil {
		t.Fatal(err)
	}
	if raw, err := os.ReadFile(destination); err != nil || string(raw) != "binary" {
		t.Fatalf("binary = %q, %v", raw, err)
	}

	for _, headers := range [][]tar.Header{
		{{Name: "../copilot", Typeflag: tar.TypeReg, Size: 1}},
		{{Name: "copilot", Typeflag: tar.TypeSymlink, Linkname: "/tmp/escape"}},
		{{Name: "copilot", Typeflag: tar.TypeReg, Size: 1}, {Name: "extra", Typeflag: tar.TypeReg, Size: 1}},
	} {
		fixture := tarFixture(t, headers, nil)
		if err := extractCopilotTar(context.Background(), bytes.NewReader(fixture), filepath.Join(t.TempDir(), "copilot")); !errors.Is(err, errUnsafeArchive) {
			t.Fatalf("headers %#v error = %v", headers, err)
		}
	}
}

func TestExtractZipAcceptsOnlyOneCopilotExe(t *testing.T) {
	valid := zipFixture(t, []zipEntry{{name: "copilot.exe", body: "binary"}})
	destination := filepath.Join(t.TempDir(), "copilot.exe")
	if err := extractCopilotZip(context.Background(), bytes.NewReader(valid), int64(len(valid)), destination); err != nil {
		t.Fatal(err)
	}
	for _, entries := range [][]zipEntry{
		{{name: "../copilot.exe", body: "bad"}},
		{{name: "copilot.exe", body: "binary", mode: os.ModeSymlink | 0o777}},
		{{name: "copilot.exe", body: "binary"}, {name: "extra", body: "bad"}},
	} {
		fixture := zipFixture(t, entries)
		if err := extractCopilotZip(context.Background(), bytes.NewReader(fixture), int64(len(fixture)), filepath.Join(t.TempDir(), "copilot.exe")); !errors.Is(err, errUnsafeArchive) {
			t.Fatalf("entries %#v error = %v", entries, err)
		}
	}
}

func tarFixture(t *testing.T, headers []tar.Header, bodies [][]byte) []byte {
	t.Helper()
	var output bytes.Buffer
	gz := gzip.NewWriter(&output)
	writer := tar.NewWriter(gz)
	for index := range headers {
		header := headers[index]
		body := []byte("x")
		if index < len(bodies) {
			body = bodies[index]
		}
		if header.Typeflag == tar.TypeReg && header.Size == 0 {
			header.Size = int64(len(body))
		}
		if err := writer.WriteHeader(&header); err != nil {
			t.Fatal(err)
		}
		if header.Typeflag == tar.TypeReg && header.Size > 0 {
			if int64(len(body)) < header.Size {
				body = append(body, make([]byte, header.Size-int64(len(body)))...)
			}
			if _, err := writer.Write(body[:header.Size]); err != nil {
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
	return output.Bytes()
}

type zipEntry struct {
	name, body string
	mode       os.FileMode
}

func zipFixture(t *testing.T, entries []zipEntry) []byte {
	t.Helper()
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	for _, entry := range entries {
		header := &zip.FileHeader{Name: entry.name, Method: zip.Deflate}
		mode := entry.mode
		if mode == 0 {
			mode = 0o755
		}
		header.SetMode(mode)
		file, err := writer.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.Write([]byte(entry.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}
