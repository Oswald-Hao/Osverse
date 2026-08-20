package harnessinstall

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	proxyservice "github.com/Oswald-Hao/Osverse/internal/proxy"
)

const versionTimeout = 20 * time.Second

type managedPaths struct {
	root        string
	stagingRoot string
	toolRoot    string
	finalRoot   string
	currentPath string
	binRoot     string
	shimPath    string
	wrapperPath string
}

func (manager *Manager) execute(
	ctx context.Context,
	_ storedPlan,
	protocol proxyservice.Protocol,
	port int,
	progress func(string, int, string),
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	paths := managedPathsFor(manager.home, manager.goos, harnessVer)
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
	payload := filepath.Join(staging, "payload")
	client, err := manager.client(protocol, port)
	if err != nil || client == nil {
		return fmt.Errorf("%w: HTTP client unavailable", errDownload)
	}
	progress("downloading", 4, "正在准备 DeepSeek Harness 运行时")
	if err := buildPayload(ctx, client, manager.goos, manager.goarch, staging, payload, func(done, total int, message string) {
		value := 4
		if total > 0 {
			value += done * 78 / total
		}
		progress("downloading", value, message)
	}); err != nil {
		return err
	}
	progress("verifying", 84, "正在验证 DeepSeek Harness")
	if err := writeHarnessWrapper(payload, manager.goos, paths.finalRoot); err != nil {
		return err
	}
	if err := verifyHarness(ctx, payload, manager.goos); err != nil {
		return err
	}
	progress("committing", 92, "正在原子切换 dsh 命令入口")
	if err := commitAndActivateHarnessPayload(manager.home, payload, paths, manager.goos, activateHarnessCommand); err != nil {
		return err
	}
	progress("committing", 99, "dsh 命令入口已更新")
	return nil
}

func ensureManagedDirectory(home, destination string) error {
	home = filepath.Clean(home)
	destination = filepath.Clean(destination)
	if !pathWithin(home, destination) && destination != home {
		return errors.New("Harness managed path escapes user home")
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
			return errors.New("invalid Harness managed path")
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
			return errors.New("Harness managed directory is unsafe")
		}
	}
	return nil
}

func writeHarnessWrapper(payload, goos, finalRoot string) error {
	directory := filepath.Join(payload, "bin")
	if err := os.Mkdir(directory, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	path := filepath.Join(directory, "dsh")
	node := filepath.Join(finalRoot, "runtime", "bin", "node")
	script := filepath.Join(finalRoot, "app", "node_modules", "@deepseek-ai", "dsh", "lib", "bin.js")
	content := "#!/bin/sh\nset -eu\nexec " + shellQuote(node) + " " + shellQuote(script) + " \"$@\"\n"
	mode := os.FileMode(0o755)
	if goos == "windows" {
		path += ".cmd"
		content = "@echo off\r\nsetlocal DisableDelayedExpansion\r\n\"%~dp0..\\runtime\\node.exe\" \"%~dp0..\\app\\node_modules\\@deepseek-ai\\dsh\\lib\\bin.js\" %*\r\n"
		mode = 0o600
	}
	return os.WriteFile(path, []byte(content), mode)
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func verifyHarness(ctx context.Context, payload, goos string) error {
	node := filepath.Join(payload, "runtime", "bin", "node")
	if goos == "windows" {
		node = filepath.Join(payload, "runtime", "node.exe")
	}
	script := filepath.Join(payload, "app", "node_modules", "@deepseek-ai", "dsh", "lib", "bin.js")
	for _, path := range []string{node, script} {
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return errVersion
		}
	}
	verifyContext, cancel := context.WithTimeout(ctx, versionTimeout)
	defer cancel()
	command := exec.CommandContext(verifyContext, node, script, "--version")
	command.Env = append(os.Environ(), "NO_COLOR=1")
	output, err := command.CombinedOutput()
	if err != nil || len(output) > 4096 || strings.TrimSpace(string(output)) != harnessVer {
		return errVersion
	}
	return nil
}

func commitAndActivateHarnessPayload(home, payload string, paths managedPaths, goos string, activate func(string, managedPaths, string) error) error {
	marker, err := os.ReadFile(filepath.Join(payload, ".osverse-harness-runtime"))
	if err != nil {
		return err
	}
	quarantined, err := quarantineDamagedHarnessPayload(home, payload, paths, goos)
	if err != nil {
		return err
	}
	restoreQuarantined := func() error {
		if quarantined == "" {
			return nil
		}
		if _, statErr := os.Lstat(paths.finalRoot); statErr == nil {
			if removeErr := removeCommittedHarnessPayload(home, paths.finalRoot, marker); removeErr != nil {
				return removeErr
			}
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return statErr
		}
		return commitHarnessRename(quarantined, paths.finalRoot)
	}
	created, err := commitHarnessPayload(payload, paths.finalRoot, goos)
	if err != nil {
		if restoreErr := restoreQuarantined(); restoreErr != nil {
			return errors.Join(errRollback, err, restoreErr)
		}
		return err
	}
	if err := activate(home, paths, goos); err != nil {
		if created {
			if rollbackErr := removeCommittedHarnessPayload(home, paths.finalRoot, marker); rollbackErr != nil {
				return errors.Join(errRollback, err, rollbackErr)
			}
		}
		if restoreErr := restoreQuarantined(); restoreErr != nil {
			return errors.Join(errRollback, err, restoreErr)
		}
		return err
	}
	return nil
}

// quarantineDamagedHarnessPayload recognizes only a fixed-version runtime
// carrying the exact manifest of the newly verified payload. Damaged legacy
// bytes are moved into Osverse's recovery area before replacement, so repair
// is reversible and never expands into user profiles or third-party installs.
func quarantineDamagedHarnessPayload(home, payload string, paths managedPaths, goos string) (string, error) {
	info, err := os.Lstat(paths.finalRoot)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("Harness version path is unsafe")
	}
	expectedMarker, err := os.ReadFile(filepath.Join(payload, ".osverse-harness-runtime"))
	if err != nil {
		return "", err
	}
	existingMarker, err := os.ReadFile(filepath.Join(paths.finalRoot, ".osverse-harness-runtime"))
	if err != nil || !bytes.Equal(existingMarker, expectedMarker) {
		return "", errors.New("Harness version identity mismatch")
	}
	damaged := false
	for _, relative := range criticalHarnessPaths(goos) {
		equal, compareErr := sameRegularFile(
			filepath.Join(payload, filepath.FromSlash(relative)),
			filepath.Join(paths.finalRoot, filepath.FromSlash(relative)),
		)
		if compareErr != nil || !equal {
			damaged = true
			break
		}
	}
	if !damaged {
		return "", nil
	}
	recoveryRoot := filepath.Join(paths.root, "recovery")
	if err := ensureManagedDirectory(home, recoveryRoot); err != nil {
		return "", err
	}
	quarantine, err := os.MkdirTemp(recoveryRoot, "install-"+componentID+"-")
	if err != nil {
		return "", err
	}
	if err := os.Remove(quarantine); err != nil {
		return "", err
	}
	if err := commitHarnessRename(paths.finalRoot, quarantine); err != nil {
		return "", err
	}
	return quarantine, nil
}

