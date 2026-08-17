//go:build windows

package windowsinstall

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
	"unsafe"

	"github.com/Oswald-Hao/Osverse/internal/platform"
	proxyservice "github.com/Oswald-Hao/Osverse/internal/proxy"
	xwindows "golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const (
	maxArchiveEntries = 2048
	versionTimeout    = 15 * time.Second
	versionOutput     = 64 * 1024
	shimMarkerPrefix  = "@rem Osverse managed shim v1: "
)

var (
	errDownload      = errors.New("Windows artifact download failed")
	errHashMismatch  = errors.New("Windows artifact hash mismatch")
	errUnsafeArchive = errors.New("unsafe Windows artifact archive")
	errVersion       = errors.New("installed Windows version verification failed")
	errExternalShim  = errors.New("external Windows command entry exists")
	errRollback      = errors.New("Windows installation rollback failed")
)

func (manager *Manager) execute(ctx context.Context, stored storedPlan, protocol proxyservice.Protocol, port int, progress func(progressUpdate)) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	item := stored.item
	root, err := ensureManagedDirectories(manager.home, "AppData", "Local", "Osverse")
	if err != nil {
		return err
	}
	stagingRoot, err := ensureManagedDirectories(root, "staging")
	if err != nil {
		return err
	}
	staging, err := os.MkdirTemp(stagingRoot, item.ID+"-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)

	progress(progressUpdate{phase: "downloading", progress: 5, message: "正在下载官方 Windows 制品"})
	archive := filepath.Join(staging, "artifact.tgz")
	if err := manager.download(ctx, item, protocol, port, archive, progress); err != nil {
		return err
	}
	progress(progressUpdate{phase: "verifying", progress: 72, message: "正在校验并安全展开安装包"})
	payload := filepath.Join(staging, "payload")
	if err := os.Mkdir(payload, 0o700); err != nil {
		return err
	}
	if err := extractArchive(ctx, archive, payload, item.ExpandedBytesLimit); err != nil {
		return err
	}
	binary := filepath.Join(payload, filepath.FromSlash(item.BinaryPath))
	if err := validatePayloadBinary(payload, binary); err != nil {
		return err
	}
	if err := manager.verifyVersion(ctx, binary, item); err != nil {
		return err
	}
	marker := []byte(item.SHA256 + "\r\n" + item.BinaryPath + "\r\n")
	if err := os.WriteFile(filepath.Join(payload, ".osverse-artifact"), marker, 0o600); err != nil {
		return err
	}

	progress(progressUpdate{phase: "committing", progress: 91, message: "正在切换 Osverse 命令入口"})
	toolRoot, err := ensureManagedDirectories(root, "tools", item.ID)
	if err != nil {
		return err
	}
	finalRoot := filepath.Join(toolRoot, item.Version)
	return commitAndActivateWindowsPayload(manager.home, payload, finalRoot, marker, func() error {
		finalBinary := filepath.Join(finalRoot, filepath.FromSlash(item.BinaryPath))
		binRoot, err := ensureManagedDirectories(manager.home, ".local", "bin")
		if err != nil {
			return err
		}
		if err := rejectConflictingAliases(binRoot, item.Command); err != nil {
			return err
		}
		shimPath := filepath.Join(binRoot, item.Command+".cmd")
		previous, existed, err := inspectShim(shimPath, item.ID, filepath.Join(root, "tools"))
		if err != nil {
			return err
		}
		shim := managedShim(item.ID, finalBinary)
		if err := atomicReplace(shimPath, shim); err != nil {
			if restoreErr := restoreShim(shimPath, previous, existed); restoreErr != nil {
				return errors.Join(errRollback, err, restoreErr)
			}
			return err
		}
		pathChanged, originalPath, originalType, err := ensureUserPath(binRoot)
		if err != nil {
			if restoreErr := restoreShim(shimPath, previous, existed); restoreErr != nil {
				return errors.Join(errRollback, err, restoreErr)
			}
			return err
		}
		if err := ctx.Err(); err != nil {
			var rollbackErrors []error
			if pathChanged {
				if restoreErr := restoreUserPath(originalPath, originalType); restoreErr != nil {
					rollbackErrors = append(rollbackErrors, restoreErr)
				}
			}
			if restoreErr := restoreShim(shimPath, previous, existed); restoreErr != nil {
				rollbackErrors = append(rollbackErrors, restoreErr)
			}
			if len(rollbackErrors) > 0 {
				return errors.Join(append([]error{errRollback, err}, rollbackErrors...)...)
			}
			return err
		}
		broadcastEnvironmentChange()
		progress(progressUpdate{phase: "committing", progress: 99, message: "命令入口和用户 PATH 已更新"})
		return nil
	})
}

