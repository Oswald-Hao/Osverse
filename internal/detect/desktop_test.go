package detect

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/Oswald-Hao/Osverse/internal/domain"
	"github.com/Oswald-Hao/Osverse/internal/platform"
)

func TestDesktopSpecsCatalogAndCompatibilityFloors(t *testing.T) {
	specs := DesktopSpecs()
	wantIDs := []string{"claude-desktop", "chatgpt-desktop", "opencode-desktop", "cc-switch", "cockpit-tools"}
	gotIDs := make([]string, 0, len(specs))
	byID := make(map[string]DesktopSpec, len(specs))
	for _, spec := range specs {
		gotIDs = append(gotIDs, spec.ID)
		byID[spec.ID] = spec
		if spec.Name == "" || spec.Category == "" || spec.PackageName == "" ||
			spec.ExecutableName == "" || spec.DesktopFileName == "" || spec.MinimumUbuntu == "" {
			t.Fatalf("DesktopSpecs() contains incomplete spec: %#v", spec)
		}
	}
	if !reflect.DeepEqual(gotIDs, wantIDs) {
		t.Fatalf("DesktopSpecs() IDs = %#v, want %#v", gotIDs, wantIDs)
	}
	if byID["claude-desktop"].MinimumUbuntu != "22.04" {
		t.Fatalf("Claude minimum = %q, want 22.04", byID["claude-desktop"].MinimumUbuntu)
	}
	if byID["chatgpt-desktop"].MinimumUbuntu != "24.04" {
		t.Fatalf("ChatGPT minimum = %q, want 24.04", byID["chatgpt-desktop"].MinimumUbuntu)
	}
	for _, id := range []string{"cc-switch", "cockpit-tools"} {
		if byID[id].MinimumUbuntu != "20.04" {
			t.Fatalf("%s minimum = %q, want Phase-1 floor 20.04", id, byID[id].MinimumUbuntu)
		}
	}
}

func TestDetectDesktopAbsentCompatibility(t *testing.T) {
	byID := desktopSpecsByID(DesktopSpecs())
	tests := []struct {
		name    string
		id      string
		version string
		want    domain.ComponentStatus
	}{
		{name: "Claude Ubuntu 20.04", id: "claude-desktop", version: "20.04", want: domain.StatusUnsupported},
		{name: "Claude Ubuntu 22.04", id: "claude-desktop", version: "22.04", want: domain.StatusMissing},
		{name: "ChatGPT Ubuntu 20.04", id: "chatgpt-desktop", version: "20.04", want: domain.StatusUnsupported},
		{name: "ChatGPT Ubuntu 22.04", id: "chatgpt-desktop", version: "22.04", want: domain.StatusUnsupported},
		{name: "CC Switch Ubuntu 20.04", id: "cc-switch", version: "20.04", want: domain.StatusMissing},
		{name: "Cockpit Tools Ubuntu 20.04", id: "cockpit-tools", version: "20.04", want: domain.StatusMissing},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			component, err := DetectDesktop(context.Background(), byID[tt.id], supportedUbuntu(tt.version), nil,
				fakePackageQuery{}, fstest.MapFS{}, "/home/tester")
			if err != nil {
				t.Fatalf("DetectDesktop() error = %v", err)
			}
			if component.Status != tt.want {
				t.Fatalf("Status = %q, want %q; component: %#v", component.Status, tt.want, component)
			}
		})
	}
}

