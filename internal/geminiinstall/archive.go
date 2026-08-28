package geminiinstall

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
)

const (
	maxArchiveEntries  = 4096
	maxPackageExpanded = 180 * 1024 * 1024
	maxNodeExpanded    = 160 * 1024 * 1024
)

var errUnsafeArchive = errors.New("Gemini CLI archive is unsafe")

func extractRuntime(ctx context.Context, item runtimeArtifact, archive, destination string) error {
	wanted := item.ArchiveRoot + "/" + item.ArchivePath
	if item.Format == "zip" {
		return extractNodeZip(ctx, archive, wanted, destination)
	}
	return extractNodeTar(ctx, archive, wanted, destination)
}

func extractNodeTar(ctx context.Context, archive, wanted, destination string) error {
	file, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer file.Close()
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return errUnsafeArchive
	}
	defer gzipReader.Close()
	reader := tar.NewReader(gzipReader)
	found := false
	for entries := 0; ; entries++ {
		if entries >= maxArchiveEntries {
			return errUnsafeArchive
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return errUnsafeArchive
		}
		if !safeArchiveName(header.Name) {
			return errUnsafeArchive
		}
		if header.Name != wanted {
			continue
		}
		if found || !header.FileInfo().Mode().IsRegular() || header.Size <= 0 || header.Size > maxNodeExpanded {
			return errUnsafeArchive
		}
		if err := writeLimitedFile(ctx, destination, reader, header.Size, 0o700); err != nil {
			return err
		}
		found = true
	}
	if !found {
		return errUnsafeArchive
	}
	return nil
}

func extractNodeZip(ctx context.Context, archive, wanted, destination string) error {
	reader, err := zip.OpenReader(archive)
	if err != nil {
		return errUnsafeArchive
	}
	defer reader.Close()
	if len(reader.File) == 0 || len(reader.File) > maxArchiveEntries {
		return errUnsafeArchive
	}
	found := false
	for _, item := range reader.File {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !safeArchiveName(item.Name) {
			return errUnsafeArchive
		}
		if item.Name != wanted {
			continue
		}
		if found || !item.Mode().IsRegular() || item.UncompressedSize64 == 0 || item.UncompressedSize64 > maxNodeExpanded {
			return errUnsafeArchive
		}
		input, err := item.Open()
		if err != nil {
			return errUnsafeArchive
		}
		err = writeLimitedFile(ctx, destination, input, int64(item.UncompressedSize64), 0o700)
		_ = input.Close()
		if err != nil {
			return err
		}
		found = true
	}
	if !found {
		return errUnsafeArchive
	}
	return nil
}

func extractPackage(ctx context.Context, archive, destination string) error {
	file, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer file.Close()
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return errUnsafeArchive
	}
	defer gzipReader.Close()
	reader := tar.NewReader(gzipReader)
	var expanded int64
	entries := 0
	files := 0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil || entries >= maxArchiveEntries {
			return errUnsafeArchive
		}
		entries++
		if !safePackageName(header.Name) || header.Size < 0 {
			return errUnsafeArchive
		}
		relative := path.Clean(strings.TrimPrefix(header.Name, "package/"))
		output := filepath.Join(destination, "package", filepath.FromSlash(relative))
		if !pathWithin(filepath.Join(destination, "package"), output) {
			return errUnsafeArchive
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(output, 0o700); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			expanded += header.Size
			if expanded > maxPackageExpanded {
				return errUnsafeArchive
			}
			if err := writeLimitedFile(ctx, output, reader, header.Size, 0o600); err != nil {
				return err
			}
			files++
		default:
			return errUnsafeArchive
		}
	}
	if files < 3 || expanded == 0 {
		return errUnsafeArchive
	}
	return nil
}

func writeLimitedFile(ctx context.Context, destination string, source io.Reader, size int64, mode os.FileMode) error {
	if size < 0 || size > maxPackageExpanded {
		return errUnsafeArchive
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return err
	}
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	written, copyErr := copyContext(ctx, output, io.LimitReader(source, size+1))
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
	buffer := make([]byte, 128*1024)
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

func safeArchiveName(value string) bool {
	return value != "" && value == path.Clean(value) && !strings.HasPrefix(value, "/") &&
		!strings.Contains(value, `\`) && !strings.Contains(value, "\x00") &&
		value != ".." && !strings.HasPrefix(value, "../") && !strings.Contains(value, "/../")
}

func safePackageName(value string) bool {
	return strings.HasPrefix(value, "package/") && value != "package/" && safeArchiveName(value)
}

func pathWithin(root, candidate string) bool {
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(candidate))
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func archiveError(item string, err error) error {
	return fmt.Errorf("%s: %w", item, err)
}
