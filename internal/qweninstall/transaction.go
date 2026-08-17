package qweninstall

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	proxyservice "github.com/Oswald-Hao/Osverse/internal/proxy"
	"github.com/Oswald-Hao/Osverse/internal/releaseasset"
)

const versionTimeout = 30 * time.Second

var (
	errDownload        = errors.New("Qwen Code download failed")
	errHashMismatch    = errors.New("Qwen Code artifact hash mismatch")
	errVersion         = errors.New("Qwen Code version verification failed")
	errExternalCommand = errors.New("Qwen Code command is owned by another program")
	errRollback        = errors.New("Qwen Code activation rollback failed")
)

type managedPaths struct {
	root, stagingRoot, toolRoot, finalRoot, currentPath, binRoot, shimPath, wrapperPath string
}

func (manager *Manager) execute(ctx context.Context, stored storedPlan, protocol proxyservice.Protocol, port int, progress func(string, int, string)) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	paths := managedPathsFor(manager.home, manager.goos, qwenVersion)
	for _, directory := range []string{paths.root, paths.stagingRoot, paths.toolRoot, paths.binRoot} {
		if err := ensureManagedDirectory(manager.home, directory); err != nil {
			return err
		}
	}
	if err := inspectCommandEntry(paths, manager.goos); err != nil {
		return err
	}
	staging, err := os.MkdirTemp(paths.stagingRoot, componentID+"-")
	if err != nil {
		return err
	}
	if err := os.Chmod(staging, 0o700); err != nil {
		_ = os.RemoveAll(staging)
		return err
	}
	defer os.RemoveAll(staging)
	archive := filepath.Join(staging, "qwen-code."+stored.artifact.Format)
	payload := filepath.Join(staging, "payload")
	client, err := manager.client(protocol, port)
	if err != nil || client == nil {
		return fmt.Errorf("%w: HTTP client unavailable", errDownload)
	}
	progress("downloading", 4, "正在下载 Qwen Code 独立运行时")
	if err := downloadArtifact(ctx, client, stored.artifact, archive, func(done int64) {
		progress("downloading", 4+int(done*68/stored.artifact.Size), "正在下载并校验 Qwen Code")
	}); err != nil {
		return err
	}
	if err := os.Mkdir(payload, 0o700); err != nil {
		return err
	}
	progress("verifying", 74, "正在安全解压 Qwen Code")
	if err := extractArtifact(ctx, stored.artifact, archive, payload); err != nil {
		return err
	}
	progress("verifying", 88, "正在验证 Qwen Code 版本")
	if err := verifyQwenPayload(ctx, payload, manager.goos, manager.goarch); err != nil {
		return err
	}
	if err := writeQwenWrapper(payload, paths.finalRoot, manager.goos); err != nil {
		return err
	}
	marker := []byte(fmt.Sprintf("component=%s\nversion=%s\ntarget=%s/%s\nsha256=%s\n", componentID, qwenVersion, manager.goos, manager.goarch, stored.artifact.SHA256))
	if err := os.WriteFile(filepath.Join(payload, ".osverse-qwen-runtime"), marker, 0o600); err != nil {
		return err
	}
	progress("committing", 94, "正在原子切换 qwen 命令入口")
	if err := commitAndActivatePayload(manager.home, payload, paths, marker, manager.goos, activateCommand); err != nil {
		return err
	}
	progress("committing", 99, "qwen 命令入口已更新")
	return nil
}

