package detect

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"testing"

	"github.com/Oswald-Hao/Osverse/internal/domain"
	"github.com/Oswald-Hao/Osverse/internal/platform"
)

var testCommandSpec = CommandSpec{
	ID:              "test-cli",
	Name:            "Test CLI",
	ExecutableNames: []string{"test-cli"},
	VersionArgs:     []string{"--version"},
	VersionPattern:  regexp.MustCompile(`^test-cli ([0-9]+\.[0-9]+\.[0-9]+)$`),
	MinimumOS:       "Ubuntu 20.04",
}

func TestCommandDetectorMissingWhenNoCandidateExists(t *testing.T) {
	runner := &fakeCommandRunner{}
	component := (CommandDetector{Runner: runner}).Detect(
		context.Background(), testCommandSpec, []string{t.TempDir()},
	)

	assertComponentIdentity(t, component)
	if component.Status != domain.StatusMissing {
		t.Fatalf("Status = %q, want %q", component.Status, domain.StatusMissing)
	}
	if len(component.Installations) != 0 {
		t.Fatalf("Installations = %#v, want none", component.Installations)
	}
	if len(runner.requests) != 0 {
		t.Fatalf("runner received %d calls without a candidate", len(runner.requests))
	}
}

func TestCommandDetectorInstalledUsesExplicitPathFixedArgsAndBounds(t *testing.T) {
	directory := t.TempDir()
	candidate := writeExecutable(t, directory, "test-cli", 0o700)
	runner := &fakeCommandRunner{outcomes: map[string]fakeCommandOutcome{
		candidate: {result: platform.CommandResult{ExitCode: 0, Stdout: "test-cli 1.2.3\n"}},
	}}

	component := (CommandDetector{Runner: runner}).Detect(
		context.Background(), testCommandSpec, []string{directory},
	)

	if component.Status != domain.StatusInstalled {
		t.Fatalf("Status = %q, want %q", component.Status, domain.StatusInstalled)
	}
	want := []domain.Installation{{
		Path: candidate, ResolvedPath: candidate, Version: "1.2.3", Source: "path", Managed: false,
	}}
	if !reflect.DeepEqual(component.Installations, want) {
		t.Fatalf("Installations = %#v, want %#v", component.Installations, want)
	}
	if len(runner.requests) != 1 {
		t.Fatalf("runner calls = %d, want 1", len(runner.requests))
	}
	request := runner.requests[0]
	if request.Path != candidate {
		t.Errorf("request Path = %q, want explicit candidate %q", request.Path, candidate)
	}
	if !reflect.DeepEqual(request.Args, []string{"--version"}) {
		t.Errorf("request Args = %#v, want fixed version args", request.Args)
	}
	if request.Timeout <= 0 || request.OutputLimit <= 0 {
		t.Errorf("request bounds = timeout %v, output %d; want positive", request.Timeout, request.OutputLimit)
	}
}

func TestCommandDetectorIgnoresFilesNotExecutableByCurrentUser(t *testing.T) {
	directory := t.TempDir()
	writeExecutable(t, directory, "test-cli", 0o010)
	runner := &fakeCommandRunner{}

	component := (CommandDetector{Runner: runner}).Detect(
		context.Background(), testCommandSpec, []string{directory},
	)

	if component.Status != domain.StatusMissing {
		t.Fatalf("Status = %q, want %q", component.Status, domain.StatusMissing)
	}
	if len(runner.requests) != 0 {
		t.Fatalf("runner received %d calls for a non-executable candidate", len(runner.requests))
	}
}

func TestCommandDetectorConflictsForTwoDistinctValidInstallations(t *testing.T) {
	directoryB := t.TempDir()
	directoryA := t.TempDir()
	candidateB := writeExecutable(t, directoryB, "test-cli", 0o700)
	candidateA := writeExecutable(t, directoryA, "test-cli", 0o700)
	runner := &fakeCommandRunner{outcomes: map[string]fakeCommandOutcome{
		candidateA: {result: platform.CommandResult{ExitCode: 0, Stdout: "test-cli 1.0.0"}},
		candidateB: {result: platform.CommandResult{ExitCode: 0, Stdout: "test-cli 2.0.0"}},
	}}

	component := (CommandDetector{Runner: runner}).Detect(
		context.Background(), testCommandSpec, []string{directoryB, directoryA},
	)

	if component.Status != domain.StatusConflict {
		t.Fatalf("Status = %q, want %q", component.Status, domain.StatusConflict)
	}
	if len(component.Installations) != 2 {
		t.Fatalf("Installations = %#v, want both valid installations", component.Installations)
	}
	paths := []string{component.Installations[0].Path, component.Installations[1].Path}
	if !sort.StringsAreSorted(paths) {
		t.Fatalf("installation paths are not sorted: %#v", paths)
	}
	if !reflect.DeepEqual(sortedStrings(candidateA, candidateB), paths) {
		t.Fatalf("installation paths = %#v, want %#v", paths, sortedStrings(candidateA, candidateB))
	}
}

