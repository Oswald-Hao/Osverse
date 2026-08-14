//go:build windows

// Package detect contains read-only component detectors.
package detect

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Oswald-Hao/Osverse/internal/domain"
	"github.com/Oswald-Hao/Osverse/internal/platform"
	platformwindows "github.com/Oswald-Hao/Osverse/internal/platform/windows"
)

const (
	commandCategory                   = "Core CLI"
	versionTimeout                    = 3 * time.Second
	versionOutputLimit                = 64 * 1024
	missingMessage                    = "未检测到安装"
	installedMessage                  = "已安装"
	multipleInstalledMessage          = "已安装，检测到多个安装位置"
	partiallyVerifiedInstalledMessage = "已安装，另有安装位置无法验证"
	installedUnverifiedMessage        = "已安装，版本暂时无法读取"
	brokenVersionMessage              = "版本检测失败"
)

type CommandDetector struct{ Runner platform.CommandRunner }

type CommandComponentProbe struct {
	Detector CommandDetector
	Spec     CommandSpec
}

func (probe CommandComponentProbe) Descriptor() domain.Component {
	return commandComponent(probe.Spec, domain.StatusDetecting, nil, "")
}

func (probe CommandComponentProbe) Detect(ctx context.Context, _ domain.SystemInfo, paths []string) (domain.Component, error) {
	return probe.Detector.Detect(ctx, probe.Spec, paths), nil
}

func (detector CommandDetector) Detect(ctx context.Context, spec CommandSpec, paths []string) domain.Component {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return commandComponent(spec, domain.StatusBroken, nil, brokenVersionMessage)
	}
	candidates, canceled := windowsCommandCandidates(ctx, spec, paths)
	defer closeWindowsCandidates(candidates)
	if canceled || ctx.Err() != nil {
		return commandComponent(spec, domain.StatusBroken, nil, brokenVersionMessage)
	}
	if len(candidates) == 0 {
		return commandComponent(spec, domain.StatusMissing, nil, missingMessage)
	}
	managedRoot, managedOK := windowsManagedToolsRoot()
	installations := make([]domain.Installation, 0, len(candidates))
	valid, present, broken := 0, 0, false
	for _, candidate := range candidates {
		installation := domain.Installation{Path: candidate.path, ResolvedPath: candidate.resolved, Source: "path"}
		if managedOK && (pathWithinWindows(managedRoot, candidate.resolved) || managedWindowsShim(candidate.path, managedRoot, spec.ID)) {
			installation.Managed, installation.Source = true, "osverse"
		}
		if detector.Runner == nil || ctx.Err() != nil || !candidate.evidence.Unchanged(candidate.path) {
			broken = true
			invalidateWindowsInstallation(&installation)
			installations = append(installations, installation)
			continue
		}
		result, err := detector.Runner.Run(ctx, platform.CommandRequest{
			Path: candidate.path, Args: append([]string(nil), spec.VersionArgs...),
			Timeout: versionTimeout, OutputLimit: versionOutputLimit,
		})
		if !candidate.evidence.Unchanged(candidate.path) {
			broken = true
			invalidateWindowsInstallation(&installation)
			installations = append(installations, installation)
			continue
		}
		present++
		version, parsed := parseCommandVersion(spec.VersionPattern, result)
		if err != nil || result.TimedOut || result.Truncated || result.ExitCode != 0 || !parsed || ctx.Err() != nil {
			broken = true
			installation.Version = "unknown"
		} else {
			installation.Version = version
			valid++
		}
		installations = append(installations, installation)
	}
	sort.Slice(installations, func(i, j int) bool {
		return strings.ToLower(installations[i].Path) < strings.ToLower(installations[j].Path)
	})
	switch {
	case valid > 0 && broken:
		return commandComponent(spec, domain.StatusInstalled, installations, partiallyVerifiedInstalledMessage)
	case valid >= 2:
		return commandComponent(spec, domain.StatusInstalled, installations, multipleInstalledMessage)
	case valid == 1:
		return commandComponent(spec, domain.StatusInstalled, installations, installedMessage)
	case present > 0:
		return commandComponent(spec, domain.StatusInstalled, installations, installedUnverifiedMessage)
	default:
		return commandComponent(spec, domain.StatusBroken, installations, brokenVersionMessage)
	}
}

type windowsCommandCandidate struct {
	path, resolved string
	evidence       *platformwindows.ExecutableEvidence
}

