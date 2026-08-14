//go:build linux

// Package detect contains read-only component detectors.
package detect

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Oswald-Hao/Osverse/internal/domain"
	"github.com/Oswald-Hao/Osverse/internal/platform"
	"golang.org/x/sys/unix"
)

const (
	commandCategory                   = "Core CLI"
	versionTimeout                    = 3 * time.Second
	versionOutputLimit                = 64 * 1024
	missingMessage                    = "未检测到安装"
	installedMessage                  = "已安装"
	multipleInstalledMessage          = "已安装，检测到多个安装位置"
	partiallyVerifiedInstalledMessage = "已安装，另有安装位置无法验证"
	conflictMessage                   = "检测到多个安装位置"
	brokenVersionMessage              = "版本检测失败"
)

// CommandSpec defines the fixed executable names and version parser for a CLI.
type CommandSpec struct {
	ID              string
	Name            string
	ExecutableNames []string
	VersionArgs     []string
	VersionPattern  *regexp.Regexp
	MinimumOS       string
}

// CommandDetector detects CLI candidates through explicit filesystem paths.
type CommandDetector struct {
	Runner platform.CommandRunner
}

// CommandComponentProbe adapts a command detector to the scan component API.
type CommandComponentProbe struct {
	Detector CommandDetector
	Spec     CommandSpec
}

// Descriptor returns the stable identity exposed while this probe is running.
func (p CommandComponentProbe) Descriptor() domain.Component {
	return commandComponent(p.Spec, domain.StatusDetecting, nil, "")
}

// Detect delegates command probing. Detection failures are component states,
// not orchestration errors.
func (p CommandComponentProbe) Detect(
	ctx context.Context,
	_ domain.SystemInfo,
	paths []string,
) (domain.Component, error) {
	return p.Detector.Detect(ctx, p.Spec, paths), nil
}

// Detect finds all regular, executable candidates and verifies their versions.
func (d CommandDetector) Detect(ctx context.Context, spec CommandSpec, paths []string) domain.Component {
	if ctx.Err() != nil {
		return commandComponent(spec, domain.StatusBroken, nil, brokenVersionMessage)
	}

	candidates, canceled := commandCandidates(ctx, spec.ExecutableNames, paths)
	defer closeCommandCandidates(candidates)
	if canceled || ctx.Err() != nil {
		return commandComponent(spec, domain.StatusBroken, nil, brokenVersionMessage)
	}
	if len(candidates) == 0 {
		return commandComponent(spec, domain.StatusMissing, nil, missingMessage)
	}

	managedRoot, managedRootOK := osverseToolsRoot()
	installations := make([]domain.Installation, 0, len(candidates))
	valid := 0
	broken := false
	for _, candidate := range candidates {
		pinnedExecution := candidate.format.requiresPinnedExecution()
		installation := domain.Installation{
			Path:         candidate.path,
			ResolvedPath: candidate.resolvedPath,
			Source:       "path",
		}
		if managedRootOK && pathWithin(managedRoot, candidate.resolvedPath) {
			installation.Managed = true
			installation.Source = "osverse"
		}

		if ctx.Err() != nil || d.Runner == nil {
			broken = true
			installations = append(installations, installation)
			continue
		}
		if !pinnedExecution && !directCandidateUnchanged(candidate) {
			broken = true
			invalidateInstallation(&installation)
			installations = append(installations, installation)
			continue
		}
		var pinnedExecutable *os.File
		if pinnedExecution {
			pinnedExecutable = candidate.file
		}
		result, err := d.Runner.Run(ctx, platform.CommandRequest{
			Path:             candidate.path,
			PinnedExecutable: pinnedExecutable,
			Args:             append([]string(nil), spec.VersionArgs...),
			Timeout:          versionTimeout,
			OutputLimit:      versionOutputLimit,
		})
		if !pinnedExecution && !directCandidateUnchanged(candidate) {
			broken = true
			invalidateInstallation(&installation)
			installations = append(installations, installation)
			continue
		}
		version, parsed := parseCommandVersion(spec.VersionPattern, result)
		if err != nil || result.TimedOut || result.Truncated || result.ExitCode != 0 || !parsed || ctx.Err() != nil {
			broken = true
		} else {
			installation.Version = version
			valid++
		}
		installations = append(installations, installation)
	}

	sort.Slice(installations, func(i, j int) bool {
		return installations[i].Path < installations[j].Path
	})
	switch {
	case valid > 0 && broken:
		return commandComponent(spec, domain.StatusInstalled, installations, partiallyVerifiedInstalledMessage)
	case valid >= 2:
		return commandComponent(spec, domain.StatusInstalled, installations, multipleInstalledMessage)
	case valid == 1:
		return commandComponent(spec, domain.StatusInstalled, installations, installedMessage)
	default:
		return commandComponent(spec, domain.StatusBroken, installations, brokenVersionMessage)
	}
}

