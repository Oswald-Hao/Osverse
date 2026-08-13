package detect

import (
	"context"
	"errors"
	"io/fs"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Oswald-Hao/Osverse/internal/domain"
	"github.com/Oswald-Hao/Osverse/internal/platform"
)

const (
	desktopQueryTimeout     = 3 * time.Second
	desktopQueryOutputLimit = 64 * 1024
	desktopBrokenMessage    = "Installation evidence found without an executable"
	desktopUnsupported      = "Unsupported on this system"
	desktopInstalledWarning = "Installed, but unsupported on this system"
)

var debianPackageName = regexp.MustCompile(`^[a-z0-9][a-z0-9+.-]*$`)
var debianVersion = regexp.MustCompile(`^(?:[0-9]+:)?[0-9][A-Za-z0-9.+~]*(?:-[A-Za-z0-9.+~]+)?$`)

// PackageQuery reports the installed version of one fixed package name.
type PackageQuery interface {
	InstalledVersion(context.Context, string) (string, bool, error)
}

// DesktopSpec defines the fixed evidence names and compatibility floor for a
// desktop application or management tool.
type DesktopSpec struct {
	ID              string
	Name            string
	Category        string
	PackageName     string
	ExecutableName  string
	DesktopFileName string
	MinimumUbuntu   string
}

// DesktopSpecs returns the Phase-1 desktop and management-tool catalog in
// stable dashboard order.
func DesktopSpecs() []DesktopSpec {
	return []DesktopSpec{
		{
			ID: "claude-desktop", Name: "Claude Desktop", Category: "Desktop Applications",
			PackageName: "claude-desktop", ExecutableName: "claude-desktop",
			DesktopFileName: "claude-desktop.desktop", MinimumUbuntu: "22.04",
		},
		{
			ID: "chatgpt-desktop", Name: "ChatGPT Desktop", Category: "Desktop Applications",
			PackageName: "chatgpt-desktop", ExecutableName: "chatgpt-desktop",
			DesktopFileName: "chatgpt-desktop.desktop", MinimumUbuntu: "24.04",
		},
		{
			ID: "opencode-desktop", Name: "OpenCode Desktop", Category: "Desktop Applications",
			PackageName: "opencode-desktop", ExecutableName: "opencode-desktop",
			DesktopFileName: "opencode-desktop.desktop", MinimumUbuntu: "20.04",
		},
		{
			ID: "cc-switch", Name: "CC Switch", Category: "Management Tools",
			PackageName: "cc-switch", ExecutableName: "cc-switch",
			DesktopFileName: "cc-switch.desktop", MinimumUbuntu: "20.04",
		},
		{
			ID: "cockpit-tools", Name: "Cockpit Tools", Category: "Management Tools",
			PackageName: "cockpit-tools", ExecutableName: "cockpit-tools",
			DesktopFileName: "cockpit-tools.desktop", MinimumUbuntu: "20.04",
		},
	}
}

// DetectDesktop merges read-only package, fixed desktop-file, and executable
// evidence. Desktop files are never read or parsed.
func DetectDesktop(
	ctx context.Context,
	spec DesktopSpec,
	system domain.SystemInfo,
	paths []string,
	packages PackageQuery,
	fsys fs.FS,
	home string,
) (domain.Component, error) {
	if err := ctx.Err(); err != nil {
		return desktopComponent(spec, domain.StatusBroken, nil, ""), err
	}
	if err := validateDesktopSpec(spec); err != nil {
		return desktopComponent(spec, domain.StatusBroken, nil, ""), invalidDesktopResult(err)
	}
	if packages == nil || fsys == nil {
		return desktopComponent(spec, domain.StatusBroken, nil, ""),
			invalidDesktopResult(errors.New("desktop detector dependency is nil"))
	}

	version, packageInstalled, err := packages.InstalledVersion(ctx, spec.PackageName)
	if err != nil {
		return desktopComponent(spec, domain.StatusBroken, nil, ""), err
	}
	if err := ctx.Err(); err != nil {
		return desktopComponent(spec, domain.StatusBroken, nil, ""), err
	}

	desktopPaths, err := fixedDesktopPaths(ctx, fsys, home, spec.DesktopFileName)
	if err != nil {
		return desktopComponent(spec, domain.StatusBroken, nil, ""), err
	}
	candidates, canceled := commandCandidates(ctx, []string{spec.ExecutableName}, paths)
	defer closeCommandCandidates(candidates)
	if canceled || ctx.Err() != nil {
		return desktopComponent(spec, domain.StatusBroken, nil, ""), ctx.Err()
	}

	allowed := desktopAllowed(system, spec.MinimumUbuntu)
	hasInstallEvidence := packageInstalled || len(desktopPaths) > 0
	if !hasInstallEvidence {
		if !allowed {
			return desktopComponent(spec, domain.StatusUnsupported, nil, desktopUnsupported), nil
		}
		return desktopComponent(spec, domain.StatusMissing, nil, missingMessage), nil
	}
	if len(candidates) == 0 {
		return desktopComponent(spec, domain.StatusBroken, nil, desktopBrokenMessage), nil
	}

	source := "desktop"
	if packageInstalled {
		source = "dpkg"
	}
	installations := make([]domain.Installation, 0, len(candidates))
	for _, candidate := range candidates {
		installation := domain.Installation{
			Path: candidate.path, ResolvedPath: candidate.resolvedPath, Source: source,
		}
		if packageInstalled {
			installation.Version = version
		}
		installations = append(installations, installation)
	}
	sort.Slice(installations, func(i, j int) bool {
		return installations[i].Path < installations[j].Path
	})
	if len(installations) > 1 {
		return desktopComponent(spec, domain.StatusConflict, installations, conflictMessage), nil
	}
	message := installedMessage
	if !allowed {
		message = desktopInstalledWarning
	}
	return desktopComponent(spec, domain.StatusInstalled, installations, message), nil
}

