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

	"golang.org/x/sys/unix"
)

const linuxUpdateLockName = ".osverse-update.lock"

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
	directory := filepath.Dir(target)
	lock, err := acquireUpdateLock(directory)
	if err != nil {
		return ApplyResult{}, err
	}
	defer lock.Close()
	info, err := executableFile(target)
	if err != nil {
		return ApplyResult{}, err
	}
	backup := target + ".osverse-previous"
	hadBackup := false
	if backupInfo, backupErr := os.Lstat(backup); backupErr == nil {
		if !backupInfo.Mode().IsRegular() || backupInfo.Mode()&os.ModeSymlink != 0 {
			return ApplyResult{}, errors.New("update backup path is unsafe")
		}
		hadBackup = true
	} else if !os.IsNotExist(backupErr) {
		return ApplyResult{}, backupErr
	}
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
	if err := atomicExchange(target, stagedPath); err != nil {
		return ApplyResult{}, err
	}
	cleanup = false
	if hadBackup {
		if err := atomicExchange(stagedPath, backup); err != nil {
			return ApplyResult{}, rollbackPreBackupExchange(target, stagedPath, err, &cleanup)
		}
	} else if err := os.Rename(stagedPath, backup); err != nil {
		return ApplyResult{}, rollbackPreBackupExchange(target, stagedPath, err, &cleanup)
	}
	if err := syncUpdateDirectory(directory); err != nil {
		rollbackErr := rollbackFinalizedUpdate(target, backup, stagedPath, hadBackup)
		if rollbackErr == nil {
			cleanup = true
		}
		return ApplyResult{}, errors.Join(err, rollbackErr)
	}
	process := exec.Command(target)
	if err := process.Start(); err != nil {
		rollbackErr := rollbackFinalizedUpdate(target, backup, stagedPath, hadBackup)
		if rollbackErr == nil {
			cleanup = true
		}
		return ApplyResult{}, fmt.Errorf("restart updated Osverse: %w", errors.Join(err, rollbackErr))
	}
	cleanup = true
	if process.Process != nil {
		_ = process.Process.Release()
	}
	_ = os.Remove(stagedPath)
	_ = syncUpdateDirectory(directory)
	return ApplyResult{Started: true, ShouldQuit: true, Message: "更新已安装，正在重新启动 Osverse"}, nil
}

func atomicExchange(left, right string) error {
	if !filepath.IsAbs(left) || !filepath.IsAbs(right) || filepath.Clean(left) != left || filepath.Clean(right) != right ||
		filepath.Dir(left) != filepath.Dir(right) || left == right {
		return errors.New("atomic update exchange paths are unsafe")
	}
	if err := unix.Renameat2(unix.AT_FDCWD, left, unix.AT_FDCWD, right, unix.RENAME_EXCHANGE); err != nil {
		return fmt.Errorf("atomic update exchange unavailable: %w", err)
	}
	return nil
}

func rollbackPreBackupExchange(target, staged string, cause error, cleanup *bool) error {
	rollbackErr := atomicExchange(target, staged)
	if rollbackErr == nil {
		*cleanup = true
	}
	return errors.Join(cause, rollbackErr)
}

func rollbackFinalizedUpdate(target, backup, staged string, hadBackup bool) error {
	if err := atomicExchange(target, backup); err != nil {
		return err
	}
	if hadBackup {
		if err := atomicExchange(backup, staged); err != nil {
			return err
		}
	} else if err := os.Rename(backup, staged); err != nil {
		return err
	}
	if err := os.Remove(staged); err != nil {
		return err
	}
	return syncUpdateDirectory(filepath.Dir(target))
}

func syncUpdateDirectory(directory string) error {
	handle, err := os.Open(directory)
	if err != nil {
		return err
	}
	syncErr := handle.Sync()
	closeErr := handle.Close()
	return errors.Join(syncErr, closeErr)
}

func acquireUpdateLock(directory string) (*os.File, error) {
	if !filepath.IsAbs(directory) || filepath.Clean(directory) != directory {
		return nil, errors.New("update lock directory is unsafe")
	}
	path := filepath.Join(directory, linuxUpdateLockName)
	descriptor, err := unix.Open(path, unix.O_RDWR|unix.O_CREAT|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0o600)
	if err != nil {
		return nil, err
	}
	closeOnError := true
	defer func() {
		if closeOnError {
			_ = unix.Close(descriptor)
		}
	}()
	var stat unix.Stat_t
	if err := unix.Fstat(descriptor, &stat); err != nil {
		return nil, err
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Nlink != 1 || stat.Uid != uint32(os.Geteuid()) || stat.Mode&0o777 != 0o600 {
		return nil, errors.New("update lock evidence is unsafe")
	}
	if err := unix.Flock(descriptor, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
			return nil, ErrUpdateInProgress
		}
		return nil, err
	}
	file := os.NewFile(uintptr(descriptor), path)
	if file == nil {
		return nil, errors.New("update lock handle is unavailable")
	}
	closeOnError = false
	return file, nil
}
