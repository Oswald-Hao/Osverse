package kimiinstall

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	errDownload        = errors.New("Kimi Code download failed")
	errHashMismatch    = errors.New("Kimi Code artifact hash mismatch")
	errVersion         = errors.New("Kimi Code version verification failed")
	errExternalCommand = errors.New("kimi command is owned by another program")
)

type managedPaths struct {
	root, stagingRoot, toolRoot, finalRoot, binRoot, shimPath, wrapperPath, binaryPath string
}

func (manager *Manager) execute(ctx context.Context, stored storedPlan, protocol proxyservice.Protocol, port int, progress func(string, int, string)) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	paths := managedPathsFor(manager.home, manager.goos, kimiVersion)
	for _, directory := range []string{paths.root, paths.stagingRoot, paths.toolRoot, paths.binRoot} {
		if err := ensureManagedDirectory(manager.home, directory); err != nil {
			return err
		}
	}
	if err := inspectCommandEntry(paths); err != nil {
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
	archive := filepath.Join(staging, "kimi."+stored.artifact.Format)
	payload := filepath.Join(staging, "payload")
	client, err := manager.client(protocol, port)
	if err != nil || client == nil {
		return errDownload
	}
	progress("downloading", 4, "正在下载 Kimi Code 独立运行时")
	if err := downloadArtifact(ctx, client, stored.artifact, archive, func(done int64) {
		progress("downloading", 4+int(done*68/stored.artifact.Size), "正在下载并校验 Kimi Code")
	}); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(payload, "bin"), 0o700); err != nil {
		return err
	}
	binary := filepath.Join(payload, "bin", "kimi.real")
	if manager.goos == "windows" {
		binary += ".exe"
	}
	progress("verifying", 74, "正在安全解压 Kimi Code")
	if err := extractArtifact(ctx, stored.artifact, archive, binary); err != nil {
		return err
	}
	progress("verifying", 88, "正在验证 Kimi Code 版本")
	if err := verifyKimi(ctx, binary); err != nil {
		return err
	}
	if err := writeWrapper(payload, paths.finalRoot, manager.goos); err != nil {
		return err
	}
	marker := []byte(fmt.Sprintf("component=%s\nversion=%s\ntarget=%s/%s\nsha256=%s\n", componentID, kimiVersion, manager.goos, manager.goarch, stored.artifact.SHA256))
	if err := os.WriteFile(filepath.Join(payload, ".osverse-kimi-runtime"), marker, 0o600); err != nil {
		return err
	}
	progress("committing", 94, "正在原子切换 kimi 命令入口")
	created, err := commitPayload(payload, paths.finalRoot, marker, manager.goos)
	if err != nil {
		return err
	}
	if err := activateCommand(manager.home, paths); err != nil {
		if created {
			_ = removeCommittedPayload(manager.home, paths.finalRoot, marker)
		}
		return err
	}
	progress("committing", 99, "kimi 命令入口已更新")
	return nil
}

func downloadArtifact(ctx context.Context, client *http.Client, item artifact, destination string, progress func(int64)) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, item.URL, nil)
	if err != nil {
		return errDownload
	}
	request.Header.Set("Accept-Encoding", "identity")
	request.Header.Set("User-Agent", "Osverse-Kimi-Installer")
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
	written, copyErr := copyWithProgress(ctx, io.MultiWriter(output, hash), io.LimitReader(response.Body, item.Size+1), progress)
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
	var total int64
	buffer := make([]byte, 128*1024)
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

func extractArtifact(ctx context.Context, item artifact, archive, binary string) error {
	file, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer file.Close()
	if item.Format != "zip" {
		return errUnsafeArchive
	}
	info, err := file.Stat()
	if err != nil {
		return err
	}
	expected := "kimi"
	if item.GOOS == "windows" {
		expected = "kimi.exe"
	}
	return extractKimiZip(ctx, file, info.Size(), binary, expected)
}

