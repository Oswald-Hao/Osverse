package qweninstall

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const (
	maxArchiveEntries = 100_000
	maxExpandedBytes  = int64(900 * 1024 * 1024)
)

var errUnsafeArchive = errors.New("unsafe Qwen Code archive")

func extractQwenTar(ctx context.Context, source io.Reader, destination string) error {
	gz, err := gzip.NewReader(source)
	if err != nil {
		return errUnsafeArchive
	}
	defer gz.Close()
	reader := tar.NewReader(gz)
	var expanded int64
	entries := 0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			if entries == 0 {
				return errUnsafeArchive
			}
			return nil
		}
		if err != nil {
			return errUnsafeArchive
		}
		entries++
		if entries > maxArchiveEntries || !safeArchivePath(header.Name) || !withinQwenRoot(header.Name) {
			return errUnsafeArchive
		}
		target := filepath.Join(destination, filepath.FromSlash(path.Clean(strings.TrimSuffix(header.Name, "/"))))
		if !pathWithin(destination, target) {
			return errUnsafeArchive
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := ensureArchiveDirectory(destination, target); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if header.Size < 0 || expanded > maxExpandedBytes-header.Size {
				return errUnsafeArchive
			}
			expanded += header.Size
			mode := os.FileMode(0o600)
			if header.Mode&0o111 != 0 {
				mode = 0o700
			}
			if err := writeArchiveFile(ctx, reader, header.Size, destination, target, mode); err != nil {
				return err
			}
		default:
			return errUnsafeArchive
		}
	}
}

func extractQwenZip(ctx context.Context, source io.ReaderAt, sourceSize int64, destination string) error {
	reader, err := zip.NewReader(source, sourceSize)
	if err != nil || len(reader.File) == 0 || len(reader.File) > maxArchiveEntries {
		return errUnsafeArchive
	}
	var expanded uint64
	for _, entry := range reader.File {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !safeArchivePath(entry.Name) || !withinQwenRoot(entry.Name) || entry.Mode()&os.ModeSymlink != 0 {
			return errUnsafeArchive
		}
		target := filepath.Join(destination, filepath.FromSlash(path.Clean(strings.TrimSuffix(entry.Name, "/"))))
		if !pathWithin(destination, target) {
			return errUnsafeArchive
		}
		if entry.FileInfo().IsDir() {
			if err := ensureArchiveDirectory(destination, target); err != nil {
				return err
			}
			continue
		}
		if !entry.Mode().IsRegular() || entry.UncompressedSize64 > uint64(maxExpandedBytes) ||
			expanded > uint64(maxExpandedBytes)-entry.UncompressedSize64 {
			return errUnsafeArchive
		}
		expanded += entry.UncompressedSize64
		input, err := entry.Open()
		if err != nil {
			return errUnsafeArchive
		}
		mode := os.FileMode(0o600)
		if entry.Mode().Perm()&0o111 != 0 || strings.HasSuffix(strings.ToLower(entry.Name), ".exe") {
			mode = 0o700
		}
		err = writeArchiveFile(ctx, input, int64(entry.UncompressedSize64), destination, target, mode)
		closeErr := input.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}

func withinQwenRoot(name string) bool {
	trimmed := strings.TrimSuffix(name, "/")
	return trimmed == "qwen-code" || strings.HasPrefix(trimmed, "qwen-code/")
}

func ensureArchiveDirectory(root, destination string) error {
	if !pathWithin(root, destination) {
		return errUnsafeArchive
	}
	return os.MkdirAll(destination, 0o700)
}

func writeArchiveFile(ctx context.Context, source io.Reader, size int64, root, destination string, mode os.FileMode) error {
	if size < 0 || !pathWithin(root, destination) {
		return errUnsafeArchive
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return err
	}
	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return errUnsafeArchive
	}
	written, copyErr := copyContext(ctx, output, io.LimitReader(source, size))
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if written != size {
		return errUnsafeArchive
	}
	return nil
}

func copyContext(ctx context.Context, destination io.Writer, source io.Reader) (int64, error) {
	buffer := make([]byte, 64*1024)
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		read, readErr := source.Read(buffer)
		if read > 0 {
			written, writeErr := destination.Write(buffer[:read])
			total += int64(written)
			if writeErr != nil {
				return total, writeErr
			}
			if written != read {
				return total, io.ErrShortWrite
			}
		}
		if errors.Is(readErr, io.EOF) {
			return total, nil
		}
		if readErr != nil {
			return total, readErr
		}
	}
}

func safeArchivePath(name string) bool {
	trimmed := strings.TrimSuffix(name, "/")
	if trimmed == "" || !utf8.ValidString(name) || strings.ContainsAny(name, "\\:\x00") || strings.HasPrefix(name, "/") {
		return false
	}
	for _, component := range strings.Split(trimmed, "/") {
		for _, value := range component {
			if value < 0x20 {
				return false
			}
		}
		if component == "" || component == "." || component == ".." || strings.TrimRight(component, ". ") != component || reservedWindowsName(component) {
			return false
		}
	}
	return true
}

func reservedWindowsName(component string) bool {
	base := strings.ToUpper(strings.TrimSuffix(strings.TrimRight(component, ". "), path.Ext(component)))
	if base == "CON" || base == "PRN" || base == "AUX" || base == "NUL" || base == "CLOCK$" {
		return true
	}
	return len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && base[3] >= '1' && base[3] <= '9'
}

func pathWithin(root, candidate string) bool {
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(candidate))
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}