func TestDetectDesktopInstalledPackageRequiresExecutable(t *testing.T) {
	spec := desktopSpecsByID(DesktopSpecs())["claude-desktop"]
	directory := t.TempDir()
	executable := writeExecutable(t, directory, spec.ExecutableName, 0o700)
	packages := fakePackageQuery{versions: map[string]string{spec.PackageName: "1.2.3"}}

	component, err := DetectDesktop(context.Background(), spec, supportedUbuntu("20.04"), []string{directory},
		packages, fstest.MapFS{}, "/home/tester")
	if err != nil {
		t.Fatalf("DetectDesktop() error = %v", err)
	}
	if component.Status != domain.StatusInstalled || !strings.Contains(strings.ToLower(component.Message), "unsupported") {
		t.Fatalf("component = %#v, want installed with compatibility warning", component)
	}
	wantInstallation := domain.Installation{
		Path: executable, ResolvedPath: executable, Version: "1.2.3", Source: "dpkg", Managed: false,
	}
	if !reflect.DeepEqual(component.Installations, []domain.Installation{wantInstallation}) {
		t.Fatalf("Installations = %#v, want %#v", component.Installations, []domain.Installation{wantInstallation})
	}

	broken, err := DetectDesktop(context.Background(), spec, supportedUbuntu("22.04"), nil,
		packages, fstest.MapFS{}, "/home/tester")
	if err != nil {
		t.Fatalf("DetectDesktop() broken case error = %v", err)
	}
	if broken.Status != domain.StatusBroken || len(broken.Installations) != 0 {
		t.Fatalf("package without executable = %#v, want broken without invented path", broken)
	}
}

func TestDetectDesktopExternalDesktopAndExecutableIsInstalled(t *testing.T) {
	spec := desktopSpecsByID(DesktopSpecs())["cc-switch"]
	directory := t.TempDir()
	executable := writeExecutable(t, directory, spec.ExecutableName, 0o700)
	root := fstest.MapFS{
		"home/tester/.local/share/applications/" + spec.DesktopFileName: &fstest.MapFile{
			Data: []byte("[Desktop Entry]\nExec=/definitely/not/executed --dangerous\n"), Mode: 0o600,
		},
	}

	component, err := DetectDesktop(context.Background(), spec, supportedUbuntu("22.04"), []string{directory},
		fakePackageQuery{}, root, "/home/tester")
	if err != nil {
		t.Fatalf("DetectDesktop() error = %v", err)
	}
	want := []domain.Installation{{Path: executable, ResolvedPath: executable, Source: "desktop"}}
	if component.Status != domain.StatusInstalled || !reflect.DeepEqual(component.Installations, want) {
		t.Fatalf("component = %#v, want installed external evidence %#v", component, want)
	}
}

func TestDetectDesktopConflictingExecutablesAreStable(t *testing.T) {
	spec := desktopSpecsByID(DesktopSpecs())["opencode-desktop"]
	firstDir := t.TempDir()
	secondDir := t.TempDir()
	first := writeExecutable(t, firstDir, spec.ExecutableName, 0o700)
	second := writeExecutable(t, secondDir, spec.ExecutableName, 0o700)
	root := fstest.MapFS{
		"usr/share/applications/" + spec.DesktopFileName: &fstest.MapFile{Data: []byte("ignored"), Mode: 0o644},
	}

	component, err := DetectDesktop(context.Background(), spec, supportedUbuntu("22.04"), []string{secondDir, firstDir, secondDir},
		fakePackageQuery{}, root, "/home/tester")
	if err != nil {
		t.Fatalf("DetectDesktop() error = %v", err)
	}
	wantPaths := []string{first, second}
	if wantPaths[1] < wantPaths[0] {
		wantPaths[0], wantPaths[1] = wantPaths[1], wantPaths[0]
	}
	if component.Status != domain.StatusConflict || len(component.Installations) != 2 {
		t.Fatalf("component = %#v, want conflict with two installations", component)
	}
	for index, want := range wantPaths {
		if component.Installations[index].Path != want {
			t.Fatalf("installation paths = %#v, want %#v", component.Installations, wantPaths)
		}
	}
}