func (manager *Manager) download(ctx context.Context, item artifact, protocol proxyservice.Protocol, port int, destination string, progress func(progressUpdate)) error {
	client, err := manager.client(protocol, port)
	if err != nil || client == nil {
		return fmt.Errorf("%w: client unavailable", errDownload)
	}
	copyClient := *client
	copyClient.CheckRedirect = func(*http.Request, []*http.Request) error { return errors.New("artifact redirects are disabled") }
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, item.URL, nil)
	if err != nil {
		return fmt.Errorf("%w: request", errDownload)
	}
	request.Header.Set("Accept-Encoding", "identity")
	request.Header.Set("User-Agent", "Osverse-Windows-Installer")
	response, err := copyClient.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("%w: transport", errDownload)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || (response.ContentLength >= 0 && response.ContentLength != item.DownloadBytes) {
		return fmt.Errorf("%w: unexpected response", errDownload)
	}
	file, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	hash := sha256.New()
	reader := io.LimitReader(response.Body, item.DownloadBytes+1)
	written, err := copyContext(ctx, io.MultiWriter(file, hash), reader, func(total int64) {
		progress(progressUpdate{phase: "downloading", progress: 5 + int(total*60/item.DownloadBytes), message: "正在下载官方 Windows 制品"})
	})
	if err != nil {
		return err
	}
	if written != item.DownloadBytes || hex.EncodeToString(hash.Sum(nil)) != item.SHA256 {
		return errHashMismatch
	}
	return file.Sync()
}

func extractArchive(ctx context.Context, archivePath, destination string, expandedLimit int64) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
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
			return nil
		}
		if err != nil {
			return errUnsafeArchive
		}
		entries++
		if entries > maxArchiveEntries || !safeArchiveName(header.Name) {
			return errUnsafeArchive
		}
		target := filepath.Join(destination, filepath.FromSlash(path.Clean(header.Name)))
		if !within(destination, target) {
			return errUnsafeArchive
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o700); err != nil {
				return errUnsafeArchive
			}
		case tar.TypeReg, tar.TypeRegA:
			if header.Size < 0 || expanded > expandedLimit-header.Size {
				return errUnsafeArchive
			}
			expanded += header.Size
			if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
				return errUnsafeArchive
			}
			output, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
			if err != nil {
				return errUnsafeArchive
			}
			written, copyErr := copyContext(ctx, output, io.LimitReader(reader, header.Size), nil)
			closeErr := output.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil || written != header.Size {
				return errUnsafeArchive
			}
		default:
			return errUnsafeArchive
		}
	}
}

func safeArchiveName(name string) bool {
	cleanInput := strings.TrimSuffix(name, "/")
	if cleanInput == "" || !utf8.ValidString(name) || strings.ContainsAny(name, "\\:\x00") ||
		strings.HasPrefix(name, "/") || path.Clean(cleanInput) != cleanInput {
		return false
	}
	for _, component := range strings.Split(cleanInput, "/") {
		trimmed := strings.TrimRight(component, ". ")
		base := strings.ToUpper(strings.TrimSuffix(trimmed, path.Ext(trimmed)))
		if component == "" || component == "." || component == ".." || trimmed != component || reservedWindowsName(base) {
			return false
		}
	}
	return true
}

