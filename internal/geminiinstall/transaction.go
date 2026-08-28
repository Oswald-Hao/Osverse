package geminiinstall

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
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Oswald-Hao/Osverse/internal/managedcommand"
	proxyservice "github.com/Oswald-Hao/Osverse/internal/proxy"
)

const versionTimeout = 30 * time.Second

var (
	errDownload        = errors.New("Gemini CLI download failed")
	errHashMismatch    = errors.New("Gemini CLI artifact hash mismatch")
	errVersion         = errors.New("Gemini CLI version verification failed")
	errExternalCommand = errors.New("Gemini CLI command is owned by another program")
	errRollback        = errors.New("Gemini CLI activation rollback failed")
)

type managedPaths struct {
	root, stagingRoot, toolRoot, finalRoot, currentPath, binRoot, shimPath, wrapperPath string
}

func (manager *Manager) execute(ctx context.Context, stored storedPlan, protocol proxyservice.Protocol, port int, progress func(string, int, string)) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	paths := managedPathsFor(manager.home, manager.goos, geminiVersion)
	for _, directory := range []string{paths.root, paths.stagingRoot, paths.toolRoot, paths.binRoot} {
		if err := ensureManagedDirectory(manager.home, directory); err != nil {
			return err
		}
	}
	commandPaths := managedcommand.Paths{
		ToolRoot: paths.toolRoot, CurrentPath: paths.currentPath, BinRoot: paths.binRoot,
		ShimPath: paths.shimPath, WrapperPath: paths.wrapperPath,
	}
	if err := managedcommand.Inspect(manager.home, componentID, commandName, commandPaths); err != nil {
		if errors.Is(err, managedcommand.ErrExternalCommand) {
			return errExternalCommand
		}
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
	client, err := manager.client(protocol, port)
	if err != nil || client == nil {
		return fmt.Errorf("%w: HTTP client unavailable", errDownload)
	}
	total := stored.runtime.Size + stored.pack.Size
	runtimeArchive := filepath.Join(staging, "node-runtime."+stored.runtime.Format)
	progress("downloading", 4, "正在下载固定 Node.js 运行时")
	if err := download(ctx, client, stored.runtime.URL, stored.runtime.SHA256, stored.runtime.Size, runtimeArchive, func(done int64) {
		progress("downloading", 4+int(done*70/total), "正在下载并校验 Node.js 运行时")
	}); err != nil {
		return err
	}
	packageArchive := filepath.Join(staging, "gemini-cli.tgz")
	if err := download(ctx, client, stored.pack.URL, stored.pack.SHA256, stored.pack.Size, packageArchive, func(done int64) {
		progress("downloading", 4+int((stored.runtime.Size+done)*70/total), "正在下载并校验 Gemini CLI")
	}); err != nil {
		return err
	}
	payload := filepath.Join(staging, "payload")
	if err := os.Mkdir(payload, 0o700); err != nil {
		return err
	}
	progress("verifying", 76, "正在安全展开固定运行时")
	node := filepath.Join(payload, "runtime", "bin", "node")
	if manager.goos == "windows" {
		node = filepath.Join(payload, "runtime", "node.exe")
	}
	if err := extractRuntime(ctx, stored.runtime, runtimeArchive, node); err != nil {
		return archiveError("Node.js", err)
	}
	if err := extractPackage(ctx, packageArchive, filepath.Join(payload, "app")); err != nil {
		return archiveError("Gemini CLI", err)
	}
	if err := writeWrapper(payload, paths.finalRoot, manager.goos); err != nil {
		return err
	}
	progress("verifying", 90, "正在验证 Gemini CLI 版本")
	if err := verifyPayload(ctx, payload, manager.goos); err != nil {
		return err
	}
	marker := []byte(fmt.Sprintf(
		"component=%s\nversion=%s\nnode=%s\ntarget=%s/%s\npackage_sha256=%s\nnode_sha256=%s\n",
		componentID, geminiVersion, nodeVersion, manager.goos, manager.goarch, stored.pack.SHA256, stored.runtime.SHA256,
	))
	if err := os.WriteFile(filepath.Join(payload, ".osverse-gemini-runtime"), marker, 0o600); err != nil {
		return err
	}
	progress("committing", 95, "正在原子切换 gemini 命令入口")
	activate := func() error {
		err := managedcommand.Activate(manager.home, componentID, commandName, geminiVersion, commandPaths)
		if errors.Is(err, managedcommand.ErrExternalCommand) {
			return errExternalCommand
		}
		return err
	}
	if err := commitAndActivate(manager.home, payload, paths, marker, manager.goos, activate); err != nil {
		return err
	}
	progress("committing", 99, "gemini 命令入口已更新")
	return nil
}