func TestDetectDesktopRejectsUnsafeEvidence(t *testing.T) {
	spec := desktopSpecsByID(DesktopSpecs())["cc-switch"]
	directory := t.TempDir()
	writeExecutable(t, directory, spec.ExecutableName, 0o600)
	root := fstest.MapFS{
		"home/escape/.local/share/applications/" + spec.DesktopFileName: &fstest.MapFile{Data: []byte("ignored"), Mode: 0o644},
		"usr/share/applications/" + spec.DesktopFileName:                &fstest.MapFile{Data: []byte("../../../../secret"), Mode: fs.ModeSymlink | 0o777},
	}

	component, err := DetectDesktop(context.Background(), spec, supportedUbuntu("22.04"), []string{directory},
		fakePackageQuery{}, root, "/home/tester/../escape")
	if err != nil {
		t.Fatalf("DetectDesktop() error = %v", err)
	}
	if component.Status != domain.StatusMissing {
		t.Fatalf("unsafe home, desktop symlink, and non-executable candidate produced %#v, want missing", component)
	}

	malicious := spec
	malicious.DesktopFileName = "../../secret.desktop"
	if _, err := DetectDesktop(context.Background(), malicious, supportedUbuntu("22.04"), nil,
		fakePackageQuery{}, root, "/home/tester"); !hasPublicCode(err, domain.ErrInvalidResult) {
		t.Fatalf("invalid desktop basename error = %v, want INVALID_RESULT", err)
	}
}

func TestDetectDesktopPropagatesPackageAndFilesystemErrors(t *testing.T) {
	spec := desktopSpecsByID(DesktopSpecs())["cc-switch"]
	packageErr := errors.New("private package error")
	if _, err := DetectDesktop(context.Background(), spec, supportedUbuntu("22.04"), nil,
		fakePackageQuery{err: packageErr}, fstest.MapFS{}, "/home/tester"); !errors.Is(err, packageErr) {
		t.Fatalf("package error = %v, want wrapped/returned package error", err)
	}

	fsErr := errors.New("private filesystem error")
	if _, err := DetectDesktop(context.Background(), spec, supportedUbuntu("22.04"), nil,
		fakePackageQuery{}, failingFS{err: fsErr}, "/home/tester"); !errors.Is(err, fsErr) {
		t.Fatalf("filesystem error = %v, want wrapped/returned filesystem error", err)
	}
}

func TestDesktopComponentProbeDescriptorAndDelegation(t *testing.T) {
	spec := desktopSpecsByID(DesktopSpecs())["cockpit-tools"]
	probe := DesktopComponentProbe{Spec: spec, Packages: fakePackageQuery{}, FS: fstest.MapFS{}, Home: "/home/tester"}
	descriptor := probe.Descriptor()
	if descriptor.ID != spec.ID || descriptor.Name != spec.Name || descriptor.Category != spec.Category ||
		descriptor.Status != domain.StatusDetecting || descriptor.MinimumOS != "Ubuntu 20.04" {
		t.Fatalf("Descriptor() = %#v, want stable detecting descriptor", descriptor)
	}
	component, err := probe.Detect(context.Background(), supportedUbuntu("22.04"), nil)
	if err != nil || component.Status != domain.StatusMissing {
		t.Fatalf("Detect() = (%#v, %v), want delegated missing result", component, err)
	}
}

func TestDpkgQueryFixedRequestAndInstalledResult(t *testing.T) {
	runner := &desktopRecordingRunner{result: platform.CommandResult{
		ExitCode: 0, Stdout: "install ok installed\t1.2.3-1ubuntu1\n",
	}}
	version, installed, err := (DpkgQuery{Runner: runner}).InstalledVersion(context.Background(), "claude-desktop")
	if err != nil || !installed || version != "1.2.3-1ubuntu1" {
		t.Fatalf("InstalledVersion() = (%q, %t, %v)", version, installed, err)
	}
	want := platform.CommandRequest{
		Path: "/usr/bin/dpkg-query", Args: []string{"-W", "-f=${Status}\t${Version}", "claude-desktop"},
		Timeout: 3 * time.Second, OutputLimit: 64 * 1024,
	}
	if !reflect.DeepEqual(runner.requests, []platform.CommandRequest{want}) {
		t.Fatalf("requests = %#v, want fixed request %#v", runner.requests, []platform.CommandRequest{want})
	}
}