func writeQwenWrapper(payload, finalRoot, goos string) error {
	directory := filepath.Join(payload, "bin")
	if err := os.Mkdir(directory, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	path := filepath.Join(directory, "qwen")
	upstream := filepath.Join(finalRoot, "qwen-code", "bin", "qwen")
	content := "#!/bin/sh\nset -eu\nexec " + shellQuote(upstream) + " \"$@\"\n"
	mode := os.FileMode(0o700)
	if goos == "windows" {
		path += ".cmd"
		content = "@echo off\r\ncall \"%~dp0..\\qwen-code\\bin\\qwen.cmd\" %*\r\nexit /b %ERRORLEVEL%\r\n"
		mode = 0o600
	}
	return os.WriteFile(path, []byte(content), mode)
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func ensureManagedDirectory(home, destination string) error {
	home, destination = filepath.Clean(home), filepath.Clean(destination)
	if !pathWithin(home, destination) && destination != home {
		return errors.New("Qwen Code managed path escapes user home")
	}
	relative, err := filepath.Rel(home, destination)
	if err != nil {
		return err
	}
	current := home
	for _, component := range strings.Split(relative, string(filepath.Separator)) {
		if component == "" || component == "." {
			continue
		}
		if component == ".." || filepath.Base(component) != component {
			return errors.New("invalid Qwen Code managed path")
		}
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		switch {
		case errors.Is(err, os.ErrNotExist):
			if err := os.Mkdir(current, 0o700); err != nil {
				return err
			}
		case err != nil:
			return err
		case !info.IsDir() || info.Mode()&os.ModeSymlink != 0:
			return errors.New("Qwen Code managed directory is unsafe")
		}
	}
	return nil
}

func downloadArtifact(ctx context.Context, client *http.Client, item artifact, destination string, progress func(int64)) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, item.URL, nil)
	if err != nil {
		return errDownload
	}
	request.Header.Set("Accept-Encoding", "identity")
	request.Header.Set("User-Agent", "Osverse-Qwen-Installer")
	copyClient := *client
	copyClient.CheckRedirect = releaseasset.GitHubRedirectPolicy(item.URL)
	response, err := copyClient.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errDownload
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || (response.ContentLength >= 0 && response.ContentLength != item.Size) {
		return errDownload
	}
	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	hash := sha256.New()
	reader := io.LimitReader(response.Body, item.Size+1)
	written, copyErr := copyWithProgress(ctx, io.MultiWriter(output, hash), reader, progress)
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if written != item.Size || hex.EncodeToString(hash.Sum(nil)) != item.SHA256 {
		return errHashMismatch
	}
	return nil
}