type commandCandidate struct {
	path         string
	resolvedPath string
	info         os.FileInfo
	file         *os.File
	aliasStat    unix.Stat_t
	targetStat   unix.Stat_t
	format       commandExecutableFormat
}

type commandExecutableFormat uint8

const (
	commandExecutableUnknown commandExecutableFormat = iota
	commandExecutableNonELF
	commandExecutableELF
)

func (format commandExecutableFormat) requiresPinnedExecution() bool {
	return format != commandExecutableNonELF
}

func commandCandidates(ctx context.Context, executableNames, paths []string) ([]commandCandidate, bool) {
	seenPaths := make(map[string]struct{})
	var candidates []commandCandidate
	for _, directory := range paths {
		if ctx.Err() != nil {
			return candidates, true
		}
		if directory == "" || !filepath.IsAbs(directory) {
			continue
		}
		directory = filepath.Clean(directory)
		for _, name := range executableNames {
			if ctx.Err() != nil {
				return candidates, true
			}
			if !validExecutableName(name) {
				continue
			}
			candidatePath := filepath.Join(directory, name)
			if _, seen := seenPaths[candidatePath]; seen {
				continue
			}
			seenPaths[candidatePath] = struct{}{}
			candidate, ok := inspectCommandCandidate(candidatePath)
			if ok {
				candidates = append(candidates, candidate)
			}
		}
	}
	if ctx.Err() != nil {
		return candidates, true
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].path < candidates[j].path
	})
	unique := candidates[:0]
	for _, candidate := range candidates {
		if samePhysicalCommand(unique, candidate) {
			_ = candidate.file.Close()
			continue
		}
		unique = append(unique, candidate)
	}
	return unique, false
}

func validExecutableName(name string) bool {
	return name != "" && name != "." && !filepath.IsAbs(name) && filepath.Base(name) == name
}

func inspectCommandCandidate(path string) (commandCandidate, bool) {
	fd, err := unix.Open(path, unix.O_PATH|unix.O_CLOEXEC, 0)
	if err != nil {
		return commandCandidate{}, false
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = unix.Close(fd)
		return commandCandidate{}, false
	}
	closeOnFailure := true
	defer func() {
		if closeOnFailure {
			_ = file.Close()
		}
	}()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return commandCandidate{}, false
	}
	fdPath := filepath.Join("/proc/self/fd", strconv.FormatUint(uint64(file.Fd()), 10))
	// Check effective-user access through the pinned descriptor path. This
	// remains tied to the open file object and works on the Ubuntu 20.04 kernel,
	// which predates faccessat2(2).
	if err := unix.Faccessat(unix.AT_FDCWD, fdPath, unix.X_OK, unix.AT_EACCESS); err != nil {
		return commandCandidate{}, false
	}
	resolvedPath, err := os.Readlink(fdPath)
	if err != nil || !filepath.IsAbs(resolvedPath) {
		return commandCandidate{}, false
	}
	var aliasStat, targetStat unix.Stat_t
	if err := unix.Lstat(path, &aliasStat); err != nil {
		return commandCandidate{}, false
	}
	if err := unix.Fstat(fd, &targetStat); err != nil {
		return commandCandidate{}, false
	}
	format := classifyCommandExecutableHeader(func() (*os.File, error) {
		return os.Open(fdPath)
	})
	closeOnFailure = false
	return commandCandidate{
		path: path, resolvedPath: filepath.Clean(resolvedPath), info: info, file: file,
		aliasStat: aliasStat, targetStat: targetStat, format: format,
	}, true
}

