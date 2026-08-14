//go:build linux

package linux

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/Oswald-Hao/Osverse/internal/platform"
	"golang.org/x/sys/unix"
)

var errDetachedLaunch = errors.New("detached launch failed")

type terminalSpec struct {
	path      string
	arguments func(string) []string
}

var linuxTerminals = []terminalSpec{
	{path: "/usr/bin/x-terminal-emulator", arguments: terminalDashE},
	{path: "/usr/bin/gnome-terminal", arguments: terminalDoubleDash},
	{path: "/usr/bin/konsole", arguments: terminalDashE},
	{path: "/usr/bin/xfce4-terminal", arguments: terminalDashX},
	{path: "/usr/bin/kitty", arguments: terminalDoubleDash},
	{path: "/usr/bin/xterm", arguments: terminalDashE},
}

func terminalDashE(path string) []string      { return []string{"-e", path} }
func terminalDashX(path string) []string      { return []string{"-x", path} }
func terminalDoubleDash(path string) []string { return []string{"--", path} }

type detachedStarter struct {
	terminals []terminalSpec
	start     func(*exec.Cmd) error
}

// NewDetachedStarter starts GUI applications directly and CLIs in a known
// terminal emulator. It never invokes a command shell.
func NewDetachedStarter() platform.ProcessStarter {
	return &detachedStarter{terminals: append([]terminalSpec(nil), linuxTerminals...), start: startAndRelease}
}

func (starter *detachedStarter) Start(request platform.LaunchRequest) error {
	before, ok := inspectLaunchIdentity(request.Path, request.ExpectedResolvedPath)
	if starter == nil || starter.start == nil || !ok {
		return errDetachedLaunch
	}
	command := exec.Command(request.Path)
	if request.Terminal {
		terminal, found := firstTerminal(starter.terminals)
		if !found {
			return errDetachedLaunch
		}
		command = exec.Command(terminal.path, terminal.arguments(request.Path)...)
	}
	command.Env = launchEnvironment()
	if home, err := os.UserHomeDir(); err == nil && filepath.IsAbs(home) {
		command.Dir = filepath.Clean(home)
	}
	command.Stdin, command.Stdout, command.Stderr = nil, nil, nil
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := starter.start(command); err != nil {
		return errDetachedLaunch
	}
	after, ok := inspectLaunchIdentity(request.Path, request.ExpectedResolvedPath)
	if !ok || !sameLaunchIdentity(before, after) {
		return errDetachedLaunch
	}
	return nil
}

func startAndRelease(command *exec.Cmd) error {
	if err := command.Start(); err != nil {
		return err
	}
	return command.Process.Release()
}

func firstTerminal(candidates []terminalSpec) (terminalSpec, bool) {
	for _, candidate := range candidates {
		if candidate.arguments == nil || !filepath.IsAbs(candidate.path) || filepath.Clean(candidate.path) != candidate.path {
			continue
		}
		info, err := os.Stat(candidate.path)
		if err == nil && info.Mode().IsRegular() && info.Mode()&0o111 != 0 {
			return candidate, true
		}
	}
	return terminalSpec{}, false
}

type launchIdentity struct {
	alias  unix.Stat_t
	target unix.Stat_t
}

func inspectLaunchIdentity(path, expectedResolved string) (launchIdentity, bool) {
	if !safeLaunchPath(path) || !safeLaunchPath(expectedResolved) {
		return launchIdentity{}, false
	}
	var identity launchIdentity
	if err := unix.Lstat(path, &identity.alias); err != nil {
		return launchIdentity{}, false
	}
	mode := identity.alias.Mode & unix.S_IFMT
	if mode != unix.S_IFREG && mode != unix.S_IFLNK {
		return launchIdentity{}, false
	}
	fd, err := unix.Open(path, unix.O_PATH|unix.O_CLOEXEC, 0)
	if err != nil {
		return launchIdentity{}, false
	}
	defer unix.Close(fd)
	if err := unix.Fstat(fd, &identity.target); err != nil || identity.target.Mode&unix.S_IFMT != unix.S_IFREG {
		return launchIdentity{}, false
	}
	fdPath := filepath.Join("/proc/self/fd", strconv.Itoa(fd))
	if err := unix.Faccessat(unix.AT_FDCWD, fdPath, unix.X_OK, unix.AT_EACCESS); err != nil {
		return launchIdentity{}, false
	}
	resolved, err := os.Readlink(fdPath)
	if err != nil || filepath.Clean(resolved) != expectedResolved {
		return launchIdentity{}, false
	}
	return identity, true
}

func safeLaunchPath(path string) bool {
	return path != "" && filepath.IsAbs(path) && filepath.Clean(path) == path && !strings.ContainsAny(path, "\x00\r\n")
}

func sameLaunchIdentity(left, right launchIdentity) bool {
	return sameLaunchStat(left.alias, right.alias) && sameLaunchStat(left.target, right.target)
}

func sameLaunchStat(left, right unix.Stat_t) bool {
	return left.Dev == right.Dev && left.Ino == right.Ino && left.Mode == right.Mode &&
		left.Size == right.Size && left.Mtim == right.Mtim && left.Ctim == right.Ctim
}

func launchEnvironment() []string {
	names := []string{
		"HOME", "PATH", "LANG", "LC_ALL", "LC_CTYPE", "TERM", "COLORTERM",
		"DISPLAY", "WAYLAND_DISPLAY", "XDG_RUNTIME_DIR", "DBUS_SESSION_BUS_ADDRESS",
		"XAUTHORITY", "DESKTOP_SESSION", "XDG_CURRENT_DESKTOP", "SSH_AUTH_SOCK",
	}
	result := make([]string, 0, len(names))
	for _, name := range names {
		if value, ok := os.LookupEnv(name); ok && !strings.ContainsAny(value, "\x00\r\n") {
			result = append(result, name+"="+value)
		}
	}
	return result
}
