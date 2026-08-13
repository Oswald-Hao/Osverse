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
	root := desktopTestFS(t, "home/tester/.local/share/applications/"+spec.DesktopFileName,
		[]byte("[Desktop Entry]\nExec=/definitely/not/executed --dangerous\n"))

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
	root := desktopTestFS(t, "usr/share/applications/"+spec.DesktopFileName, []byte("ignored"))

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

func TestDetectDesktopBelowFloorMultipleExecutablesRemainInstalled(t *testing.T) {
	spec := desktopSpecsByID(DesktopSpecs())["claude-desktop"]
	firstDir := t.TempDir()
	secondDir := t.TempDir()
	writeExecutable(t, firstDir, spec.ExecutableName, 0o700)
	writeExecutable(t, secondDir, spec.ExecutableName, 0o700)
	component, err := DetectDesktop(context.Background(), spec, supportedUbuntu("20.04"), []string{secondDir, firstDir},
		fakePackageQuery{versions: map[string]string{spec.PackageName: "1.2.3"}}, fstest.MapFS{}, "/home/tester")
	if err != nil {
		t.Fatalf("DetectDesktop() error = %v", err)
	}
	if component.Status != domain.StatusInstalled || len(component.Installations) != 2 ||
		!strings.Contains(strings.ToLower(component.Message), "unsupported") {
		t.Fatalf("component = %#v, want installed with two paths and unsupported warning", component)
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
		ExitCode: 0, Stdout: "claude-desktop\tii \t2:1.2.3:vendor-1ubuntu1\n",
	}}
	version, installed, err := (DpkgQuery{Runner: runner}).InstalledVersion(context.Background(), "claude-desktop")
	if err != nil || !installed || version != "2:1.2.3:vendor-1ubuntu1" {
		t.Fatalf("InstalledVersion() = (%q, %t, %v)", version, installed, err)
	}
	want := platform.CommandRequest{
		Path: "/usr/bin/dpkg-query",
		Args: []string{"-W", "-f=${Package}\t${db:Status-Abbrev}\t${Version}", "claude-desktop"},
		Env:  []string{"LC_ALL=C"}, Timeout: 3 * time.Second, OutputLimit: 64 * 1024,
	}
	if !reflect.DeepEqual(runner.requests, []platform.CommandRequest{want}) {
		t.Fatalf("requests = %#v, want fixed request %#v", runner.requests, []platform.CommandRequest{want})
	}
}

func TestDpkgQueryInstalledUsesCurrentStatusAndAcceptsReinstreq(t *testing.T) {
	for _, status := range []string{
		"ui ", "ii ", "hi ", "ri ", "pi ",
		"uiR", "iiR", "hiR", "riR", "piR",
	} {
		t.Run(strings.ReplaceAll(status, " ", "space"), func(t *testing.T) {
			version, installed, err := (DpkgQuery{Runner: &desktopRecordingRunner{result: platform.CommandResult{
				ExitCode: 0, Stdout: "claude-desktop\t" + status + "\t1.2.3",
			}}}).InstalledVersion(context.Background(), "claude-desktop")
			if err != nil || !installed || version != "1.2.3" {
				t.Fatalf("status %q = (%q, %t, %v), want installed", status, version, installed, err)
			}
		})
	}
}