func TestCommandDetectorBrokenVersionResults(t *testing.T) {
	tests := []struct {
		name    string
		outcome fakeCommandOutcome
	}{
		{
			name: "timeout",
			outcome: fakeCommandOutcome{
				result: platform.CommandResult{ExitCode: -1, TimedOut: true},
				err:    errors.New("deadline exceeded"),
			},
		},
		{
			name:    "unparsable output",
			outcome: fakeCommandOutcome{result: platform.CommandResult{ExitCode: 0, Stdout: "unknown build"}},
		},
		{
			name: "nonzero exit",
			outcome: fakeCommandOutcome{
				result: platform.CommandResult{ExitCode: 17, Stdout: "test-cli 9.9.9"},
				err:    errors.New("command failed"),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			directory := t.TempDir()
			candidate := writeExecutable(t, directory, "test-cli", 0o700)
			runner := &fakeCommandRunner{outcomes: map[string]fakeCommandOutcome{candidate: tt.outcome}}

			component := (CommandDetector{Runner: runner}).Detect(
				context.Background(), testCommandSpec, []string{directory},
			)

			if component.Status != domain.StatusBroken {
				t.Fatalf("Status = %q, want %q", component.Status, domain.StatusBroken)
			}
			if len(component.Installations) != 1 || component.Installations[0].Path != candidate {
				t.Fatalf("Installations = %#v, want broken candidate preserved", component.Installations)
			}
			if component.Installations[0].Version != "" {
				t.Fatalf("Version = %q for failed verification, want empty", component.Installations[0].Version)
			}
		})
	}
}

func TestCommandDetectorManagedUsesResolvedTargetAndSafeContainment(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	managedDirectory := filepath.Join(home, ".local", "share", "osverse", "tools", "test-cli", "1.2.3", "bin")
	prefixTrapDirectory := filepath.Join(home, ".local", "share", "osverse", "tools-evil")
	externalDirectory := t.TempDir()
	managedTarget := writeExecutable(t, managedDirectory, "managed-target", 0o700)
	prefixTrap := writeExecutable(t, prefixTrapDirectory, "test-cli", 0o700)
	externalTarget := writeExecutable(t, externalDirectory, "external-target", 0o700)
	aliasToManagedDirectory := t.TempDir()
	aliasToExternalDirectory := filepath.Join(home, ".local", "share", "osverse", "tools", "test-cli", "current", "bin")
	makeSymlink(t, managedTarget, filepath.Join(aliasToManagedDirectory, "test-cli"))
	makeSymlink(t, externalTarget, filepath.Join(aliasToExternalDirectory, "test-cli"))
	runner := &fakeCommandRunner{defaultOutcome: fakeCommandOutcome{
		result: platform.CommandResult{ExitCode: 0, Stdout: "test-cli 1.2.3"},
	}}

	component := (CommandDetector{Runner: runner}).Detect(context.Background(), testCommandSpec, []string{
		aliasToManagedDirectory,
		aliasToExternalDirectory,
		prefixTrapDirectory,
	})

	if component.Status != domain.StatusConflict {
		t.Fatalf("Status = %q, want %q", component.Status, domain.StatusConflict)
	}
	managedByResolvedPath := make(map[string]bool)
	sourceByResolvedPath := make(map[string]string)
	for _, installation := range component.Installations {
		managedByResolvedPath[installation.ResolvedPath] = installation.Managed
		sourceByResolvedPath[installation.ResolvedPath] = installation.Source
	}
	if !managedByResolvedPath[managedTarget] || sourceByResolvedPath[managedTarget] != "osverse" {
		t.Errorf("target inside managed tools = managed %t, source %q; want true, osverse",
			managedByResolvedPath[managedTarget], sourceByResolvedPath[managedTarget])
	}
	for _, target := range []string{externalTarget, prefixTrap} {
		if managedByResolvedPath[target] || sourceByResolvedPath[target] != "path" {
			t.Errorf("external target %q = managed %t, source %q; want false, path",
				target, managedByResolvedPath[target], sourceByResolvedPath[target])
		}
	}
}

func TestCommandDetectorDeduplicatesPhysicalTargetWithStableAlias(t *testing.T) {
	targetDirectory := t.TempDir()
	target := writeExecutable(t, targetDirectory, "real-test-cli", 0o700)
	aliasDirectoryB := t.TempDir()
	aliasDirectoryA := t.TempDir()
	aliasB := filepath.Join(aliasDirectoryB, "test-cli")
	aliasA := filepath.Join(aliasDirectoryA, "test-cli")
	makeSymlink(t, target, aliasB)
	makeSymlink(t, target, aliasA)
	runner := &fakeCommandRunner{defaultOutcome: fakeCommandOutcome{
		result: platform.CommandResult{ExitCode: 0, Stdout: "test-cli 1.2.3"},
	}}

	component := (CommandDetector{Runner: runner}).Detect(
		context.Background(), testCommandSpec, []string{aliasDirectoryB, aliasDirectoryA},
	)

	if component.Status != domain.StatusInstalled {
		t.Fatalf("Status = %q, want %q", component.Status, domain.StatusInstalled)
	}
	if len(component.Installations) != 1 || len(runner.requests) != 1 {
		t.Fatalf("installations = %d, runner calls = %d; want one physical target",
			len(component.Installations), len(runner.requests))
	}
	wantAlias := sortedStrings(aliasA, aliasB)[0]
	installation := component.Installations[0]
	if installation.Path != wantAlias || installation.ResolvedPath != target {
		t.Fatalf("installation = %#v, want stable alias %q resolving to %q", installation, wantAlias, target)
	}
}

