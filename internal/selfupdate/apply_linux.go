//go:build linux

package selfupdate

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func applyArtifact(staged string, artifact Artifact) (ApplyResult, error) {
	switch artifact.Format {
	case "deb":
		return openSystemInstaller("/usr/bin/xdg-open", staged, "系统软件安装器已打开，请确认更新")
	case "appimage":
		target := os.Getenv("APPIMAGE")
		if !filepath.IsAbs(target) {
			return ApplyResult{}, errors.New("APPIMAGE path is unavailable")
		}
		return replaceAndRestart(staged, target)
	case "tar.gz":
		executable, err := os.Executable()
		if err != nil {
			return ApplyResult{}, err
		}
		executable, err = filepath.EvalSymlinks(executable)
		if err != nil {
			return ApplyResult{}, err
		}
		binary, err := extractPortableBinary(staged, artifact.Size)
		if err != nil {
			return ApplyResult{}, err
		}
		defer os.Remove(binary)
		return replaceAndRestart(binary, executable)
	default:
		return ApplyResult{}, errors.New("unsupported Linux update format")
	}
}

func openSystemInstaller(command, staged, message string) (ApplyResult, error) {
	if _, err := executableFile(command); err != nil {
		return ApplyResult{}, err
	}
	process := exec.Command(command, staged)
	if err := process.Start(); err != nil {
		return ApplyResult{}, err
	}
	if process.Process != nil {
		_ = process.Process.Release()
	}
	return ApplyResult{Started: true, Message: message}, nil
}

func extractPortableBinary(archivePath string, archiveSize int64) (string, error) {
	archive, err := os.Open(archivePath)
	if err != nil {
		return "", err
	}
	defer archive.Close()
	gzipReader, err := gzip.NewReader(io.LimitReader(archive, archiveSize+1))
	if err != nil {
		return "", err
	}
	defer gzipReader.Close()
	tarReader := tar.NewReader(gzipReader)
	destination, err := os.CreateTemp(filepath.Dir(archivePath), ".osverse-binary-*")
	if err != nil {
		return "", err
	}
	path := destination.Name()
	found := false
	remove := true
	defer func() {
		_ = destination.Close()
		if remove {
			_ = os.Remove(path)
		}
	}()
	for entries := 0; ; entries++ {
		if entries > 32 {
			return "", errors.New("portable archive has too many entries")
		}
		header, nextErr := tarReader.Next()
		if nextErr == io.EOF {
			break
		}
		if nextErr != nil {
			return "", nextErr
		}
		clean := filepath.ToSlash(filepath.Clean(header.Name))
		if strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") {
			return "", errors.New("portable archive path is unsafe")
		}
		parts := strings.Split(clean, "/")
		if len(parts) != 2 || parts[0] == "" || parts[1] != "osverse" {
			continue
		}
		if found || header.Typeflag != tar.TypeReg || header.Size <= 0 || header.Size > 256<<20 {
			return "", errors.New("portable archive binary is invalid")
		}
		if _, err := io.CopyN(destination, tarReader, header.Size); err != nil {
			return "", err
		}
		found = true
	}
	if !found {
		return "", errors.New("portable archive does not contain Osverse")
	}
	if err := destination.Chmod(0o700); err != nil {
		return "", err
	}
	if err := destination.Sync(); err != nil {
		return "", err
	}
	if err := destination.Close(); err != nil {
		return "", err
	}
	remove = false
	return path, nil
}

func replaceAndRestart(source, target string) (ApplyResult, error) {
	info, err := executableFile(target)
	if err != nil {
		return ApplyResult{}, err
	}
	directory := filepath.Dir(target)
	staged, err := os.CreateTemp(directory, ".osverse-replacement-*")
	if err != nil {
		return ApplyResult{}, err
	}
	stagedPath := staged.Name()
	cleanup := true
	defer func() {
		_ = staged.Close()
		if cleanup {
			_ = os.Remove(stagedPath)
		}
	}()
	input, err := os.Open(source)
	if err != nil {
		return ApplyResult{}, err
	}
	_, copyErr := io.Copy(staged, io.LimitReader(input, maxArtifactBytes+1))
	closeErr := input.Close()
	if copyErr != nil {
		return ApplyResult{}, copyErr
	}
	if closeErr != nil {
		return ApplyResult{}, closeErr
	}
	mode := info.Mode().Perm() | 0o700
	if err := staged.Chmod(mode); err != nil {
		return ApplyResult{}, err
	}
	if err := staged.Sync(); err != nil {
		return ApplyResult{}, err
	}
	if err := staged.Close(); err != nil {
		return ApplyResult{}, err
	}
	backup := target + ".osverse-previous"
	if backupInfo, backupErr := os.Lstat(backup); backupErr == nil {
		if !backupInfo.Mode().IsRegular() || backupInfo.Mode()&os.ModeSymlink != 0 {
			return ApplyResult{}, errors.New("update backup path is unsafe")
		}
		if err := os.Remove(backup); err != nil {
			return ApplyResult{}, err
		}
	} else if !os.IsNotExist(backupErr) {
		return ApplyResult{}, backupErr
	}
	if err := os.Rename(target, backup); err != nil {
		return ApplyResult{}, err
	}
	if err := os.Rename(stagedPath, target); err != nil {
		_ = os.Rename(backup, target)
		return ApplyResult{}, err
	}
	cleanup = false
	if directoryHandle, openErr := os.Open(directory); openErr == nil {
		_ = directoryHandle.Sync()
		_ = directoryHandle.Close()
	}
	process := exec.Command(target)
	if err := process.Start(); err != nil {
		failed := target + ".osverse-failed"
		_ = os.Rename(target, failed)
		_ = os.Rename(backup, target)
		_ = os.Remove(failed)
		return ApplyResult{}, fmt.Errorf("restart updated Osverse: %w", err)
	}
	if process.Process != nil {
		_ = process.Process.Release()
	}
	return ApplyResult{Started: true, ShouldQuit: true, Message: "更新已安装，正在重新启动 Osverse"}, nil
}