func TestDpkgQueryMultiarchPackageIdentity(t *testing.T) {
	runner := &desktopRecordingRunner{result: platform.CommandResult{
		ExitCode: 0, Stdout: "libc6\tii \t2.35-0ubuntu3.14",
	}}
	version, installed, err := (DpkgQuery{Runner: runner}).InstalledVersion(context.Background(), "libc6:amd64")
	if err != nil || !installed || version != "2.35-0ubuntu3.14" {
		t.Fatalf("multiarch query = (%q, %t, %v), want installed", version, installed, err)
	}
	wantArgs := []string{"-W", "-f=${Package}\t${db:Status-Abbrev}\t${Version}", "libc6:amd64"}
	if len(runner.requests) != 1 || !reflect.DeepEqual(runner.requests[0].Args, wantArgs) {
		t.Fatalf("request args = %#v, want %#v", runner.requests, wantArgs)
	}
	hyphenatedRunner := &desktopRecordingRunner{result: platform.CommandResult{
		ExitCode: 0, Stdout: "libc6\tii \t2.35-0ubuntu3.14",
	}}
	if _, installed, err := (DpkgQuery{Runner: hyphenatedRunner}).
		InstalledVersion(context.Background(), "libc6:linux-amd64"); err != nil || !installed {
		t.Fatalf("hyphenated architecture = (installed %t, error %v), want installed", installed, err)
	}

	for _, invalid := range []string{
		"libc6:amd64:evil", "libc6:", ":amd64", "libc6:amd64/evil",
		"libc6:AMD64", "libc6:-amd64", "libc6:amd64-", "libc6:amd--64", "libc6:amd.64",
	} {
		invalidRunner := &desktopRecordingRunner{}
		if _, _, err := (DpkgQuery{Runner: invalidRunner}).InstalledVersion(context.Background(), invalid); !hasPublicCode(err, domain.ErrInvalidResult) {
			t.Fatalf("package %q error = %v, want INVALID_RESULT", invalid, err)
		}
		if len(invalidRunner.requests) != 0 {
			t.Fatalf("package %q executed %d request(s), want zero", invalid, len(invalidRunner.requests))
		}
	}
	for _, outputName := range []string{"libc", "libc60", "xlibc6", "libc6-extra", "libc6:amd64"} {
		_, _, err := (DpkgQuery{Runner: &desktopRecordingRunner{result: platform.CommandResult{
			ExitCode: 0, Stdout: outputName + "\tii \t2.35-0ubuntu3.14",
		}}}).InstalledVersion(context.Background(), "libc6:amd64")
		if !hasPublicCode(err, domain.ErrInvalidResult) {
			t.Fatalf("output package %q error = %v, want INVALID_RESULT", outputName, err)
		}
	}
}

func TestDpkgQueryNotInstalledAndNonzeroFailures(t *testing.T) {
	notInstalledRunner := &desktopRecordingRunner{
		result: platform.CommandResult{ExitCode: 1, Stderr: "dpkg-query: no packages found matching missing-package\n"},
		err:    domain.NewPublicError(domain.ErrCommandFailed, "command failed", errors.New("private")),
	}
	version, installed, err := (DpkgQuery{Runner: notInstalledRunner}).InstalledVersion(context.Background(), "missing-package")
	if err != nil || installed || version != "" {
		t.Fatalf("not installed = (%q, %t, %v), want empty, false, nil", version, installed, err)
	}

	operationalFailures := []struct {
		name   string
		result platform.CommandResult
		err    error
	}{
		{
			name:   "database permission error on exit one",
			result: platform.CommandResult{ExitCode: 1, Stderr: "dpkg-query: error: cannot read database: Permission denied\n", Truncated: true},
			err:    domain.NewPublicError(domain.ErrCommandFailed, "command failed", errors.New("permission private")),
		},
		{
			name:   "launch failure",
			result: platform.CommandResult{ExitCode: -1},
			err:    errors.New("private launch failure"),
		},
	}
	for _, tt := range operationalFailures {
		t.Run(tt.name, func(t *testing.T) {
			_, _, gotErr := (DpkgQuery{Runner: &desktopRecordingRunner{result: tt.result, err: tt.err}}).
				InstalledVersion(context.Background(), "claude-desktop")
			if !errors.Is(gotErr, tt.err) {
				t.Fatalf("error = %v, want operational runner error", gotErr)
			}
			if strings.Contains(gotErr.Error(), tt.result.Stderr) && tt.result.Stderr != "" {
				t.Fatalf("error leaks command output: %v", gotErr)
			}
		})
	}
}