func TestCommandDetectorIgnoresBrokenAndLoopingSymlinks(t *testing.T) {
	directory := t.TempDir()
	makeSymlink(t, filepath.Join(directory, "missing"), filepath.Join(directory, "test-cli"))
	loopDirectory := t.TempDir()
	makeSymlink(t, "test-cli", filepath.Join(loopDirectory, "test-cli"))
	runner := &fakeCommandRunner{}

	component := (CommandDetector{Runner: runner}).Detect(
		context.Background(), testCommandSpec, []string{directory, loopDirectory},
	)

	if component.Status != domain.StatusMissing {
		t.Fatalf("Status = %q, want %q", component.Status, domain.StatusMissing)
	}
	if len(runner.requests) != 0 {
		t.Fatalf("runner received %d calls for invalid symlinks", len(runner.requests))
	}
}

func TestCommandDetectorCanceledScanIsBrokenWithoutRunningCandidates(t *testing.T) {
	directory := t.TempDir()
	writeExecutable(t, directory, "test-cli", 0o700)
	runner := &fakeCommandRunner{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	component := (CommandDetector{Runner: runner}).Detect(ctx, testCommandSpec, []string{directory})

	if component.Status != domain.StatusBroken {
		t.Fatalf("Status = %q, want %q", component.Status, domain.StatusBroken)
	}
	if len(runner.requests) != 0 {
		t.Fatalf("runner received %d calls after cancellation", len(runner.requests))
	}
}

func TestCommandComponentProbeDescriptorAndDetect(t *testing.T) {
	directory := t.TempDir()
	candidate := writeExecutable(t, directory, "test-cli", 0o700)
	probe := CommandComponentProbe{
		Detector: CommandDetector{Runner: &fakeCommandRunner{outcomes: map[string]fakeCommandOutcome{
			candidate: {result: platform.CommandResult{ExitCode: 0, Stdout: "test-cli 3.2.1"}},
		}}},
		Spec: testCommandSpec,
	}

	descriptor := probe.Descriptor()
	assertComponentIdentity(t, descriptor)
	if descriptor.Status != domain.StatusDetecting || len(descriptor.Installations) != 0 {
		t.Fatalf("Descriptor() = %#v, want detecting descriptor without installations", descriptor)
	}
	component, err := probe.Detect(context.Background(), domain.SystemInfo{Supported: false}, []string{directory})
	if err != nil {
		t.Fatalf("Detect() error = %v, want nil", err)
	}
	if component.Status != domain.StatusInstalled || component.Installations[0].Version != "3.2.1" {
		t.Fatalf("Detect() = %#v, want delegated installed result", component)
	}
}

type fakeCommandOutcome struct {
	result platform.CommandResult
	err    error
}

type fakeCommandRunner struct {
	outcomes       map[string]fakeCommandOutcome
	defaultOutcome fakeCommandOutcome
	requests       []platform.CommandRequest
}

func (runner *fakeCommandRunner) Run(_ context.Context, request platform.CommandRequest) (platform.CommandResult, error) {
	runner.requests = append(runner.requests, request)
	if outcome, ok := runner.outcomes[request.Path]; ok {
		return outcome.result, outcome.err
	}
	return runner.defaultOutcome.result, runner.defaultOutcome.err
}

func writeExecutable(t *testing.T, directory, name string, mode os.FileMode) string {
	t.Helper()
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", directory, err)
	}
	path := filepath.Join(directory, name)
	if err := os.WriteFile(path, []byte("fixture"), mode); err != nil {
		t.Fatalf("WriteFile(%q): %v", path, err)
	}
	return path
}

func makeSymlink(t *testing.T, target, alias string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(alias), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", filepath.Dir(alias), err)
	}
	if err := os.Symlink(target, alias); err != nil {
		t.Fatalf("Symlink(%q, %q): %v", target, alias, err)
	}
}

func sortedStrings(values ...string) []string {
	result := append([]string(nil), values...)
	sort.Strings(result)
	return result
}

func assertComponentIdentity(t *testing.T, component domain.Component) {
	t.Helper()
	if component.ID != testCommandSpec.ID || component.Name != testCommandSpec.Name ||
		component.MinimumOS != testCommandSpec.MinimumOS {
		t.Fatalf("component identity = %#v, want ID %q, name %q, minimum OS %q",
			component, testCommandSpec.ID, testCommandSpec.Name, testCommandSpec.MinimumOS)
	}
}
