package detect

import (
	"context"
	"errors"
	"io/fs"
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
	desktopQueryTimeout     = 3 * time.Second
	desktopQueryOutputLimit = 64 * 1024
	desktopBrokenMessage    = "Installation evidence found without an executable"
	desktopUnsupported      = "Unsupported on this system"
	desktopInstalledWarning = "Installed, but unsupported on this system"
)

var debianPackageName = regexp.MustCompile(`^[a-z0-9][a-z0-9+.-]*(?::[a-z0-9]+(?:-[a-z0-9]+)*)?$`)

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

	desktopEvidence, err := fixedDesktopEvidence(ctx, fsys, home, spec.DesktopFileName)
	if err != nil {
		return desktopComponent(spec, domain.StatusBroken, nil, ""), err
	}
	defer closeDesktopEvidence(desktopEvidence)
	candidates, canceled := commandCandidates(ctx, []string{spec.ExecutableName}, paths)
	defer closeCommandCandidates(candidates)
	if canceled || ctx.Err() != nil {
		return desktopComponent(spec, domain.StatusBroken, nil, ""), ctx.Err()
	}

	allowed := desktopAllowed(system, spec.MinimumUbuntu)
	hasInstallEvidence := packageInstalled || len(desktopEvidence) > 0
	if !hasInstallEvidence {
		if !allowed {
			return desktopComponent(spec, domain.StatusUnsupported, nil, desktopUnsupported), nil
		}
		return desktopComponent(spec, domain.StatusMissing, nil, missingMessage), nil
	}
	if len(candidates) == 0 {
		return desktopComponent(spec, domain.StatusBroken, nil, desktopBrokenMessage), nil
	}
	if !desktopEvidenceUnchanged(desktopEvidence) || !desktopCandidatesUnchanged(candidates) {
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
	if !allowed {
		return desktopComponent(spec, domain.StatusInstalled, installations, desktopInstalledWarning), nil
	}
	if len(installations) > 1 {
		return desktopComponent(spec, domain.StatusConflict, installations, conflictMessage), nil
	}
	return desktopComponent(spec, domain.StatusInstalled, installations, installedMessage), nil
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
		Path: "/usr/bin/dpkg-query",
		// Package is the unqualified binary control-field name; unlike
		// binary:Package, it cannot acquire an architecture suffix.
		Args:        []string{"-W", "-f=${Package}\t${db:Status-Abbrev}\t${Version}", packageName},
		Env:         []string{"LC_ALL=C"},
		Timeout:     desktopQueryTimeout,
		OutputLimit: desktopQueryOutputLimit,
	})
	if ctxErr := ctx.Err(); ctxErr != nil {
		return "", false, ctxErr
	}
	if result.TimedOut {
		if err != nil {
			return "", false, err
		}
		return "", false, domain.NewPublicError(domain.ErrCommandTimeout, "package query timed out", nil)
	}
	if err != nil && exactDpkgNotFound(result, packageName) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if result.Truncated {
		return "", false, invalidDesktopResult(errors.New("truncated package query output"))
	}
	if result.TimedOut || result.ExitCode != 0 {
		return "", false, domain.NewPublicError(domain.ErrCommandFailed, "package query failed", nil)
	}

	line, ok := singleDpkgLine(result.Stdout)
	if !ok {
		return "", false, invalidDesktopResult(errors.New("malformed package query output"))
	}
	fields := strings.Split(line, "\t")
	installed := len(fields) == 3 && len(fields[1]) == 3 && fields[1][1] == 'i'
	if len(fields) != 3 || fields[0] != dpkgPackageBase(packageName) || !validDpkgStatus(fields[1]) ||
		(fields[2] != "" && !validDpkgVersion(fields[2])) || installed && fields[2] == "" {
		return "", false, invalidDesktopResult(errors.New("malformed package query result"))
	}
	return fields[2], installed, nil
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

type desktopEvidence struct {
	path     string
	file     fs.File
	info     fs.FileInfo
	sameFile func(fs.FileInfo, fs.FileInfo) bool
}