func TestDpkgQueryValidNoninstalledStates(t *testing.T) {
	tests := []struct {
		name    string
		status  string
		version string
	}{
		{name: "config files remain", status: "rc ", version: "1.2.3-1"},
		{name: "not installed", status: "un ", version: ""},
		{name: "half installed and reinstreq", status: "iHR", version: "1.2.3"},
		{name: "unpacked", status: "iU ", version: "1.2.3"},
		{name: "half configured", status: "iF ", version: "2:1.0:vendor-3"},
		{name: "triggers awaiting", status: "iW ", version: "1.2.3"},
		{name: "triggers pending", status: "it ", version: "1.2.3"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout := "claude-desktop\t" + tt.status + "\t" + tt.version
			version, installed, err := (DpkgQuery{Runner: &desktopRecordingRunner{
				result: platform.CommandResult{ExitCode: 0, Stdout: stdout},
			}}).InstalledVersion(context.Background(), "claude-desktop")
			if err != nil || installed || version != tt.version {
				t.Fatalf("InstalledVersion() = (%q, %t, %v), want (%q, false, nil)", version, installed, err, tt.version)
			}
		})
	}
}

func TestDpkgQueryRejectsInvalidResultsWithoutOutputLeak(t *testing.T) {
	tests := []struct {
		name   string
		result platform.CommandResult
	}{
		{name: "malformed status", result: platform.CommandResult{ExitCode: 0, Stdout: "claude-desktop\tzz \t1.2.3"}},
		{name: "undocumented desired state", result: platform.CommandResult{ExitCode: 0, Stdout: "claude-desktop\twh \t1.2.3"}},
		{name: "wrong status case", result: platform.CommandResult{ExitCode: 0, Stdout: "claude-desktop\tih \t1.2.3"}},
		{name: "unknown error flag", result: platform.CommandResult{ExitCode: 0, Stdout: "claude-desktop\tiiX\t1.2.3"}},
		{name: "short status", result: platform.CommandResult{ExitCode: 0, Stdout: "claude-desktop\tii\t1.2.3"}},
		{name: "wrong package", result: platform.CommandResult{ExitCode: 0, Stdout: "other-package\tii \t1.2.3"}},
		{name: "missing installed version", result: platform.CommandResult{ExitCode: 0, Stdout: "claude-desktop\tii \t"}},
		{name: "invalid version", result: platform.CommandResult{ExitCode: 0, Stdout: "claude-desktop\tii \t1.2 private"}},
		{name: "extra field", result: platform.CommandResult{ExitCode: 0, Stdout: "claude-desktop\tii \t1.2.3\textra"}},
		{name: "multiline", result: platform.CommandResult{ExitCode: 0, Stdout: "claude-desktop\tii \t1.2.3\nsecret"}},
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

func TestValidDpkgVersionStructuralGrammar(t *testing.T) {
	valid := []string{
		"1", "1.2.3-1ubuntu1", "2:1.0~rc.1+git:vendor-3+deb12u1", "1:1-2-3", "2:1.0:vendor",
	}
	for _, version := range valid {
		if !validDpkgVersion(version) {
			t.Errorf("validDpkgVersion(%q) = false, want true", version)
		}
	}
	invalid := []string{
		"", ":1.0", "x:1.0", "1:", "1.0:vendor", "1-", "-1", "1-1:bad", "1.0-1-bad:revision",
		"1.0 bad", "1.0\n1", "1.0\x00bad",
	}
	for _, version := range invalid {
		if validDpkgVersion(version) {
			t.Errorf("validDpkgVersion(%q) = true, want false", version)
		}
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
	runner := &desktopRecordingRunner{result: platform.CommandResult{ExitCode: 0, Stdout: "claude-desktop\tii \t1.2.3"}}
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

func desktopTestFS(t *testing.T, path string, data []byte) fs.FS {
	t.Helper()
	return os.DirFS(writeDesktopTestRoot(t, path, data))
}

func writeDesktopTestRoot(t *testing.T, path string, data []byte) string {
	t.Helper()
	root := t.TempDir()
	fullPath := filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fullPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

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
	root := desktopTestFS(t, "usr/share/applications/"+spec.DesktopFileName, []byte("ignored"))
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
	if err := os.WriteFile(desktopPath, make([]byte, 2<<20), 0o400); err != nil {
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

func TestDetectDesktopRejectsProductionFinalDesktopSymlink(t *testing.T) {
	spec := desktopSpecsByID(DesktopSpecs())["cc-switch"]
	directory := t.TempDir()
	writeExecutable(t, directory, spec.ExecutableName, 0o700)
	rootDir := t.TempDir()
	desktopDir := filepath.Join(rootDir, "usr", "share", "applications")
	if err := os.MkdirAll(desktopDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/etc/passwd", filepath.Join(desktopDir, spec.DesktopFileName)); err != nil {
		t.Fatal(err)
	}
	component, err := DetectDesktop(context.Background(), spec, supportedUbuntu("22.04"), []string{directory},
		fakePackageQuery{}, os.DirFS(rootDir), "/home/tester")
	if err != nil {
		t.Fatalf("DetectDesktop() error = %v", err)
	}
	if component.Status != domain.StatusMissing {
		t.Fatalf("final desktop symlink produced %#v, want missing", component)
	}
}

func TestFixedDesktopEvidencePinsOpenedRegularFile(t *testing.T) {
	rootDir := t.TempDir()
	directory := filepath.Join(rootDir, "usr", "share", "applications")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	desktopPath := filepath.Join(directory, "cc-switch.desktop")
	if err := os.WriteFile(desktopPath, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	evidence, err := fixedDesktopEvidence(context.Background(), os.DirFS(rootDir), "/home/tester", "cc-switch.desktop")
	if err != nil || len(evidence) != 1 {
		t.Fatalf("fixedDesktopEvidence() = (%#v, %v), want one pinned file", evidence, err)
	}
	defer closeDesktopEvidence(evidence)
	if err := os.Remove(desktopPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/etc/passwd", desktopPath); err != nil {
		t.Fatal(err)
	}
	if !desktopEvidenceUnchanged(evidence) {
		t.Fatal("pinned desktop evidence became invalid after pathname replacement")
	}
}

func TestDetectDesktopExecutableAliasSwapFailsEvidence(t *testing.T) {
	spec := desktopSpecsByID(DesktopSpecs())["cc-switch"]
	directory := t.TempDir()
	executable := writeExecutable(t, directory, spec.ExecutableName, 0o700)
	replacement := writeExecutable(t, t.TempDir(), spec.ExecutableName, 0o700)
	desktopPath := "usr/share/applications/" + spec.DesktopFileName
	rootDir := writeDesktopTestRoot(t, desktopPath, []byte("ignored"))
	root := &statActionFS{
		FS:       &genericDirFS{root: rootDir, target: desktopPath},
		actionAt: 2,
		action: func() {
			if err := os.Remove(executable); err != nil {
				t.Errorf("Remove executable: %v", err)
				return
			}
			if err := os.Symlink(replacement, executable); err != nil {
				t.Errorf("Symlink replacement: %v", err)
			}
		},
	}
	component, err := DetectDesktop(context.Background(), spec, supportedUbuntu("22.04"), []string{directory},
		fakePackageQuery{}, root, "/home/tester")
	if err != nil {
		t.Fatalf("DetectDesktop() error = %v", err)
	}
	if component.Status != domain.StatusBroken || len(component.Installations) != 0 {
		t.Fatalf("swapped executable produced %#v, want broken without stale evidence", component)
	}
}

func TestFixedDesktopEvidenceFailsClosedWhenGenericIdentityIsUnavailable(t *testing.T) {
	root := fstest.MapFS{
		"usr/share/applications/cc-switch.desktop": &fstest.MapFile{Data: []byte("ignored"), Mode: 0o644},
	}
	evidence, err := fixedDesktopEvidence(context.Background(), root, "/home/tester", "cc-switch.desktop")
	closeDesktopEvidence(evidence)
	if !hasPublicCode(err, domain.ErrInvalidResult) {
		t.Fatalf("fixedDesktopEvidence() error = %v, want fail-closed INVALID_RESULT", err)
	}
}

func TestFixedDesktopEvidenceUsesOnePinnedOpenOnGenericFS(t *testing.T) {
	rootDir := t.TempDir()
	path := "usr/share/applications/cc-switch.desktop"
	desktopPath := filepath.Join(rootDir, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(desktopPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(desktopPath, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	root := &genericDirFS{root: rootDir, target: path}
	evidence, err := fixedDesktopEvidence(context.Background(), root, "/home/tester", "cc-switch.desktop")
	if err != nil || len(evidence) != 1 {
		closeDesktopEvidence(evidence)
		t.Fatalf("fixedDesktopEvidence() = (%#v, %v), want one pinned generic file", evidence, err)
	}
	defer closeDesktopEvidence(evidence)
	if root.targetOpens != 1 {
		t.Fatalf("target opens = %d, want exactly one", root.targetOpens)
	}
	if err := os.Remove(desktopPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/etc/passwd", desktopPath); err != nil {
		t.Fatal(err)
	}
	if !desktopEvidenceUnchanged(evidence) {
		t.Fatal("generic pinned evidence became invalid after pathname replacement")
	}
}

func TestFixedDesktopEvidenceFailsClosedOnMutableGenericSysNilFile(t *testing.T) {
	path := "usr/share/applications/cc-switch.desktop"
	root := &mutableGenericFS{MapFS: fstest.MapFS{
		path: &fstest.MapFile{Data: []byte("original"), Mode: 0o644},
	}, path: path}
	evidence, err := fixedDesktopEvidence(context.Background(), root, "/home/tester", "cc-switch.desktop")
	closeDesktopEvidence(evidence)
	if !root.swapped {
		t.Fatal("mutable generic filesystem did not exercise the swap")
	}
	if !root.rootClosed || !root.targetClosed {
		t.Fatalf("closed handles = (root %t, target %t), want both true", root.rootClosed, root.targetClosed)
	}
	if !hasPublicCode(err, domain.ErrInvalidResult) {
		t.Fatalf("fixedDesktopEvidence() error = %v, want fail-closed INVALID_RESULT", err)
	}
}

type genericDirFS struct {
	root        string
	target      string
	targetOpens int
}

func (fsys *genericDirFS) Open(name string) (fs.File, error) {
	file, err := os.DirFS(fsys.root).Open(name)
	if err != nil {
		return nil, err
	}
	if name == fsys.target {
		fsys.targetOpens++
	}
	if name == "." {
		return genericFile{File: file}, nil
	}
	return file, nil
}

type genericFile struct{ fs.File }

type mutableGenericFS struct {
	fstest.MapFS
	path         string
	swapped      bool
	rootClosed   bool
	targetClosed bool
}

func (fsys *mutableGenericFS) Open(name string) (fs.File, error) {
	file, err := fsys.MapFS.Open(name)
	if err != nil {
		return nil, err
	}
	if name == fsys.path {
		fsys.MapFS[name] = &fstest.MapFile{Data: []byte("replacement"), Mode: 0o644}
		fsys.swapped = true
		return &closeTrackingFile{File: file, closed: &fsys.targetClosed}, nil
	}
	if name == "." {
		return &closeTrackingFile{File: file, closed: &fsys.rootClosed}, nil
	}
	return file, nil
}

type closeTrackingFile struct {
	fs.File
	closed *bool
}

func (file *closeTrackingFile) Close() error {
	*file.closed = true
	return file.File.Close()
}

type statActionFS struct {
	fs.FS
	actionAt int
	stats    int
	action   func()
}

func (fsys *statActionFS) Open(name string) (fs.File, error) {
	file, err := fsys.FS.Open(name)
	if err != nil {
		return nil, err
	}
	if filepath.Base(name) != "cc-switch.desktop" {
		return file, nil
	}
	return &statActionFile{File: file, owner: fsys}, nil
}

type statActionFile struct {
	fs.File
	owner *statActionFS
}

func (file *statActionFile) Stat() (fs.FileInfo, error) {
	file.owner.stats++
	if file.owner.stats == file.owner.actionAt {
		file.owner.action()
	}
	return file.File.Stat()
}
