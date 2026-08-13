// Package detect contains read-only component detectors.
package detect

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/Oswald-Hao/Osverse/internal/domain"
	"github.com/Oswald-Hao/Osverse/internal/platform"
	"golang.org/x/sys/unix"
)

const (
	commandCategory      = "Core CLI"
	versionTimeout       = 3 * time.Second
	versionOutputLimit   = 64 * 1024
	missingMessage       = "Not detected"
	installedMessage     = "Installed"
	conflictMessage      = "Multiple installations detected"
	brokenVersionMessage = "Version detection failed"
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

	candidates := commandCandidates(spec.ExecutableNames, paths)
	if len(candidates) == 0 {
		return commandComponent(spec, domain.StatusMissing, nil, missingMessage)
	}

	managedRoot, managedRootOK := osverseToolsRoot()
	installations := make([]domain.Installation, 0, len(candidates))
	valid := 0
	broken := false
	for _, candidate := range candidates {
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
		result, err := d.Runner.Run(ctx, platform.CommandRequest{
			Path:        candidate.path,
			Args:        append([]string(nil), spec.VersionArgs...),
			Timeout:     versionTimeout,
			OutputLimit: versionOutputLimit,
		})
		version, parsed := parseCommandVersion(spec.VersionPattern, result)
		if err != nil || result.TimedOut || result.ExitCode != 0 || !parsed || ctx.Err() != nil {
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
	case valid >= 2:
		return commandComponent(spec, domain.StatusConflict, installations, conflictMessage)
	case broken:
		return commandComponent(spec, domain.StatusBroken, installations, brokenVersionMessage)
	default:
		return commandComponent(spec, domain.StatusInstalled, installations, installedMessage)
	}
}

type commandCandidate struct {
	path         string
	resolvedPath string
	info         os.FileInfo
}

func commandCandidates(executableNames, paths []string) []commandCandidate {
	seenPaths := make(map[string]struct{})
	var candidates []commandCandidate
	for _, directory := range paths {
		if directory == "" || !filepath.IsAbs(directory) {
			continue
		}
		directory = filepath.Clean(directory)
		for _, name := range executableNames {
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

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].path < candidates[j].path
	})
	unique := candidates[:0]
	for _, candidate := range candidates {
		if samePhysicalCommand(unique, candidate) {
			continue
		}
		unique = append(unique, candidate)
	}
	return unique
}

func validExecutableName(name string) bool {
	return name != "" && name != "." && !filepath.IsAbs(name) && filepath.Base(name) == name
}

func inspectCommandCandidate(path string) (commandCandidate, bool) {
	resolvedPath, err := filepath.EvalSymlinks(path)
	if err != nil || !filepath.IsAbs(resolvedPath) {
		return commandCandidate{}, false
	}
	info, err := os.Stat(resolvedPath)
	if err != nil || !info.Mode().IsRegular() {
		return commandCandidate{}, false
	}
	if err := unix.Faccessat(unix.AT_FDCWD, path, unix.X_OK, unix.AT_EACCESS); err != nil {
		return commandCandidate{}, false
	}
	return commandCandidate{path: path, resolvedPath: filepath.Clean(resolvedPath), info: info}, true
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
