package apps

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	proxyservice "github.com/Oswald-Hao/Osverse/internal/proxy"
)

var (
	errDownload     = errors.New("desktop artifact download failed")
	errHashMismatch = errors.New("desktop artifact hash mismatch")
	errInvalidImage = errors.New("invalid AppImage")
)

type linkState struct {
	exists bool
	target string
}
type fileState struct {
	exists  bool
	content []byte
	mode    os.FileMode
}

func (manager *Manager) execute(ctx context.Context, item artifact, protocol proxyservice.Protocol, port int, progress func(progressUpdate)) (returnErr error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	root, err := ensureDirs(manager.home, 0o700, ".local", "share", "osverse")
	if err != nil {
		return err
	}
	appRoot, err := ensureDirs(root, 0o700, "apps", item.ID)
	if err != nil {
		return err
	}
	binRoot, err := ensureDirs(manager.home, 0o755, ".local", "bin")
	if err != nil {
		return err
	}
	applicationsRoot, err := ensureDirs(manager.home, 0o755, ".local", "share", "applications")
	if err != nil {
		return err
	}
	stagingRoot, err := ensureDirs(root, 0o700, "staging")
	if err != nil {
		return err
	}

	currentPath := filepath.Join(appRoot, "current")
	launcherPath := filepath.Join(binRoot, item.Command)
	desktopPath := filepath.Join(applicationsRoot, item.DesktopFile)
	currentBefore, err := inspectOwnedLink(currentPath, appRoot)
	if err != nil {
		return err
	}
	launcherBefore, err := inspectOwnedLink(launcherPath, appRoot)
	if err != nil {
		return ErrExternalEntry
	}
	desktopContent := managedDesktop(item, launcherPath)
	desktopBefore, err := inspectDesktop(desktopPath, desktopContent)
	if err != nil {
		return ErrExternalEntry
	}

	staging, err := os.MkdirTemp(stagingRoot, item.ID+"-")
	if err != nil {
		return err
	}
	if err := os.Chmod(staging, 0o700); err != nil {
		return err
	}
	defer os.RemoveAll(staging)
	imagePath := filepath.Join(staging, "application.AppImage")
	progress(progressUpdate{"downloading", 5, "正在下载官方 AppImage"})
	if err := manager.download(ctx, item, protocol, port, imagePath, progress); err != nil {
		return err
	}
	progress(progressUpdate{"verifying", 75, "正在校验 AppImage"})
	if err := validateImage(imagePath); err != nil {
		return err
	}
	if err := os.Chmod(imagePath, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(staging, ".osverse-artifact-sha256"), []byte(item.SHA256+"\n"), 0o600); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	finalPath := filepath.Join(appRoot, item.Version)
	if err := commitVersion(staging, finalPath, item); err != nil {
		return err
	}
	progress(progressUpdate{"committing", 92, "正在原子切换应用版本"})
	currentChanged, launcherChanged, desktopChanged := false, false, false
	committed := false
	defer func() {
		if returnErr == nil || committed {
			return
		}
		if desktopChanged {
			_ = restoreFile(desktopPath, desktopBefore)
		}
		if launcherChanged {
			_ = restoreLink(launcherPath, launcherBefore)
		}
		if currentChanged {
			_ = restoreLink(currentPath, currentBefore)
		}
	}()
	if err := replaceLink(currentPath, item.Version); err != nil {
		return err
	}
	currentChanged = true
	if err := replaceLink(launcherPath, filepath.Join(appRoot, "current", "application.AppImage")); err != nil {
		return err
	}
	launcherChanged = true
	if err := atomicWrite(desktopPath, desktopContent, 0o644); err != nil {
		return err
	}
	desktopChanged = true
	if err := ctx.Err(); err != nil {
		return err
	}
	committed = true
	progress(progressUpdate{"committing", 99, "桌面入口已更新"})
	return nil
}

