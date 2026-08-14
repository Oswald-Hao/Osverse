package linux

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/Oswald-Hao/Osverse/internal/platform"
)

func TestDetachedStarterBuildsDirectAndTerminalCommandsWithoutShell(t *testing.T) {
	root := t.TempDir()
	target := writeLaunchExecutable(t, root, "target")
	alias := filepath.Join(root, "tool")
	if err := os.Symlink(target, alias); err != nil {
		t.Fatal(err)
	}
	terminal := writeLaunchExecutable(t, root, "terminal")
	var commands []*exec.Cmd
	starter := &detachedStarter{
		terminals: []terminalSpec{{path: terminal, arguments: func(path string) []string { return []string{"-e", path} }}},
		start: func(command *exec.Cmd) error {
			commands = append(commands, command)
			return nil
		},
	}

	if err := starter.Start(platform.LaunchRequest{Path: alias, ExpectedResolvedPath: target, Terminal: true}); err != nil {
		t.Fatalf("terminal Start() error = %v", err)
	}
	if err := starter.Start(platform.LaunchRequest{Path: alias, ExpectedResolvedPath: target}); err != nil {
		t.Fatalf("direct Start() error = %v", err)
	}
	if len(commands) != 2 {
		t.Fatalf("commands = %d", len(commands))
	}
	if commands[0].Path != terminal || len(commands[0].Args) != 3 || commands[0].Args[1] != "-e" || commands[0].Args[2] != alias {
		t.Fatalf("terminal command = %#v", commands[0].Args)
	}
	if commands[1].Path != alias || len(commands[1].Args) != 1 {
		t.Fatalf("direct command = %q %#v", commands[1].Path, commands[1].Args)
	}
	for _, command := range commands {
		if command.Path == "/bin/sh" || command.Path == "/bin/bash" {
			t.Fatal("launch involved a command shell")
		}
	}
}

func TestDetachedStarterRequiresExactFreshExecutableIdentity(t *testing.T) {
	root := t.TempDir()
	target := writeLaunchExecutable(t, root, "target")
	alias := filepath.Join(root, "tool")
	if err := os.Symlink(target, alias); err != nil {
		t.Fatal(err)
	}
	starts := 0
	starter := &detachedStarter{start: func(*exec.Cmd) error { starts++; return nil }}

	for _, request := range []platform.LaunchRequest{
		{Path: "relative", ExpectedResolvedPath: target},
		{Path: alias, ExpectedResolvedPath: filepath.Join(root, "other")},
		{Path: alias, ExpectedResolvedPath: target, Terminal: true},
	} {
		if err := starter.Start(request); !errors.Is(err, errDetachedLaunch) {
			t.Fatalf("Start(%#v) error = %v", request, err)
		}
	}
	if starts != 0 {
		t.Fatalf("invalid requests started %d processes", starts)
	}
}

func TestDetachedStarterDetectsReplacementDuringStart(t *testing.T) {
	root := t.TempDir()
	target := writeLaunchExecutable(t, root, "target")
	alias := filepath.Join(root, "tool")
	if err := os.Symlink(target, alias); err != nil {
		t.Fatal(err)
	}
	replacement := writeLaunchExecutable(t, root, "replacement")
	starter := &detachedStarter{start: func(*exec.Cmd) error {
		if err := os.Remove(alias); err != nil {
			return err
		}
		return os.Symlink(replacement, alias)
	}}

	if err := starter.Start(platform.LaunchRequest{Path: alias, ExpectedResolvedPath: target}); !errors.Is(err, errDetachedLaunch) {
		t.Fatalf("Start() error = %v, want replacement rejection", err)
	}
}

func writeLaunchExecutable(t *testing.T, root, name string) string {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}