func windowsCommandCandidates(ctx context.Context, spec CommandSpec, paths []string) ([]windowsCommandCandidate, bool) {
	seenPaths, seenTargets := map[string]bool{}, map[string]bool{}
	result := make([]windowsCommandCandidate, 0)
	for _, directory := range paths {
		if ctx.Err() != nil {
			return result, true
		}
		if !filepath.IsAbs(directory) {
			continue
		}
		for _, name := range spec.ExecutableNames {
			if ctx.Err() != nil {
				return result, true
			}
			if !validWindowsExecutableName(name) {
				continue
			}
			path := filepath.Join(filepath.Clean(directory), name)
			if excludedWindowsCLICandidate(spec.ID, path) {
				continue
			}
			pathKey := strings.ToLower(path)
			if seenPaths[pathKey] {
				continue
			}
			seenPaths[pathKey] = true
			evidence, err := platformwindows.OpenExecutableEvidence(path)
			if err != nil {
				continue
			}
			resolved := filepath.Clean(evidence.ResolvedPath())
			targetKey := strings.ToLower(resolved)
			if seenTargets[targetKey] {
				_ = evidence.Close()
				continue
			}
			seenTargets[targetKey] = true
			result = append(result, windowsCommandCandidate{path: path, resolved: resolved, evidence: evidence})
		}
	}
	return result, ctx.Err() != nil
}

func excludedWindowsCLICandidate(componentID, path string) bool {
	canonical := strings.ToLower(filepath.ToSlash(filepath.Clean(path)))
	if strings.Contains(canonical, "/appdata/local/microsoft/windowsapps/") {
		return true
	}
	switch componentID {
	case "claude-code":
		return strings.Contains(canonical, "/appdata/local/programs/claude/") ||
			strings.Contains(canonical, "/appdata/local/anthropicclaude/")
	case "opencode-cli":
		return strings.Contains(canonical, "/appdata/local/programs/opencode/") ||
			strings.Contains(canonical, "/appdata/local/programs/@opencode-aidesktop/") ||
			strings.Contains(canonical, "/program files/opencode/")
	default:
		return false
	}
}

func validWindowsExecutableName(name string) bool {
	if name == "" || filepath.Base(name) != name || filepath.IsAbs(name) || strings.ContainsAny(name, "\x00\r\n") {
		return false
	}
	extension := strings.ToLower(filepath.Ext(name))
	return extension == ".exe" || extension == ".com" || extension == ".cmd" || extension == ".bat"
}

func closeWindowsCandidates(candidates []windowsCommandCandidate) {
	for _, candidate := range candidates {
		_ = candidate.evidence.Close()
	}
}

func windowsManagedToolsRoot() (string, bool) {
	home, err := os.UserHomeDir()
	if err != nil || !filepath.IsAbs(home) {
		return "", false
	}
	return filepath.Join(filepath.Clean(home), "AppData", "Local", "Osverse", "tools"), true
}

func managedWindowsShim(candidatePath, managedRoot, componentID string) bool {
	if strings.ToLower(filepath.Ext(candidatePath)) != ".cmd" || componentID == "" || len(componentID) > 64 ||
		strings.ContainsAny(componentID, "\\/:\x00\r\n") {
		return false
	}
	info, err := os.Lstat(candidatePath)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() <= 0 || info.Size() > 16*1024 {
		return false
	}
	raw, err := os.ReadFile(candidatePath)
	if err != nil {
		return false
	}
	lines := strings.Split(string(raw), "\r\n")
	if len(lines) != 5 || lines[0] != "@rem Osverse managed shim v1: "+componentID ||
		lines[1] != "@echo off" || lines[2] != "setlocal DisableDelayedExpansion" || lines[4] != "" {
		return false
	}
	commandLine := lines[3]
	if !strings.HasPrefix(commandLine, `"`) || !strings.HasSuffix(commandLine, `" %*`) {
		return false
	}
	target, ok := decodeWindowsShimPath(strings.TrimSuffix(strings.TrimPrefix(commandLine, `"`), `" %*`))
	if !ok || !filepath.IsAbs(target) {
		return false
	}
	return pathWithinWindows(managedRoot, filepath.Clean(target))
}

func decodeWindowsShimPath(value string) (string, bool) {
	var result strings.Builder
	result.Grow(len(value))
	for index := 0; index < len(value); index++ {
		if value[index] != '%' {
			result.WriteByte(value[index])
			continue
		}
		if index+1 >= len(value) || value[index+1] != '%' {
			return "", false
		}
		result.WriteByte('%')
		index++
	}
	return result.String(), true
}

func pathWithinWindows(root, target string) bool {
	relative, err := filepath.Rel(root, target)
	if err != nil || relative == "." || filepath.IsAbs(relative) {
		return false
	}
	relative = strings.ToLower(relative)
	return relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func invalidateWindowsInstallation(installation *domain.Installation) {
	installation.ResolvedPath, installation.Version, installation.Source, installation.Managed = "", "", "unknown", false
}

func commandComponent(spec CommandSpec, status domain.ComponentStatus, installations []domain.Installation, message string) domain.Component {
	return domain.Component{ID: spec.ID, Name: spec.Name, Category: commandCategory, Status: status,
		Installations: installations, Message: message, MinimumOS: spec.MinimumOS}
}
