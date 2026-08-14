//go:build windows

package detect

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/Oswald-Hao/Osverse/internal/domain"
	"github.com/Oswald-Hao/Osverse/internal/platform"
)

type failingWindowsVersionRunner struct{}

func (failingWindowsVersionRunner) Run(context.Context, platform.CommandRequest) (platform.CommandResult, error) {
	return platform.CommandResult{ExitCode: 1, Stderr: "version unavailable"}, errors.New("version unavailable")
}

func TestWindowsCommandDetectorTreatsExistingClaudeAsInstalledWhenVersionIsUnavailable(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "claude.exe")
	if err := os.WriteFile(path, []byte("MZ fixture"), 0o700); err != nil {
		t.Fatal(err)
	}
	spec := CommandSpec{ID: "claude-code", Name: "Claude Code", ExecutableNames: []string{"claude.exe"},
		VersionArgs: []string{"--version"}, VersionPattern: regexp.MustCompile(`^([0-9]+\.[0-9]+\.[0-9]+)$`), MinimumOS: "Windows 10 1809"}
	component := (CommandDetector{Runner: failingWindowsVersionRunner{}}).Detect(context.Background(), spec, []string{directory})
	if component.Status != domain.StatusInstalled || component.Message != installedUnverifiedMessage || len(component.Installations) != 1 || component.Installations[0].Version != "unknown" {
		t.Fatalf("component = %#v", component)
	}
}

func TestManagedWindowsShimRequiresExactMarkerAndContainedTarget(t *testing.T) {
	home := t.TempDir()
	managedRoot := filepath.Join(home, "AppData", "Local", "Osverse", "tools")
	target := filepath.Join(managedRoot, "claude-code", "2.1.232", "package", "claude.exe")
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		t.Fatal(err)
	}
	shim := filepath.Join(home, "claude.cmd")
	content := "@rem Osverse managed shim v1: claude-code\r\n@echo off\r\nsetlocal DisableDelayedExpansion\r\n\"" + target + "\" %*\r\n"
	if err := os.WriteFile(shim, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if !managedWindowsShim(shim, managedRoot, "claude-code") {
		t.Fatal("exact Osverse shim was not recognized")
	}
	outside := filepath.Join(home, "outside.exe")
	hostile := "@rem Osverse managed shim v1: claude-code\r\n@echo off\r\nsetlocal DisableDelayedExpansion\r\n\"" + outside + "\" %*\r\n"
	if err := os.WriteFile(shim, []byte(hostile), 0o600); err != nil {
		t.Fatal(err)
	}
	if managedWindowsShim(shim, managedRoot, "claude-code") {
		t.Fatal("escaping Osverse shim was accepted")
	}
}

func TestDecodeWindowsShimPathAcceptsOnlyEscapedPercent(t *testing.T) {
	decoded, ok := decodeWindowsShimPath(`C:\Users\100%%\tool.exe`)
	if !ok || decoded != `C:\Users\100%\tool.exe` {
		t.Fatalf("decoded path = (%q, %v)", decoded, ok)
	}
	if _, ok := decodeWindowsShimPath(`C:\Users\%USERPROFILE%\tool.exe`); ok {
		t.Fatal("unescaped environment expansion was accepted")
	}
}