func verifyKimi(ctx context.Context, binary string) error {
	info, err := os.Lstat(binary)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return errVersion
	}
	verifyContext, cancel := context.WithTimeout(ctx, versionTimeout)
	defer cancel()
	command := exec.CommandContext(verifyContext, binary, "--version")
	command.Env = append(os.Environ(), "NO_COLOR=1", "CI=1", "KIMI_CODE_NO_AUTO_UPDATE=1")
	output, err := command.CombinedOutput()
	if err != nil || len(output) > 4096 || strings.TrimSpace(string(output)) != kimiVersion {
		return errVersion
	}
	return nil
}

func writeWrapper(payload, finalRoot, goos string) error {
	path := filepath.Join(payload, "bin", "kimi")
	real := filepath.Join(finalRoot, "bin", "kimi.real")
	content := "#!/bin/sh\nset -eu\nexport KIMI_CODE_NO_AUTO_UPDATE=1\nexec " + shellQuote(real) + " \"$@\"\n"
	mode := os.FileMode(0o700)
	if goos == "windows" {
		path += ".cmd"
		content = "@echo off\r\nset \"KIMI_CODE_NO_AUTO_UPDATE=1\"\r\ncall \"%~dp0kimi.real.exe\" %*\r\nexit /b %ERRORLEVEL%\r\n"
		mode = 0o600
	}
	return os.WriteFile(path, []byte(content), mode)
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func commitPayload(payload, destination string, marker []byte, goos string) (bool, error) {
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
	existing, err := os.ReadFile(filepath.Join(destination, ".osverse-kimi-runtime"))
	if err != nil || !bytes.Equal(existing, marker) {
		return false, errVersion
	}
	for _, relative := range criticalPaths(goos) {
		equal, err := sameRegularFile(filepath.Join(payload, relative), filepath.Join(destination, relative))
		if err != nil || !equal {
			return false, errVersion
		}
	}
	return false, nil
}

func removeCommittedPayload(home, destination string, marker []byte) error {
	home, destination = filepath.Clean(home), filepath.Clean(destination)
	if !pathWithin(home, destination) || destination == home {
		return errVersion
	}
	info, err := os.Lstat(destination)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errVersion
	}
	existing, err := os.ReadFile(filepath.Join(destination, ".osverse-kimi-runtime"))
	if err != nil || !bytes.Equal(existing, marker) {
		return errVersion
	}
	return os.RemoveAll(destination)
}

func criticalPaths(goos string) []string {
	if goos == "windows" {
		return []string{"bin/kimi.real.exe", "bin/kimi.cmd"}
	}
	return []string{"bin/kimi.real", "bin/kimi"}
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
	left := sha256.New()
	right := sha256.New()
	if _, err := io.Copy(left, expected); err != nil {
		return false, err
	}
	if _, err := io.Copy(right, actual); err != nil {
		return false, err
	}
	return bytes.Equal(left.Sum(nil), right.Sum(nil)), nil
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

func ensureManagedDirectory(home, destination string) error {
	home, destination = filepath.Clean(home), filepath.Clean(destination)
	if !pathWithin(home, destination) && destination != home {
		return errors.New("Kimi managed path escapes user home")
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
			return errors.New("invalid Kimi managed path")
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
			return errors.New("Kimi managed directory is unsafe")
		}
	}
	return nil
}

func pathWithin(root, candidate string) bool {
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(candidate))
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}

func publicFailure(err error) string {
	switch {
	case errors.Is(err, errExternalCommand):
		return "kimi 命令入口已被其他程序占用，未修改原安装"
	case errors.Is(err, errHashMismatch):
		return "Kimi Code 下载文件校验失败，未安装"
	case errors.Is(err, errUnsafeArchive):
		return "Kimi Code 安装包内容不安全，已拒绝安装"
	case errors.Is(err, errVersion):
		return "Kimi Code 版本验证失败，未安装"
	case errors.Is(err, errDownload):
		return "Kimi Code 下载失败，请检查网络或代理"
	default:
		return "Kimi Code 安装失败，原版本未改变"
	}
}