func reservedWindowsName(value string) bool {
	if value == "CON" || value == "PRN" || value == "AUX" || value == "NUL" {
		return true
	}
	return len(value) == 4 && (strings.HasPrefix(value, "COM") || strings.HasPrefix(value, "LPT")) && value[3] >= '1' && value[3] <= '9'
}

func validatePayloadBinary(root, binary string) error {
	if !within(root, binary) {
		return errUnsafeArchive
	}
	info, err := os.Lstat(binary)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return errUnsafeArchive
	}
	return nil
}

func (manager *Manager) verifyVersion(ctx context.Context, binary string, item artifact) error {
	result, err := manager.runner.Run(ctx, platform.CommandRequest{Path: binary, Args: item.VersionArgs, Timeout: versionTimeout, OutputLimit: versionOutput})
	output := result.Stdout + "\n" + result.Stderr
	if err != nil || result.ExitCode != 0 || result.TimedOut || result.Truncated || !strings.Contains(output, item.Version) {
		return errVersion
	}
	return nil
}

func ensureManagedDirectories(base string, components ...string) (string, error) {
	current := filepath.Clean(base)
	for _, component := range components {
		if component == "" || filepath.Base(component) != component || component == "." || component == ".." {
			return "", errors.New("invalid managed directory component")
		}
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			if err := os.Mkdir(current, 0o700); err != nil {
				return "", err
			}
			continue
		}
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("managed directory path is unsafe")
		}
	}
	return current, nil
}

func commitAndActivateWindowsPayload(home, payload, destination string, marker []byte, activate func() error) error {
	created, err := commitPayload(payload, destination, marker)
	if err != nil {
		return err
	}
	if err := activate(); err != nil {
		if created {
			if rollbackErr := removeCommittedWindowsPayload(home, destination, marker); rollbackErr != nil {
				return errors.Join(errRollback, err, rollbackErr)
			}
		}
		return err
	}
	return nil
}

func commitPayload(payload, destination string, marker []byte) (bool, error) {
	info, err := os.Lstat(destination)
	if err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return false, errors.New("managed version path is unsafe")
		}
		existing, readErr := os.ReadFile(filepath.Join(destination, ".osverse-artifact"))
		if readErr != nil || !bytes.Equal(existing, marker) {
			return false, errors.New("managed version identity mismatch")
		}
		matches, compareErr := payloadTreesMatch(payload, destination)
		if compareErr != nil || !matches {
			return false, errors.New("managed version contents mismatch")
		}
		return false, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	if err := os.Rename(payload, destination); err != nil {
		return false, err
	}
	return true, nil
}

func removeCommittedWindowsPayload(home, destination string, marker []byte) error {
	home, destination = filepath.Clean(home), filepath.Clean(destination)
	if !within(home, destination) {
		return errVersion
	}
	info, err := os.Lstat(destination)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errVersion
	}
	existing, err := os.ReadFile(filepath.Join(destination, ".osverse-artifact"))
	if err != nil || !bytes.Equal(existing, marker) {
		return errVersion
	}
	return os.RemoveAll(destination)
}

type payloadTreeEntry struct {
	directory bool
	size      int64
	hash      [sha256.Size]byte
}

func payloadTreesMatch(expectedRoot, actualRoot string) (bool, error) {
	expected, err := readPayloadTree(expectedRoot)
	if err != nil {
		return false, err
	}
	actual, err := readPayloadTree(actualRoot)
	if err != nil || len(expected) != len(actual) {
		return false, err
	}
	for name, expectedEntry := range expected {
		if actualEntry, ok := actual[name]; !ok || actualEntry != expectedEntry {
			return false, nil
		}
	}
	return true, nil
}