func writeWrapper(payload, finalRoot, goos string) error {
	directory := filepath.Join(payload, "bin")
	if err := os.Mkdir(directory, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	path := filepath.Join(directory, commandName)
	node := filepath.Join(finalRoot, "runtime", "bin", "node")
	script := filepath.Join(finalRoot, "app", "package", "bundle", "gemini.js")
	content := "#!/bin/sh\nset -eu\nexec " + shellQuote(node) + " " + shellQuote(script) + " \"$@\"\n"
	mode := os.FileMode(0o700)
	if goos == "windows" {
		path += ".cmd"
		content = "@echo off\r\nsetlocal DisableDelayedExpansion\r\n\"%~dp0..\\runtime\\node.exe\" \"%~dp0..\\app\\package\\bundle\\gemini.js\" %*\r\nexit /b %ERRORLEVEL%\r\n"
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
		return errors.New("Gemini CLI managed path escapes user home")
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
			return errors.New("invalid Gemini CLI managed path")
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
			return errors.New("Gemini CLI managed directory is unsafe")
		}
	}
	return nil
}

func download(ctx context.Context, client *http.Client, rawURL, wantHash string, size int64, destination string, progress func(int64)) error {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Port() != "" || parsed.RawQuery != "" || parsed.Fragment != "" ||
		(parsed.Hostname() != "nodejs.org" && parsed.Hostname() != "registry.npmjs.org") {
		return errDownload
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return errDownload
	}
	request.Header.Set("Accept-Encoding", "identity")
	request.Header.Set("User-Agent", "Osverse-Gemini-Installer")
	copyClient := *client
	copyClient.CheckRedirect = func(*http.Request, []*http.Request) error {
		return errors.New("Gemini artifact redirects are disabled")
	}
	response, err := copyClient.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errDownload
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || (response.ContentLength >= 0 && response.ContentLength != size) {
		return errDownload
	}
	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	hash := sha256.New()
	written, copyErr := copyWithProgress(ctx, io.MultiWriter(output, hash), io.LimitReader(response.Body, size+1), progress)
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if written != size || hex.EncodeToString(hash.Sum(nil)) != wantHash {
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

func verifyPayload(ctx context.Context, payload, goos string) error {
	manifestPath := filepath.Join(payload, "app", "package", "package.json")
	info, err := os.Lstat(manifestPath)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() > 1024*1024 {
		return errVersion
	}
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		return errVersion
	}
	var manifest struct {
		Name, Version, License string
		Bin                    map[string]string `json:"bin"`
		Engines                map[string]string `json:"engines"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil || manifest.Name != "@google/gemini-cli" ||
		manifest.Version != geminiVersion || manifest.License != "Apache-2.0" ||
		manifest.Bin[commandName] != "bundle/gemini.js" || manifest.Engines["node"] != ">=20" {
		return errVersion
	}
	node := filepath.Join(payload, "runtime", "bin", "node")
	if goos == "windows" {
		node = filepath.Join(payload, "runtime", "node.exe")
	}
	script := filepath.Join(payload, "app", "package", "bundle", "gemini.js")
	for _, path := range []string{node, script} {
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return errVersion
		}
	}
	verifyContext, cancel := context.WithTimeout(ctx, versionTimeout)
	defer cancel()
	command := exec.CommandContext(verifyContext, node, script, "--version")
	command.Env = append(os.Environ(), "NO_COLOR=1", "CI=1")
	output, err := command.CombinedOutput()
	if err != nil || len(output) > 4096 || strings.TrimSpace(string(output)) != geminiVersion {
		return errVersion
	}
	return nil
}

func commitAndActivate(home, payload string, paths managedPaths, marker []byte, goos string, activate func() error) error {
	created, quarantined, err := commitPayload(home, payload, paths, marker, goos)
	if err != nil {
		return err
	}
	if err := activate(); err != nil {
		if created {
			if rollbackErr := removeCommittedPayload(home, paths.finalRoot, marker); rollbackErr != nil {
				return errors.Join(errRollback, err, rollbackErr)
			}
		}
		if quarantined != "" {
			if rollbackErr := commitRename(quarantined, paths.finalRoot); rollbackErr != nil {
				return errors.Join(errRollback, err, rollbackErr)
			}
		}
		return err
	}
	return nil
}

func commitPayload(home, payload string, paths managedPaths, marker []byte, goos string) (bool, string, error) {
	info, err := os.Lstat(paths.finalRoot)
	if errors.Is(err, os.ErrNotExist) {
		if err := commitRename(payload, paths.finalRoot); err != nil {
			return false, "", err
		}
		return true, "", nil
	}
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return false, "", errVersion
	}
	existing, err := os.ReadFile(filepath.Join(paths.finalRoot, ".osverse-gemini-runtime"))
	if err != nil || !bytes.Equal(existing, marker) {
		return false, "", errVersion
	}
	damaged := false
	for _, relative := range criticalPaths(goos) {
		equal, compareErr := sameRegularFile(filepath.Join(payload, filepath.FromSlash(relative)), filepath.Join(paths.finalRoot, filepath.FromSlash(relative)))
		if compareErr != nil || !equal {
			damaged = true
			break
		}
	}
	if !damaged {
		return false, "", nil
	}
	recoveryRoot := filepath.Join(paths.root, "recovery")
	if err := ensureManagedDirectory(home, recoveryRoot); err != nil {
		return false, "", err
	}
	quarantined, err := os.MkdirTemp(recoveryRoot, "install-"+componentID+"-")
	if err != nil {
		return false, "", err
	}
	if err := os.Remove(quarantined); err != nil {
		return false, "", err
	}
	if err := commitRename(paths.finalRoot, quarantined); err != nil {
		return false, "", err
	}
	if err := commitRename(payload, paths.finalRoot); err != nil {
		if restoreErr := commitRename(quarantined, paths.finalRoot); restoreErr != nil {
			return false, "", errors.Join(errRollback, err, restoreErr)
		}
		return false, "", err
	}
	return true, quarantined, nil
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
	existing, err := os.ReadFile(filepath.Join(destination, ".osverse-gemini-runtime"))
	if err != nil || !bytes.Equal(existing, marker) {
		return errVersion
	}
	return os.RemoveAll(destination)
}

func criticalPaths(goos string) []string {
	if goos == "windows" {
		return []string{"runtime/node.exe", "app/package/package.json", "app/package/bundle/gemini.js", "bin/gemini.cmd"}
	}
	return []string{"runtime/bin/node", "app/package/package.json", "app/package/bundle/gemini.js", "bin/gemini"}
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
		return "gemini 命令入口已被其他程序占用，未修改原安装"
	case errors.Is(err, errHashMismatch):
		return "Gemini CLI 下载文件校验失败，未安装"
	case errors.Is(err, errUnsafeArchive):
		return "Gemini CLI 安装包内容不安全，已拒绝安装"
	case errors.Is(err, errRollback):
		return "Gemini CLI 安装未完成且回滚失败，请刷新扫描确认残留状态"
	case errors.Is(err, errVersion):
		return "Gemini CLI 版本验证失败，未安装"
	case errors.Is(err, errDownload):
		return "Gemini CLI 下载失败，请检查网络或代理"
	default:
		return "Gemini CLI 安装失败，原版本未改变"
	}
}