func fixedDesktopEvidence(ctx context.Context, fsys fs.FS, home, name string) ([]desktopEvidence, error) {
	directories := []string{"/usr/share/applications", "/usr/local/share/applications"}
	if cleanHome, ok := safeDesktopHome(home); ok {
		directories = append(directories, filepath.Join(cleanHome, ".local", "share", "applications"))
	}
	found := make([]desktopEvidence, 0, len(directories))
	for _, directory := range directories {
		if err := ctx.Err(); err != nil {
			closeDesktopEvidence(found)
			return nil, err
		}
		path, ok := containedDesktopPath(directory, name)
		if !ok {
			closeDesktopEvidence(found)
			return nil, invalidDesktopResult(errors.New("desktop path escaped application directory"))
		}
		evidence, present, err := openDesktopEvidence(fsys, filepath.ToSlash(strings.TrimPrefix(directory, "/")), name, path)
		if err != nil {
			closeDesktopEvidence(found)
			return nil, err
		}
		if present {
			found = append(found, evidence)
		}
	}
	sort.Slice(found, func(i, j int) bool { return found[i].path < found[j].path })
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

func openDesktopEvidence(fsys fs.FS, directory, name, displayPath string) (desktopEvidence, bool, error) {
	if !fs.ValidPath(directory) || directory == "." || !validDesktopFileName(name) {
		return desktopEvidence{}, false, invalidDesktopResult(errors.New("invalid desktop evidence path"))
	}
	root, rootErr := fsys.Open(".")
	if rootErr == nil {
		if rootFile, ok := root.(*os.File); ok {
			evidence, present, err := openOSDesktopEvidence(rootFile, directory, name, displayPath)
			_ = root.Close()
			return evidence, present, err
		}
		_ = root.Close()
	} else if !errors.Is(rootErr, fs.ErrNotExist) {
		return desktopEvidence{}, false, rootErr
	}

	fullPath := filepath.ToSlash(filepath.Join(directory, name))
	file, err := fsys.Open(fullPath)
	if errors.Is(err, fs.ErrNotExist) {
		return desktopEvidence{}, false, nil
	}
	if err != nil {
		return desktopEvidence{}, false, err
	}
	openedInfo, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return desktopEvidence{}, false, err
	}
	if !openedInfo.Mode().IsRegular() {
		_ = file.Close()
		return desktopEvidence{}, false, nil
	}
	// A generic fs.FS has no portable no-follow operation. Treat the single
	// opened handle as the evidence snapshot only when its FileInfo exposes an
	// identity that os.SameFile can revalidate without reopening the pathname.
	if openedInfo.Sys() == nil || !os.SameFile(openedInfo, openedInfo) {
		_ = file.Close()
		return desktopEvidence{}, false, invalidDesktopResult(errors.New("filesystem cannot prove desktop identity"))
	}
	return desktopEvidence{
		path: displayPath, file: file, info: openedInfo, sameFile: os.SameFile,
	}, true, nil
}

func openOSDesktopEvidence(root *os.File, directory, name, displayPath string) (desktopEvidence, bool, error) {
	current, err := unix.Dup(int(root.Fd()))
	if err != nil {
		return desktopEvidence{}, false, err
	}
	defer func() {
		if current >= 0 {
			_ = unix.Close(current)
		}
	}()
	for _, component := range strings.Split(directory, "/") {
		next, openErr := unix.Openat(current, component,
			unix.O_PATH|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if errors.Is(openErr, unix.ENOENT) || errors.Is(openErr, unix.ENOTDIR) || errors.Is(openErr, unix.ELOOP) {
			return desktopEvidence{}, false, nil
		}
		if openErr != nil {
			return desktopEvidence{}, false, openErr
		}
		_ = unix.Close(current)
		current = next
	}
	fd, err := unix.Openat(current, name, unix.O_PATH|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if errors.Is(err, unix.ENOENT) || errors.Is(err, unix.ELOOP) {
		return desktopEvidence{}, false, nil
	}
	if err != nil {
		return desktopEvidence{}, false, err
	}
	file := os.NewFile(uintptr(fd), displayPath)
	if file == nil {
		_ = unix.Close(fd)
		return desktopEvidence{}, false, errors.New("open desktop evidence")
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		_ = file.Close()
		if err != nil {
			return desktopEvidence{}, false, err
		}
		return desktopEvidence{}, false, nil
	}
	return desktopEvidence{path: displayPath, file: file, info: info, sameFile: os.SameFile}, true, nil
}

func desktopEvidenceUnchanged(evidence []desktopEvidence) bool {
	for _, item := range evidence {
		info, err := item.file.Stat()
		if err != nil || !info.Mode().IsRegular() {
			return false
		}
		if item.sameFile == nil || !item.sameFile(item.info, info) {
			return false
		}
	}
	return true
}

func closeDesktopEvidence(evidence []desktopEvidence) {
	for _, item := range evidence {
		_ = item.file.Close()
	}
}

func desktopCandidatesUnchanged(candidates []commandCandidate) bool {
	for _, candidate := range candidates {
		if !directCandidateUnchanged(candidate) {
			return false
		}
	}
	return true
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
	if version == "" || strings.IndexFunc(version, func(r rune) bool {
		return r > 127 || r <= ' '
	}) >= 0 {
		return false
	}
	remainder := version
	if colon := strings.IndexByte(version, ':'); colon >= 0 {
		epoch := version[:colon]
		if epoch == "" || !allASCII(epoch, "0123456789") {
			return false
		}
		remainder = version[colon+1:]
	}
	upstream := remainder
	revision := ""
	if hyphen := strings.LastIndexByte(remainder, '-'); hyphen >= 0 {
		upstream = remainder[:hyphen]
		revision = remainder[hyphen+1:]
		if revision == "" || !allASCII(revision, "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789.+~") {
			return false
		}
	}
	return upstream != "" && upstream[0] >= '0' && upstream[0] <= '9' &&
		allASCII(upstream, "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789.+:~-")
}

func validDpkgStatus(status string) bool {
	if len(status) != 3 || !strings.ContainsRune("uihrp", rune(status[0])) ||
		!strings.ContainsRune("ncHUFWti", rune(status[1])) {
		return false
	}
	return status[2] == ' ' || status[2] == 'R'
}

func allASCII(value, allowed string) bool {
	for _, char := range value {
		if !strings.ContainsRune(allowed, char) {
			return false
		}
	}
	return true
}

func dpkgPackageBase(packageName string) string {
	base, _, _ := strings.Cut(packageName, ":")
	return base
}

func exactDpkgNotFound(result platform.CommandResult, packageName string) bool {
	return result.ExitCode == 1 && result.Stdout == "" && !result.TimedOut && !result.Truncated &&
		result.Stderr == "dpkg-query: no packages found matching "+packageName+"\n"
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