func readPayloadTree(root string) (map[string]payloadTreeEntry, error) {
	rootInfo, err := os.Lstat(root)
	if err != nil || !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 {
		return nil, errVersion
	}
	entries := make(map[string]payloadTreeEntry)
	err = filepath.Walk(root, func(current string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(root, current)
		if err != nil || filepath.IsAbs(relative) || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return errVersion
		}
		if relative == "." {
			return nil
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errVersion
		}
		if info.IsDir() {
			entries[relative] = payloadTreeEntry{directory: true}
			return nil
		}
		if !info.Mode().IsRegular() {
			return errVersion
		}
		file, err := os.Open(current)
		if err != nil {
			return err
		}
		openedInfo, err := file.Stat()
		if err != nil || !openedInfo.Mode().IsRegular() || !os.SameFile(info, openedInfo) {
			_ = file.Close()
			return errVersion
		}
		hash := sha256.New()
		if _, err := io.Copy(hash, file); err != nil {
			_ = file.Close()
			return err
		}
		if err := file.Close(); err != nil {
			return err
		}
		var digest [sha256.Size]byte
		copy(digest[:], hash.Sum(nil))
		entries[relative] = payloadTreeEntry{size: openedInfo.Size(), hash: digest}
		return nil
	})
	return entries, err
}

func managedShim(componentID, binary string) []byte {
	escaped := strings.ReplaceAll(binary, "%", "%%")
	return []byte(shimMarkerPrefix + componentID + "\r\n@echo off\r\nsetlocal DisableDelayedExpansion\r\n\"" + escaped + "\" %*\r\n")
}

func inspectShim(path, componentID, managedRoot string) ([]byte, bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() > 16*1024 {
		return nil, false, errExternalShim
	}
	content, err := os.ReadFile(path)
	if err != nil || !validManagedShim(content, componentID, managedRoot) {
		return nil, false, errExternalShim
	}
	return content, true, nil
}

func rejectConflictingAliases(binRoot, command string) error {
	for _, extension := range []string{"", ".com", ".exe", ".bat"} {
		path := filepath.Join(binRoot, command+extension)
		_, err := os.Lstat(path)
		if err == nil || !errors.Is(err, os.ErrNotExist) {
			return errExternalShim
		}
	}
	return nil
}

func validManagedShim(content []byte, componentID, managedRoot string) bool {
	lines := strings.Split(string(content), "\r\n")
	if len(lines) != 5 || lines[0] != shimMarkerPrefix+componentID || lines[1] != "@echo off" ||
		lines[2] != "setlocal DisableDelayedExpansion" || lines[4] != "" {
		return false
	}
	line := lines[3]
	if !strings.HasPrefix(line, `"`) || !strings.HasSuffix(line, `" %*`) {
		return false
	}
	target, ok := decodeShimPath(strings.TrimSuffix(strings.TrimPrefix(line, `"`), `" %*`))
	return ok && filepath.IsAbs(target) && within(managedRoot, filepath.Clean(target))
}

func decodeShimPath(value string) (string, bool) {
	var result strings.Builder
	result.Grow(len(value))
	for index := 0; index < len(value); index++ {
		if value[index] != '%' {
			result.WriteByte(value[index])
			continue
		}
		if index+1 >= len(value) || value[index+1] != '%' {
			return "", false
		}
		result.WriteByte('%')
		index++
	}
	return result.String(), true
}