func TestDpkgQueryNotInstalledAndNonzeroFailures(t *testing.T) {
	notInstalledRunner := &desktopRecordingRunner{
		result: platform.CommandResult{ExitCode: 1, Stderr: "dpkg-query localized/private not-found detail"},
		err:    domain.NewPublicError(domain.ErrCommandFailed, "command failed", errors.New("private")),
	}
	version, installed, err := (DpkgQuery{Runner: notInstalledRunner}).InstalledVersion(context.Background(), "missing-package")
	if err != nil || installed || version != "" {
		t.Fatalf("not installed = (%q, %t, %v), want empty, false, nil", version, installed, err)
	}

	failure := errors.New("private runner failure")
	_, _, err = (DpkgQuery{Runner: &desktopRecordingRunner{
		result: platform.CommandResult{ExitCode: 2, Stderr: "secret output"}, err: failure,
	}}).InstalledVersion(context.Background(), "claude-desktop")
	if !errors.Is(err, failure) {
		t.Fatalf("nonzero error = %v, want runner failure", err)
	}
	if strings.Contains(err.Error(), "secret output") {
		t.Fatalf("nonzero error leaks command output: %v", err)
	}
}

func TestDpkgQueryRejectsInvalidResultsWithoutOutputLeak(t *testing.T) {
	tests := []struct {
		name   string
		result platform.CommandResult
	}{
		{name: "malformed status", result: platform.CommandResult{ExitCode: 0, Stdout: "deinstall ok config-files\t1.2.3"}},
		{name: "missing version", result: platform.CommandResult{ExitCode: 0, Stdout: "install ok installed\t"}},
		{name: "invalid version", result: platform.CommandResult{ExitCode: 0, Stdout: "install ok installed\t1.2 private"}},
		{name: "extra field", result: platform.CommandResult{ExitCode: 0, Stdout: "install ok installed\t1.2.3\textra"}},
		{name: "multiline", result: platform.CommandResult{ExitCode: 0, Stdout: "install ok installed\t1.2.3\nsecret"}},
		{name: "truncated", result: platform.CommandResult{ExitCode: 0, Stdout: "private-prefix", Truncated: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := (DpkgQuery{Runner: &desktopRecordingRunner{result: tt.result}}).
				InstalledVersion(context.Background(), "claude-desktop")
			if !hasPublicCode(err, domain.ErrInvalidResult) {
				t.Fatalf("error = %v, want INVALID_RESULT", err)
			}
			if strings.Contains(err.Error(), tt.result.Stdout) && tt.result.Stdout != "" {
				t.Fatalf("error leaks stdout: %v", err)
			}
		})
	}
}

func TestDpkgQueryDoesNotMapTimedOutExitOneToNotInstalled(t *testing.T) {
	timeoutErr := domain.NewPublicError(domain.ErrCommandTimeout, "command timed out", errors.New("private"))
	_, installed, err := (DpkgQuery{Runner: &desktopRecordingRunner{
		result: platform.CommandResult{ExitCode: 1, TimedOut: true}, err: timeoutErr,
	}}).InstalledVersion(context.Background(), "claude-desktop")
	if installed || !errors.Is(err, timeoutErr) {
		t.Fatalf("timed out exit one = (installed %t, error %v), want false and timeout error", installed, err)
	}
}

func TestDpkgQueryCancellationDoesNotRunOrParse(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	runner := &desktopRecordingRunner{result: platform.CommandResult{ExitCode: 0, Stdout: "install ok installed\t1.2.3"}}
	_, _, err := (DpkgQuery{Runner: runner}).InstalledVersion(ctx, "claude-desktop")
	if !errors.Is(err, context.Canceled) || len(runner.requests) != 0 {
		t.Fatalf("canceled query error = %v, requests = %d; want context.Canceled and no execution", err, len(runner.requests))
	}
}

type fakePackageQuery struct {
	versions map[string]string
	err      error
}

func (query fakePackageQuery) InstalledVersion(_ context.Context, name string) (string, bool, error) {
	if query.err != nil {
		return "", false, query.err
	}
	version, ok := query.versions[name]
	return version, ok, nil
}

type desktopRecordingRunner struct {
	result   platform.CommandResult
	err      error
	requests []platform.CommandRequest
}

