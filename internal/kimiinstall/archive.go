package kimiinstall

import (
	"archive/zip"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
)

const maxExpandedBytes = int64(400 * 1024 * 1024)

var errUnsafeArchive = errors.New("unsafe Kimi Code archive")

func extractKimiZip(ctx context.Context, source io.ReaderAt, size int64, destination, expectedName string) error {
	reader, err := zip.NewReader(source, size)
	if err != nil || len(reader.File) != 1 || (expectedName != "kimi" && expectedName != "kimi.exe") {
		return errUnsafeArchive
	}
	entry := reader.File[0]
	if entry.Name != expectedName || !entry.Mode().IsRegular() || entry.Mode()&os.ModeSymlink != 0 ||
		entry.UncompressedSize64 == 0 || entry.UncompressedSize64 > uint64(maxExpandedBytes) {
		return errUnsafeArchive
	}
	input, err := entry.Open()
	if err != nil {
		return errUnsafeArchive
	}
	err = writeExecutable(ctx, input, int64(entry.UncompressedSize64), destination)
	closeErr := input.Close()
	if err != nil {
		return err
	}
	return closeErr
}

func writeExecutable(ctx context.Context, source io.Reader, size int64, destination string) error {
	if size <= 0 || size > maxExpandedBytes || !filepath.IsAbs(destination) {
		return errUnsafeArchive
	}
	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o700)
	if err != nil {
		return errUnsafeArchive
	}
	written, copyErr := copyContext(ctx, output, io.LimitReader(source, size))
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil || written != size {
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
