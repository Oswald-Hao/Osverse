package harnessinstall

import (
	"bytes"
	"context"
	"errors"
	"fmt"
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
	if err := commitHarnessPayload(payload, paths.finalRoot); err != nil {
		return err
	}
	if err := activateHarnessCommand(manager.home, paths, manager.goos); err != nil {
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

func commitHarnessPayload(payload, destination string) error {
	marker, err := os.ReadFile(filepath.Join(payload, ".osverse-harness-runtime"))
	if err != nil {
		return err
	}
	info, err := os.Lstat(destination)
	if err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("Harness version path is unsafe")
		}
		existing, readErr := os.ReadFile(filepath.Join(destination, ".osverse-harness-runtime"))
		if readErr != nil || !bytes.Equal(existing, marker) {
			return errors.New("Harness version identity mismatch")
		}
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.Rename(payload, destination)
}
