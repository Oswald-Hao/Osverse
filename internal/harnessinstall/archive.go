package harnessinstall

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const maxArchiveEntries = 100000

var errUnsafeArchive = errors.New("unsafe Harness archive")

func extractNPMPackage(ctx context.Context, source io.Reader, destination string, expandedLimit int64) error {
	gz, err := gzip.NewReader(source)
	if err != nil {
		return fmt.Errorf("%w: gzip", errUnsafeArchive)
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
			return nil
		}
		if err != nil {
			return fmt.Errorf("%w: tar", errUnsafeArchive)
		}
		entries++
		if entries > maxArchiveEntries || !strings.HasPrefix(header.Name, "package/") {
			return errUnsafeArchive
		}
		name := strings.TrimPrefix(header.Name, "package/")
		if name == "" && header.Typeflag == tar.TypeDir {
			continue
		}
		if !safeArchivePath(name) {
			return errUnsafeArchive
		}
		target := filepath.Join(destination, filepath.FromSlash(name))
		if !pathWithin(destination, target) {
			return errUnsafeArchive
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o700); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if header.Size < 0 || expanded > expandedLimit-header.Size {
				return errUnsafeArchive
			}
			expanded += header.Size
			if err := writeArchiveFile(ctx, reader, header.Size, destination, target); err != nil {
				return err
			}
		default:
			return errUnsafeArchive
		}
	}
}

func extractNodeTar(ctx context.Context, source io.Reader, wanted, destination string, expandedLimit int64) error {
	gz, err := gzip.NewReader(source)
	if err != nil {
		return errUnsafeArchive
	}
	defer gz.Close()
	reader := tar.NewReader(gz)
	entries := 0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return errUnsafeArchive
		}
		if err != nil {
			return errUnsafeArchive
		}
		entries++
		if entries > maxArchiveEntries || !safeArchivePath(header.Name) {
			return errUnsafeArchive
		}
		if header.Name != wanted {
			continue
		}
		if (header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA) || header.Size <= 0 || header.Size > expandedLimit {
			return errUnsafeArchive
		}
		if err := writeArchiveFile(ctx, reader, header.Size, filepath.Dir(destination), destination); err != nil {
			return err
		}
		return os.Chmod(destination, 0o755)
	}
}

func extractNodeZip(ctx context.Context, source io.ReaderAt, sourceSize int64, wanted, destination string, expandedLimit int64) error {
	reader, err := zip.NewReader(source, sourceSize)
	if err != nil || len(reader.File) > maxArchiveEntries {
		return errUnsafeArchive
	}
	for _, file := range reader.File {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !safeArchivePath(file.Name) {
			return errUnsafeArchive
		}
		if file.Name != wanted {
			continue
		}
		if !file.Mode().IsRegular() || file.UncompressedSize64 == 0 || file.UncompressedSize64 > uint64(expandedLimit) {
			return errUnsafeArchive
		}
		input, err := file.Open()
		if err != nil {
			return errUnsafeArchive
		}
		err = writeArchiveFile(ctx, input, int64(file.UncompressedSize64), filepath.Dir(destination), destination)
		closeErr := input.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		return os.Chmod(destination, 0o755)
	}
	return errUnsafeArchive
}

func writeArchiveFile(ctx context.Context, source io.Reader, size int64, root, destination string) error {
	if !pathWithin(root, destination) || size < 0 {
		return errUnsafeArchive
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return err
	}
	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
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
	if trimmed == "" || !utf8.ValidString(name) || strings.ContainsAny(name, "\\\x00") ||
		strings.HasPrefix(name, "/") || path.Clean(trimmed) != trimmed {
		return false
	}
	for _, component := range strings.Split(trimmed, "/") {
		if component == "" || component == "." || component == ".." || strings.TrimRight(component, ". ") != component {
			return false
		}
	}
	return true
}

func pathWithin(root, candidate string) bool {
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(candidate))
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}