func copyWithProgress(ctx context.Context, destination io.Writer, source io.Reader, progress func(int64)) (int64, error) {
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
			if progress != nil {
				progress(total)
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

func extractArtifact(ctx context.Context, item artifact, archive, destination string) error {
	file, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer file.Close()
	if item.Format == "zip" {
		info, err := file.Stat()
		if err != nil {
			return err
		}
		return extractQwenZip(ctx, file, info.Size(), destination)
	}
	return extractQwenTar(ctx, file, destination)
}

func verifyQwenPayload(ctx context.Context, payload, goos, goarch string) error {
	root := filepath.Join(payload, "qwen-code")
	manifestPath := filepath.Join(root, "package.json")
	info, err := os.Lstat(manifestPath)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() > 1024*1024 {
		return errVersion
	}
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		return errVersion
	}
	var manifest struct {
		Name, Version     string
		OsverseStandalone *struct {
			Target string `json:"target"`
		} `json:"osverseStandalone"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil || manifest.Name != "@qwen-code/qwen-code" || manifest.Version != qwenVersion {
		return errVersion
	}
	node := filepath.Join(root, "node", "bin", "node")
	entry := filepath.Join(root, "lib", "cli-entry.js")
	launcher := filepath.Join(root, "bin", "qwen")
	if goos == "windows" {
		node = filepath.Join(root, "node", "node.exe")
		launcher = filepath.Join(root, "bin", "qwen.cmd")
	}
	for _, path := range []string{node, entry, launcher} {
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return errVersion
		}
	}
	_ = goarch
	verifyContext, cancel := context.WithTimeout(ctx, versionTimeout)
	defer cancel()
	command := exec.CommandContext(verifyContext, node, entry, "--version")
	command.Env = append(os.Environ(), "NO_COLOR=1", "CI=1")
	output, err := command.CombinedOutput()
	if err != nil || len(output) > 4096 || strings.TrimSpace(string(output)) != qwenVersion {
		return errVersion
	}
	return nil
}

func commitAndActivatePayload(home, payload string, paths managedPaths, marker []byte, goos string, activate func(string, managedPaths, string) error) error {
	created, err := commitPayload(payload, paths.finalRoot, goos)
	if err != nil {
		return err
	}
	if err := activate(home, paths, goos); err != nil {
		if created {
			if rollbackErr := removeCommittedPayload(home, paths.finalRoot, ".osverse-qwen-runtime", marker); rollbackErr != nil {
				return errors.Join(errRollback, err, rollbackErr)
			}
		}
		return err
	}
	return nil
}

func commitPayload(payload, destination, goos string) (bool, error) {
	marker, err := os.ReadFile(filepath.Join(payload, ".osverse-qwen-runtime"))
	if err != nil {
		return false, err
	}
	info, err := os.Lstat(destination)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.Rename(payload, destination); err != nil {
			return false, err
		}
		return true, nil
	}
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return false, errVersion
	}
	existing, err := os.ReadFile(filepath.Join(destination, ".osverse-qwen-runtime"))
	if err != nil || !bytes.Equal(existing, marker) {
		return false, errVersion
	}
	for _, relative := range criticalPaths(goos) {
		equal, err := sameRegularFile(filepath.Join(payload, filepath.FromSlash(relative)), filepath.Join(destination, filepath.FromSlash(relative)))
		if err != nil || !equal {
			return false, errVersion
		}
	}
	return false, nil
}

func removeCommittedPayload(home, destination, markerName string, marker []byte) error {
	home, destination = filepath.Clean(home), filepath.Clean(destination)
	if !pathWithin(home, destination) || destination == home {
		return errVersion
	}
	info, err := os.Lstat(destination)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errVersion
	}
	existing, err := os.ReadFile(filepath.Join(destination, markerName))
	if err != nil || !bytes.Equal(existing, marker) {
		return errVersion
	}
	return os.RemoveAll(destination)
}

func criticalPaths(goos string) []string {
	if goos == "windows" {
		return []string{"qwen-code/node/node.exe", "qwen-code/lib/cli-entry.js", "qwen-code/bin/qwen.cmd", "bin/qwen.cmd"}
	}
	return []string{"qwen-code/node/bin/node", "qwen-code/lib/cli-entry.js", "qwen-code/bin/qwen", "bin/qwen"}
}

func sameRegularFile(expectedPath, actualPath string) (bool, error) {
	expected, expectedInfo, err := openVerifiedRegular(expectedPath)
	if err != nil {
		return false, err
	}
	defer expected.Close()
	actual, actualInfo, err := openVerifiedRegular(actualPath)
	if err != nil {
		return false, err
	}
	defer actual.Close()
	if expectedInfo.Size() != actualInfo.Size() {
		return false, nil
	}
	expectedHash, err := streamSHA256(expected)
	if err != nil {
		return false, err
	}
	actualHash, err := streamSHA256(actual)
	return err == nil && expectedHash == actualHash, err
}

func openVerifiedRegular(path string) (*os.File, os.FileInfo, error) {
	pathInfo, err := os.Lstat(path)
	if err != nil || !pathInfo.Mode().IsRegular() || pathInfo.Mode()&os.ModeSymlink != 0 {
		return nil, nil, errVersion
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	fileInfo, err := file.Stat()
	if err != nil || !fileInfo.Mode().IsRegular() || !os.SameFile(pathInfo, fileInfo) {
		_ = file.Close()
		return nil, nil, errVersion
	}
	return file, fileInfo, nil
}

func streamSHA256(reader io.Reader) ([sha256.Size]byte, error) {
	var result [sha256.Size]byte
	hash := sha256.New()
	if _, err := io.Copy(hash, reader); err != nil {
		return result, err
	}
	copy(result[:], hash.Sum(nil))
	return result, nil
}

func publicFailure(err error) string {
	switch {
	case errors.Is(err, errExternalCommand):
		return "qwen 命令入口已被其他程序占用，未修改原安装"
	case errors.Is(err, errHashMismatch):
		return "Qwen Code 下载文件校验失败，未安装"
	case errors.Is(err, errUnsafeArchive):
		return "Qwen Code 安装包内容不安全，已拒绝安装"
	case errors.Is(err, errRollback):
		return "Qwen Code 安装未完成且回滚失败，请刷新扫描确认残留状态"
	case errors.Is(err, errVersion):
		return "Qwen Code 版本验证失败，未安装"
	case errors.Is(err, errDownload):
		return "Qwen Code 下载失败，请检查网络或代理"
	default:
		return "Qwen Code 安装失败，原版本未改变"
	}
}
