package kimiinstall

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestExtractZipAcceptsOnlyOneKimiExe(t *testing.T) {
	valid := zipFixture(t, []zipEntry{{name: "kimi.exe", body: "binary"}})
	destination := filepath.Join(t.TempDir(), "kimi.exe")
	if err := extractKimiZip(context.Background(), bytes.NewReader(valid), int64(len(valid)), destination, "kimi.exe"); err != nil {
		t.Fatal(err)
	}
	for _, entries := range [][]zipEntry{
		{{name: "../kimi.exe", body: "bad"}},
		{{name: "kimi.exe", body: "binary", mode: os.ModeSymlink | 0o777}},
		{{name: "kimi.exe", body: "binary"}, {name: "extra", body: "bad"}},
	} {
		fixture := zipFixture(t, entries)
		if err := extractKimiZip(context.Background(), bytes.NewReader(fixture), int64(len(fixture)), filepath.Join(t.TempDir(), "kimi.exe"), "kimi.exe"); !errors.Is(err, errUnsafeArchive) {
			t.Fatalf("entries %#v error = %v", entries, err)
		}
	}
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