func classifyCommandExecutableHeader(openHeader func() (*os.File, error)) commandExecutableFormat {
	if openHeader == nil {
		return commandExecutableUnknown
	}
	reader, err := openHeader()
	if err != nil || reader == nil {
		if reader != nil {
			_ = reader.Close()
		}
		return commandExecutableUnknown
	}
	defer reader.Close()
	magic := make([]byte, 4)
	count, err := io.ReadFull(reader, magic)
	if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
		return commandExecutableNonELF
	}
	if err != nil {
		return commandExecutableUnknown
	}
	if count == len(magic) && bytes.Equal(magic, []byte{0x7f, 'E', 'L', 'F'}) {
		return commandExecutableELF
	}
	return commandExecutableNonELF
}

func directCandidateUnchanged(candidate commandCandidate) bool {
	var aliasStat unix.Stat_t
	if err := unix.Lstat(candidate.path, &aliasStat); err != nil || !sameCommandMetadata(aliasStat, candidate.aliasStat) {
		return false
	}
	fd, err := unix.Open(candidate.path, unix.O_PATH|unix.O_CLOEXEC, 0)
	if err != nil {
		return false
	}
	defer unix.Close(fd)
	var targetStat unix.Stat_t
	if err := unix.Fstat(fd, &targetStat); err != nil ||
		targetStat.Mode&unix.S_IFMT != unix.S_IFREG ||
		!sameCommandMetadata(targetStat, candidate.targetStat) {
		return false
	}
	fdPath := filepath.Join("/proc/self/fd", strconv.Itoa(fd))
	if err := unix.Faccessat(unix.AT_FDCWD, fdPath, unix.X_OK, unix.AT_EACCESS); err != nil {
		return false
	}
	resolvedPath, err := os.Readlink(fdPath)
	return err == nil && filepath.Clean(resolvedPath) == candidate.resolvedPath
}

func sameCommandMetadata(left, right unix.Stat_t) bool {
	return left.Dev == right.Dev &&
		left.Ino == right.Ino &&
		left.Mode == right.Mode &&
		left.Size == right.Size &&
		left.Mtim == right.Mtim &&
		left.Ctim == right.Ctim
}

func invalidateInstallation(installation *domain.Installation) {
	installation.ResolvedPath = ""
	installation.Version = ""
	installation.Source = "unknown"
	installation.Managed = false
}

func closeCommandCandidates(candidates []commandCandidate) {
	for _, candidate := range candidates {
		_ = candidate.file.Close()
	}
}

func samePhysicalCommand(existing []commandCandidate, candidate commandCandidate) bool {
	for _, other := range existing {
		if other.resolvedPath == candidate.resolvedPath || os.SameFile(other.info, candidate.info) {
			return true
		}
	}
	return false
}

func parseCommandVersion(pattern *regexp.Regexp, result platform.CommandResult) (string, bool) {
	if pattern == nil {
		return "", false
	}
	for _, output := range []string{strings.TrimSpace(result.Stdout), strings.TrimSpace(result.Stderr)} {
		matches := pattern.FindStringSubmatch(output)
		if len(matches) > 1 && matches[1] != "" {
			return matches[1], true
		}
	}
	return "", false
}

func osverseToolsRoot() (string, bool) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" || !filepath.IsAbs(home) {
		return "", false
	}
	home, err = filepath.EvalSymlinks(filepath.Clean(home))
	if err != nil || !filepath.IsAbs(home) {
		return "", false
	}
	info, err := os.Stat(home)
	if err != nil || !info.IsDir() {
		return "", false
	}
	return filepath.Join(home, ".local", "share", "osverse", "tools"), true
}

func pathWithin(root, target string) bool {
	relative, err := filepath.Rel(root, target)
	if err != nil || relative == "." || filepath.IsAbs(relative) {
		return false
	}
	return relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func commandComponent(
	spec CommandSpec,
	status domain.ComponentStatus,
	installations []domain.Installation,
	message string,
) domain.Component {
	return domain.Component{
		ID:            spec.ID,
		Name:          spec.Name,
		Category:      commandCategory,
		Status:        status,
		Installations: installations,
		Message:       message,
		MinimumOS:     spec.MinimumOS,
	}
}