func (manager *Manager) download(ctx context.Context, item artifact, protocol proxyservice.Protocol, port int, destination string, progress func(progressUpdate)) error {
	client, err := manager.client(protocol, port)
	if err != nil || client == nil {
		return fmt.Errorf("%w: client", errDownload)
	}
	copyClient := *client
	copyClient.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if len(via) > 3 || request.URL.Scheme != "https" || request.URL.Hostname() != "release-assets.githubusercontent.com" {
			return errors.New("untrusted artifact redirect")
		}
		return nil
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, item.URL, nil)
	if err != nil {
		return fmt.Errorf("%w: request", errDownload)
	}
	request.Header.Set("Accept-Encoding", "identity")
	request.Header.Set("User-Agent", "Osverse-Installer")
	response, err := copyClient.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("%w: transport", errDownload)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || (response.ContentLength >= 0 && response.ContentLength != item.DownloadBytes) {
		return fmt.Errorf("%w: response", errDownload)
	}
	file, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	hash := sha256.New()
	written, copyErr := copyContext(ctx, io.MultiWriter(file, hash), io.LimitReader(response.Body, item.DownloadBytes+1), func(total int64) {
		progress(progressUpdate{"downloading", 5 + int(total*62/item.DownloadBytes), "正在下载官方 AppImage"})
	})
	syncErr := file.Sync()
	closeErr := file.Close()
	if copyErr != nil {
		return copyErr
	}
	if syncErr != nil {
		return syncErr
	}
	if closeErr != nil {
		return closeErr
	}
	if written != item.DownloadBytes {
		return fmt.Errorf("%w: size", errDownload)
	}
	if hex.EncodeToString(hash.Sum(nil)) != item.SHA256 {
		return errHashMismatch
	}
	return nil
}

func copyContext(ctx context.Context, destination io.Writer, source io.Reader, report func(int64)) (int64, error) {
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
			if report != nil {
				report(total)
			}
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

func validateImage(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	header := make([]byte, 4)
	if _, err := io.ReadFull(file, header); err != nil || string(header) != "\x7fELF" {
		return errInvalidImage
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return errInvalidImage
	}
	return nil
}

func commitVersion(staging, final string, item artifact) error {
	if info, err := os.Lstat(final); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return errors.New("managed version collision")
		}
		marker, readErr := os.ReadFile(filepath.Join(final, ".osverse-artifact-sha256"))
		image, statErr := os.Lstat(filepath.Join(final, "application.AppImage"))
		if readErr != nil || statErr != nil || !image.Mode().IsRegular() || strings.TrimSpace(string(marker)) != item.SHA256 {
			return errors.New("managed version is invalid")
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.Rename(staging, final)
}

func ensureDirs(base string, mode os.FileMode, parts ...string) (string, error) {
	current := filepath.Clean(base)
	for _, part := range parts {
		if part == "" || part == "." || part == ".." || filepath.Base(part) != part {
			return "", errors.New("invalid path component")
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			if err := os.Mkdir(current, mode); err != nil {
				return "", err
			}
			continue
		}
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return "", errors.New("managed directory is unsafe")
		}
	}
	return current, nil
}

func inspectOwnedLink(path, root string) (linkState, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return linkState{}, nil
	}
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		return linkState{}, ErrExternalEntry
	}
	target, err := os.Readlink(path)
	if err != nil {
		return linkState{}, err
	}
	resolved := target
	if !filepath.IsAbs(resolved) {
		resolved = filepath.Join(filepath.Dir(path), resolved)
	}
	if !within(root, filepath.Clean(resolved)) {
		return linkState{}, ErrExternalEntry
	}
	return linkState{true, target}, nil
}

func replaceLink(path, target string) error {
	temporary := path + ".osverse-new"
	if err := os.Symlink(target, temporary); err != nil {
		return err
	}
	if err := os.Rename(temporary, path); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	return nil
}

func restoreLink(path string, state linkState) error {
	if !state.exists {
		return os.Remove(path)
	}
	return replaceLink(path, state.target)
}

func inspectDesktop(path string, expected []byte) (fileState, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return fileState{}, nil
	}
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return fileState{}, ErrExternalEntry
	}
	content, err := os.ReadFile(path)
	if err != nil || string(content) != string(expected) {
		return fileState{}, ErrExternalEntry
	}
	return fileState{true, content, info.Mode().Perm()}, nil
}

func managedDesktop(item artifact, launcher string) []byte {
	quoted := strings.NewReplacer("\\", "\\\\", "\"", "\\\"", "`", "\\`", "$", "\\$").Replace(launcher)
	return []byte("[Desktop Entry]\nType=Application\nName=" + item.Name + "\nExec=\"" + quoted + "\"\nTerminal=false\nCategories=Development;\nX-Osverse-Managed=true\n")
}

func atomicWrite(path string, content []byte, mode os.FileMode) error {
	temp, err := os.CreateTemp(filepath.Dir(path), ".osverse-desktop-")
	if err != nil {
		return err
	}
	name := temp.Name()
	defer os.Remove(name)
	if err := temp.Chmod(mode); err != nil {
		temp.Close()
		return err
	}
	if _, err := temp.Write(content); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

func restoreFile(path string, state fileState) error {
	if !state.exists {
		return os.Remove(path)
	}
	return atomicWrite(path, state.content, state.mode)
}

func within(root, candidate string) bool {
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(candidate))
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