func commitHarnessPayload(payload, destination, goos string) (bool, error) {
	marker, err := os.ReadFile(filepath.Join(payload, ".osverse-harness-runtime"))
	if err != nil {
		return false, err
	}
	info, err := os.Lstat(destination)
	if err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return false, errors.New("Harness version path is unsafe")
		}
		existing, readErr := os.ReadFile(filepath.Join(destination, ".osverse-harness-runtime"))
		if readErr != nil || !bytes.Equal(existing, marker) {
			return false, errors.New("Harness version identity mismatch")
		}
		for _, relative := range criticalHarnessPaths(goos) {
			equal, compareErr := sameRegularFile(
				filepath.Join(payload, filepath.FromSlash(relative)),
				filepath.Join(destination, filepath.FromSlash(relative)),
			)
			if compareErr != nil || !equal {
				return false, errVersion
			}
		}
		return false, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	if err := commitHarnessRename(payload, destination); err != nil {
		return false, err
	}
	return true, nil
}

func removeCommittedHarnessPayload(home, destination string, marker []byte) error {
	home, destination = filepath.Clean(home), filepath.Clean(destination)
	if !pathWithin(home, destination) || destination == home {
		return errVersion
	}
	info, err := os.Lstat(destination)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errVersion
	}
	existing, err := os.ReadFile(filepath.Join(destination, ".osverse-harness-runtime"))
	if err != nil || !bytes.Equal(existing, marker) {
		return errVersion
	}
	return os.RemoveAll(destination)
}

func criticalHarnessPaths(goos string) []string {
	if goos == "windows" {
		return []string{
			"runtime/node.exe",
			"app/node_modules/@deepseek-ai/dsh/lib/bin.js",
			"bin/dsh.cmd",
		}
	}
	return []string{
		"runtime/bin/node",
		"app/node_modules/@deepseek-ai/dsh/lib/bin.js",
		"bin/dsh",
	}
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
	expectedHash, err := fileSHA256(expected)
	if err != nil {
		return false, err
	}
	actualHash, err := fileSHA256(actual)
	if err != nil {
		return false, err
	}
	return expectedHash == actualHash, nil
}

func openVerifiedRegular(path string) (*os.File, os.FileInfo, error) {
	pathInfo, err := os.Lstat(path)
	if err != nil {
		return nil, nil, err
	}
	if !pathInfo.Mode().IsRegular() || pathInfo.Mode()&os.ModeSymlink != 0 {
		return nil, nil, errVersion
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	fileInfo, err := file.Stat()
	if err != nil || !fileInfo.Mode().IsRegular() || !os.SameFile(pathInfo, fileInfo) {
		_ = file.Close()
		if err != nil {
			return nil, nil, err
		}
		return nil, nil, errVersion
	}
	return file, fileInfo, nil
}

func fileSHA256(file *os.File) ([sha256.Size]byte, error) {
	var result [sha256.Size]byte
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return result, err
	}
	copy(result[:], hash.Sum(nil))
	return result, nil
}