func atomicReplace(destination string, content []byte) error {
	file, err := os.CreateTemp(filepath.Dir(destination), ".osverse-shim-")
	if err != nil {
		return err
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	if _, err := file.Write(content); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	sourcePath, err := xwindows.UTF16PtrFromString(temporary)
	if err != nil {
		return err
	}
	destinationPath, err := xwindows.UTF16PtrFromString(destination)
	if err != nil {
		return err
	}
	return xwindows.MoveFileEx(sourcePath, destinationPath, xwindows.MOVEFILE_REPLACE_EXISTING|xwindows.MOVEFILE_WRITE_THROUGH)
}

func restoreShim(path string, content []byte, existed bool) error {
	if !existed {
		err := os.Remove(path)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	return atomicReplace(path, content)
}

func ensureUserPath(binRoot string) (bool, string, uint32, error) {
	key, err := registry.OpenKey(registry.CURRENT_USER, `Environment`, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return false, "", 0, err
	}
	defer key.Close()
	current, valueType, err := key.GetStringValue("Path")
	if errors.Is(err, registry.ErrNotExist) {
		current, valueType, err = "", registry.SZ, nil
	}
	if err != nil {
		return false, "", 0, err
	}
	if valueType != registry.SZ && valueType != registry.EXPAND_SZ {
		return false, "", 0, errors.New("unsupported user PATH registry type")
	}
	for _, entry := range filepath.SplitList(current) {
		candidate := strings.TrimSpace(entry)
		candidate = strings.ReplaceAll(candidate, "%USERPROFILE%", filepath.Dir(filepath.Dir(binRoot)))
		candidate = strings.ReplaceAll(candidate, "%userprofile%", filepath.Dir(filepath.Dir(binRoot)))
		if strings.EqualFold(filepath.Clean(candidate), binRoot) {
			return false, current, valueType, nil
		}
	}
	next := strings.TrimSpace(current)
	if next != "" && !strings.HasSuffix(next, ";") {
		next += ";"
	}
	next += binRoot
	if len(next) > 32767 {
		return false, "", 0, errors.New("user PATH is too long")
	}
	if err := setRegistryPath(key, next, valueType); err != nil {
		return false, "", 0, err
	}
	return true, current, valueType, nil
}

func restoreUserPath(value string, valueType uint32) error {
	key, err := registry.OpenKey(registry.CURRENT_USER, `Environment`, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()
	return setRegistryPath(key, value, valueType)
}

func setRegistryPath(key registry.Key, value string, valueType uint32) error {
	if valueType == registry.EXPAND_SZ {
		return key.SetExpandStringValue("Path", value)
	}
	return key.SetStringValue("Path", value)
}

func broadcastEnvironmentChange() {
	user32 := xwindows.NewLazySystemDLL("user32.dll")
	proc := user32.NewProc("SendMessageTimeoutW")
	name, err := xwindows.UTF16PtrFromString("Environment")
	if err != nil {
		return
	}
	const (
		hwndBroadcast   = 0xffff
		wmSettingChange = 0x001a
		smtoAbortIfHung = 0x0002
	)
	var result uintptr
	_, _, _ = proc.Call(hwndBroadcast, wmSettingChange, 0, uintptr(unsafe.Pointer(name)), smtoAbortIfHung, 2000, uintptr(unsafe.Pointer(&result)))
}

func copyContext(ctx context.Context, destination io.Writer, source io.Reader, progress func(int64)) (int64, error) {
	buffer := make([]byte, 128*1024)
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		count, readErr := source.Read(buffer)
		if count > 0 {
			written, writeErr := destination.Write(buffer[:count])
			total += int64(written)
			if writeErr != nil {
				return total, writeErr
			}
			if written != count {
				return total, io.ErrShortWrite
			}
			if progress != nil {
				progress(total)
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

func within(root, target string) bool {
	relative, err := filepath.Rel(root, target)
	return err == nil && relative != "." && !filepath.IsAbs(relative) && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func publicFailure(err error) string {
	switch {
	case errors.Is(err, errExternalShim):
		return "命令入口已被其他程序占用，未修改现有安装"
	case errors.Is(err, errHashMismatch):
		return "下载文件校验失败，未安装"
	case errors.Is(err, errRollback):
		return "安装未完成且回滚失败，请刷新扫描确认残留状态"
	case errors.Is(err, errVersion):
		return "工具版本验证失败，未安装"
	case errors.Is(err, errUnsafeArchive):
		return "安装包内容不安全，已拒绝安装"
	case errors.Is(err, errDownload):
		return "下载失败，请检查网络或代理"
	default:
		return "安装失败，原命令未改变"
	}
}