func (runner *desktopRecordingRunner) Run(_ context.Context, request platform.CommandRequest) (platform.CommandResult, error) {
	runner.requests = append(runner.requests, request)
	return runner.result, runner.err
}

type failingFS struct{ err error }

func (f failingFS) Open(string) (fs.File, error) { return nil, f.err }

func supportedUbuntu(version string) domain.SystemInfo {
	return domain.SystemInfo{Distribution: "Ubuntu", Version: version, Architecture: "x86_64", Supported: true}
}

func desktopSpecsByID(specs []DesktopSpec) map[string]DesktopSpec {
	result := make(map[string]DesktopSpec, len(specs))
	for _, spec := range specs {
		result[spec.ID] = spec
	}
	return result
}

func hasPublicCode(err error, code domain.ErrorCode) bool {
	var public *domain.PublicError
	return errors.As(err, &public) && public.Code == code
}

func TestDetectDesktopPathBoundaryDoesNotUseSiblingPrefix(t *testing.T) {
	spec := desktopSpecsByID(DesktopSpecs())["cc-switch"]
	base := t.TempDir()
	allowed := filepath.Join(base, "bin")
	sibling := filepath.Join(base, "binary")
	writeExecutable(t, sibling, spec.ExecutableName, 0o700)
	root := fstest.MapFS{
		"usr/share/applications/" + spec.DesktopFileName: &fstest.MapFile{Data: []byte("ignored"), Mode: 0o644},
	}
	component, err := DetectDesktop(context.Background(), spec, supportedUbuntu("22.04"), []string{allowed},
		fakePackageQuery{}, root, "/home/tester")
	if err != nil {
		t.Fatalf("DetectDesktop() error = %v", err)
	}
	if component.Status != domain.StatusBroken {
		t.Fatalf("sibling-prefix executable produced %#v, want broken desktop evidence", component)
	}
}

func TestDetectDesktopFilesystemPresenceDoesNotReadDesktopContents(t *testing.T) {
	spec := desktopSpecsByID(DesktopSpecs())["cc-switch"]
	directory := t.TempDir()
	writeExecutable(t, directory, spec.ExecutableName, 0o700)
	rootDir := t.TempDir()
	desktopDir := filepath.Join(rootDir, "usr", "local", "share", "applications")
	if err := os.MkdirAll(desktopDir, 0o755); err != nil {
		t.Fatal(err)
	}
	desktopPath := filepath.Join(desktopDir, spec.DesktopFileName)
	if err := os.WriteFile(desktopPath, make([]byte, 2<<20), 0o000); err != nil {
		t.Fatal(err)
	}
	component, err := DetectDesktop(context.Background(), spec, supportedUbuntu("22.04"), []string{directory},
		fakePackageQuery{}, os.DirFS(rootDir), "/home/tester")
	if err != nil || component.Status != domain.StatusInstalled {
		t.Fatalf("presence-only detection = (%#v, %v), want installed without reading desktop", component, err)
	}
}

func TestDetectDesktopRejectsSymlinkedApplicationDirectoryEscape(t *testing.T) {
	spec := desktopSpecsByID(DesktopSpecs())["cc-switch"]
	directory := t.TempDir()
	writeExecutable(t, directory, spec.ExecutableName, 0o700)
	rootDir := t.TempDir()
	escapeDir := t.TempDir()
	escapedApplications := filepath.Join(escapeDir, "share", "applications")
	if err := os.MkdirAll(escapedApplications, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(escapedApplications, spec.DesktopFileName), []byte("ignored"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(escapeDir, filepath.Join(rootDir, "usr")); err != nil {
		t.Fatal(err)
	}

	component, err := DetectDesktop(context.Background(), spec, supportedUbuntu("22.04"), []string{directory},
		fakePackageQuery{}, os.DirFS(rootDir), "/home/tester")
	if err != nil {
		t.Fatalf("DetectDesktop() error = %v", err)
	}
	if component.Status != domain.StatusMissing {
		t.Fatalf("symlinked application directory escape produced %#v, want missing", component)
	}
}