// DpkgQuery is the production package-query adapter.
type DpkgQuery struct {
	Runner platform.CommandRunner
}

// InstalledVersion directly queries dpkg's exact status and version fields.
func (query DpkgQuery) InstalledVersion(ctx context.Context, packageName string) (string, bool, error) {
	if err := ctx.Err(); err != nil {
		return "", false, err
	}
	if query.Runner == nil || !debianPackageName.MatchString(packageName) {
		return "", false, invalidDesktopResult(errors.New("invalid package query"))
	}
	result, err := query.Runner.Run(ctx, platform.CommandRequest{
		Path:        "/usr/bin/dpkg-query",
		Args:        []string{"-W", "-f=${Status}\t${Version}", packageName},
		Timeout:     desktopQueryTimeout,
		OutputLimit: desktopQueryOutputLimit,
	})
	if ctxErr := ctx.Err(); ctxErr != nil {
		return "", false, ctxErr
	}
	if result.Truncated {
		return "", false, invalidDesktopResult(errors.New("truncated package query output"))
	}
	if result.TimedOut {
		if err != nil {
			return "", false, err
		}
		return "", false, domain.NewPublicError(domain.ErrCommandTimeout, "package query timed out", nil)
	}
	if result.ExitCode == 1 && result.Stdout == "" {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if result.TimedOut || result.ExitCode != 0 {
		return "", false, domain.NewPublicError(domain.ErrCommandFailed, "package query failed", nil)
	}

	line, ok := singleDpkgLine(result.Stdout)
	if !ok {
		return "", false, invalidDesktopResult(errors.New("malformed package query output"))
	}
	fields := strings.Split(line, "\t")
	if len(fields) != 2 || fields[0] != "install ok installed" || !validDpkgVersion(fields[1]) {
		return "", false, invalidDesktopResult(errors.New("malformed package query result"))
	}
	return fields[1], true, nil
}

// DesktopComponentProbe adapts desktop detection to the scan component API.
type DesktopComponentProbe struct {
	Spec     DesktopSpec
	Packages PackageQuery
	FS       fs.FS
	Home     string
}

// Descriptor returns the stable identity exposed while this probe is running.
func (probe DesktopComponentProbe) Descriptor() domain.Component {
	return desktopComponent(probe.Spec, domain.StatusDetecting, nil, "")
}

// Detect delegates read-only desktop probing and propagates adapter failures.
func (probe DesktopComponentProbe) Detect(
	ctx context.Context,
	system domain.SystemInfo,
	paths []string,
) (domain.Component, error) {
	return DetectDesktop(ctx, probe.Spec, system, paths, probe.Packages, probe.FS, probe.Home)
}

func validateDesktopSpec(spec DesktopSpec) error {
	if spec.ID == "" || spec.Name == "" || spec.Category == "" ||
		!debianPackageName.MatchString(spec.PackageName) ||
		!validExecutableName(spec.ExecutableName) ||
		!validDesktopFileName(spec.DesktopFileName) {
		return errors.New("invalid desktop specification")
	}
	if _, _, ok := parseUbuntuVersion(spec.MinimumUbuntu); !ok {
		return errors.New("invalid Ubuntu minimum")
	}
	return nil
}

func validDesktopFileName(name string) bool {
	return name != "" && name != "." && filepath.Base(name) == name &&
		!filepath.IsAbs(name) && strings.HasSuffix(name, ".desktop")
}

func fixedDesktopPaths(ctx context.Context, fsys fs.FS, home, name string) ([]string, error) {
	directories := []string{"/usr/share/applications", "/usr/local/share/applications"}
	if cleanHome, ok := safeDesktopHome(home); ok {
		directories = append(directories, filepath.Join(cleanHome, ".local", "share", "applications"))
	}
	found := make([]string, 0, len(directories))
	for _, directory := range directories {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		path, ok := containedDesktopPath(directory, name)
		if !ok {
			return nil, invalidDesktopResult(errors.New("desktop path escaped application directory"))
		}
		present, err := regularDirectoryEntry(fsys, filepath.ToSlash(strings.TrimPrefix(directory, "/")), name)
		if err != nil {
			return nil, err
		}
		if present {
			found = append(found, path)
		}
	}
	sort.Strings(found)
	return found, nil
}

func safeDesktopHome(home string) (string, bool) {
	if home == "" || !filepath.IsAbs(home) || filepath.Clean(home) != home || home == string(filepath.Separator) {
		return "", false
	}
	return home, true
}

func containedDesktopPath(directory, name string) (string, bool) {
	if !validDesktopFileName(name) || !filepath.IsAbs(directory) {
		return "", false
	}
	directory = filepath.Clean(directory)
	path := filepath.Join(directory, name)
	return path, filepath.Dir(path) == directory
}

func regularDirectoryEntry(fsys fs.FS, directory, name string) (bool, error) {
	directoryOK, err := containedFSDirectory(fsys, directory)
	if err != nil || !directoryOK {
		return false, err
	}
	entries, err := fs.ReadDir(fsys, directory)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	for _, entry := range entries {
		if entry.Name() != name {
			continue
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			return false, nil
		}
		info, err := entry.Info()
		if err != nil {
			return false, err
		}
		return info.Mode().IsRegular(), nil
	}
	return false, nil
}

func containedFSDirectory(fsys fs.FS, directory string) (bool, error) {
	if !fs.ValidPath(directory) || directory == "." {
		return false, invalidDesktopResult(errors.New("invalid application directory"))
	}
	parent := "."
	for _, name := range strings.Split(directory, "/") {
		entries, err := fs.ReadDir(fsys, parent)
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		found := false
		for _, entry := range entries {
			if entry.Name() != name {
				continue
			}
			found = true
			if entry.Type()&fs.ModeSymlink != 0 {
				return false, nil
			}
			info, err := entry.Info()
			if err != nil {
				return false, err
			}
			if !info.IsDir() {
				return false, nil
			}
			break
		}
		if !found {
			return false, nil
		}
		parent = filepath.ToSlash(filepath.Join(parent, name))
	}
	return true, nil
}

func desktopAllowed(system domain.SystemInfo, minimum string) bool {
	if !system.Supported {
		return false
	}
	major, minor, ok := parseUbuntuVersion(system.Version)
	minimumMajor, minimumMinor, minimumOK := parseUbuntuVersion(minimum)
	if !ok || !minimumOK {
		return false
	}
	return major > minimumMajor || major == minimumMajor && minor >= minimumMinor
}

func parseUbuntuVersion(version string) (int, int, bool) {
	parts := strings.Split(version, ".")
	if len(parts) != 2 || parts[0] == "" || len(parts[1]) != 2 {
		return 0, 0, false
	}
	major, majorErr := strconv.Atoi(parts[0])
	minor, minorErr := strconv.Atoi(parts[1])
	return major, minor, majorErr == nil && minorErr == nil && major >= 0 && minor >= 0
}

func singleDpkgLine(output string) (string, bool) {
	if strings.HasSuffix(output, "\n") {
		output = strings.TrimSuffix(output, "\n")
		output = strings.TrimSuffix(output, "\r")
	}
	return output, output != "" && !strings.ContainsAny(output, "\r\n")
}

func validDpkgVersion(version string) bool {
	return debianVersion.MatchString(version)
}

func desktopComponent(
	spec DesktopSpec,
	status domain.ComponentStatus,
	installations []domain.Installation,
	message string,
) domain.Component {
	minimum := ""
	if spec.MinimumUbuntu != "" {
		minimum = "Ubuntu " + spec.MinimumUbuntu
	}
	return domain.Component{
		ID: spec.ID, Name: spec.Name, Category: spec.Category, Status: status,
		Installations: installations, Message: message, MinimumOS: minimum,
	}
}

func invalidDesktopResult(cause error) error {
	return domain.NewPublicError(domain.ErrInvalidResult, "invalid desktop detection result", cause)
}
