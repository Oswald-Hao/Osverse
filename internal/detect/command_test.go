package detect

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Oswald-Hao/Osverse/internal/domain"
	"github.com/Oswald-Hao/Osverse/internal/platform"
	platformlinux "github.com/Oswald-Hao/Osverse/internal/platform/linux"
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
	if request.Timeout != 3*time.Second || request.OutputLimit != 64*1024 {
		t.Errorf("request bounds = timeout %v, output %d; want 3s and 65536", request.Timeout, request.OutputLimit)
	}
	if request.PinnedExecutable != nil {
		t.Error("non-ELF request unexpectedly pins executable")
	}
}

func TestCommandDetectorIgnoresFilesNotExecutableByCurrentUser(t *testing.T) {
	directory := t.TempDir()
	writeExecutable(t, directory, "test-cli", 0o600)
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
		{
			name: "truncated parsable output",
			outcome: fakeCommandOutcome{result: platform.CommandResult{
				ExitCode: 0, Stdout: "test-cli 9.9.9", Truncated: true,
			}},
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

func TestCommandDetectorMixedValidAndBrokenIsBroken(t *testing.T) {
	directories := []string{t.TempDir(), t.TempDir(), t.TempDir()}
	runner := &fakeCommandRunner{outcomes: make(map[string]fakeCommandOutcome)}
	for index, directory := range directories {
		candidate := writeExecutable(t, directory, "test-cli", 0o700)
		output := "test-cli 1.2.3"
		if index == len(directories)-1 {
			output = "unparsable"
		}
		runner.outcomes[candidate] = fakeCommandOutcome{result: platform.CommandResult{ExitCode: 0, Stdout: output}}
	}

	component := (CommandDetector{Runner: runner}).Detect(context.Background(), testCommandSpec, directories)

	if component.Status != domain.StatusBroken {
		t.Fatalf("Status = %q, want %q for verified and broken candidates", component.Status, domain.StatusBroken)
	}
	if len(component.Installations) != 3 {
		t.Fatalf("Installations = %#v, want all candidates preserved", component.Installations)
	}
}

func TestCommandDetectorVersionOutputUsesStdoutThenStderrAndFirstCapture(t *testing.T) {
	tests := []struct {
		name   string
		result platform.CommandResult
		want   string
	}{
		{
			name: "stdout wins",
			result: platform.CommandResult{ExitCode: 0,
				Stdout: "test-cli 1.2.3 build-a", Stderr: "test-cli 9.9.9 build-z"},
			want: "1.2.3",
		},
		{
			name:   "stderr fallback",
			result: platform.CommandResult{ExitCode: 0, Stderr: "test-cli 4.5.6 build-z"},
			want:   "4.5.6",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			directory := t.TempDir()
			candidate := writeExecutable(t, directory, "test-cli", 0o700)
			spec := testCommandSpec
			spec.VersionPattern = regexp.MustCompile(`^test-cli ([0-9]+\.[0-9]+\.[0-9]+) (build-[a-z])$`)
			component := (CommandDetector{Runner: &fakeCommandRunner{
				outcomes: map[string]fakeCommandOutcome{candidate: {result: tt.result}},
			}}).Detect(context.Background(), spec, []string{directory})

			if component.Status != domain.StatusInstalled || component.Installations[0].Version != tt.want {
				t.Fatalf("component = %#v, want installed version %q", component, tt.want)
			}
		})
	}
}

func TestCommandDetectorMissingVersionCaptureIsBroken(t *testing.T) {
	patterns := []*regexp.Regexp{nil, regexp.MustCompile(`^test-cli [0-9]+\.[0-9]+\.[0-9]+$`)}
	for _, pattern := range patterns {
		directory := t.TempDir()
		candidate := writeExecutable(t, directory, "test-cli", 0o700)
		spec := testCommandSpec
		spec.VersionPattern = pattern
		component := (CommandDetector{Runner: &fakeCommandRunner{
			outcomes: map[string]fakeCommandOutcome{candidate: {
				result: platform.CommandResult{ExitCode: 0, Stdout: "test-cli 1.2.3"},
			}},
		}}).Detect(context.Background(), spec, []string{directory})

		if component.Status != domain.StatusBroken {
			t.Fatalf("pattern %v produced Status %q, want %q", pattern, component.Status, domain.StatusBroken)
		}
	}
}

func TestCommandDetectorExecutesPinnedObjectWhenAliasChanges(t *testing.T) {
	directory := t.TempDir()
	alias := filepath.Join(directory, "test-cli")
	original, err := filepath.Abs(os.Args[0])
	if err != nil {
		t.Fatalf("absolute test executable path: %v", err)
	}
	makeSymlink(t, original, alias)
	replacement := filepath.Join(directory, "replacement")
	if err := os.WriteFile(replacement, []byte("#!/bin/sh\nprintf 'test-cli 9.9.9'"), 0o700); err != nil {
		t.Fatalf("write replacement: %v", err)
	}
	spec := testCommandSpec
	spec.VersionArgs = []string{"-test.run=^TestCommandDetectorPinnedExecutableHelper$", "--", "detector-version"}
	runner := &swappingCommandRunner{
		delegate:    platformlinux.NewExecRunner(),
		alias:       alias,
		replacement: replacement,
		wantPinned:  true,
	}

	component := (CommandDetector{Runner: runner}).Detect(context.Background(), spec, []string{directory})

	if component.Status != domain.StatusInstalled || component.Installations[0].Version != "1.2.3" {
		t.Fatalf("component = %#v, want version from pinned original object", component)
	}
	if component.Installations[0].ResolvedPath != original {
		t.Fatalf("ResolvedPath = %q, want pinned original %q", component.Installations[0].ResolvedPath, original)
	}
	if runner.recordedPinned == nil {
		t.Fatal("ELF runner did not receive a pinned executable")
	} else if _, err := runner.recordedPinned.Stat(); err == nil {
		t.Error("pinned ELF executable remains open after Detect returned")
	}
}

func TestCommandDetectorDirectEnvShellScriptPreservesPathSiblingAndStdinEOF(t *testing.T) {
	directory := t.TempDir()
	commandPath := filepath.Join(directory, "test-cli")
	resourcePath := filepath.Join(directory, "version")
	if err := os.WriteFile(resourcePath, []byte("1.2.3"), 0o600); err != nil {
		t.Fatalf("write sibling resource: %v", err)
	}
	contents := "#!/usr/bin/env sh\nresource=${0%/*}/version\nIFS= read -r version < \"$resource\"\nif IFS= read -r unexpected; then exit 9; fi\nprintf 'test-cli %s path=%s stdin=eof' \"$version\" \"$0\"\n"
	if err := os.WriteFile(commandPath, []byte(contents), 0o700); err != nil {
		t.Fatalf("write executable script: %v", err)
	}
	spec := testCommandSpec
	spec.VersionPattern = regexp.MustCompile(`^test-cli ([0-9]+\.[0-9]+\.[0-9]+) path=` + regexp.QuoteMeta(commandPath) + ` stdin=eof$`)
	runner := &recordingCommandRunner{delegate: platformlinux.NewExecRunner()}

	component := (CommandDetector{Runner: runner}).Detect(context.Background(), spec, []string{directory})

	if component.Status != domain.StatusInstalled || component.Installations[0].Version != "1.2.3" {
		t.Fatalf("component = %#v, want direct generic script semantics", component)
	}
	if len(runner.requests) != 1 || runner.requests[0].PinnedExecutable != nil {
		t.Fatalf("request = %#v, want direct-path script execution without pinned field", runner.requests)
	}
}

func TestCommandDetectorScriptReplacementBeforeExecutionIsBroken(t *testing.T) {
	directory := t.TempDir()
	commandPath := writeVersionScript(t, directory, "test-cli", "1.2.3", "")
	replacement := writeVersionScript(t, directory, "replacement", "9.9.9", "")
	runner := &swappingCommandRunner{
		delegate:    platformlinux.NewExecRunner(),
		alias:       commandPath,
		replacement: replacement,
		wantPinned:  false,
	}

	component := (CommandDetector{Runner: runner}).Detect(context.Background(), testCommandSpec, []string{directory})

	assertBrokenWithoutStaleInstallation(t, component)
}

func TestCommandDetectorScriptReplacementDuringExecutionIsBroken(t *testing.T) {
	directory := t.TempDir()
	marker := filepath.Join(directory, "started")
	commandPath := writeVersionScript(t, directory, "test-cli", "1.2.3", marker)
	replacement := writeVersionScript(t, directory, "replacement", "9.9.9", "")
	runner := &duringExecutionSwappingRunner{
		delegate:    platformlinux.NewExecRunner(),
		alias:       commandPath,
		replacement: replacement,
		marker:      marker,
	}

	component := (CommandDetector{Runner: runner}).Detect(context.Background(), testCommandSpec, []string{directory})

	assertBrokenWithoutStaleInstallation(t, component)
}

func TestCommandDetectorMidEnumerationCancellationDoesNotRunOrReturnMissing(t *testing.T) {
	directory := t.TempDir()
	writeExecutable(t, directory, "first", 0o700)
	writeExecutable(t, directory, "second", 0o700)
	spec := testCommandSpec
	spec.ExecutableNames = []string{"first", "second"}
	runner := &fakeCommandRunner{}
	ctx := newSteppedCancelContext(4)

	component := (CommandDetector{Runner: runner}).Detect(ctx, spec, []string{directory})

	if component.Status != domain.StatusBroken {
		t.Fatalf("Status = %q, want %q", component.Status, domain.StatusBroken)
	}
	if len(runner.requests) != 0 {
		t.Fatalf("runner calls = %d after enumeration cancellation, want 0", len(runner.requests))
	}
}

func TestCommandDetectorPostEnumerationCancellationDoesNotReturnMissing(t *testing.T) {
	ctx := newSteppedCancelContext(2)
	component := (CommandDetector{Runner: &fakeCommandRunner{}}).Detect(ctx, testCommandSpec, nil)

	if component.Status != domain.StatusBroken {
		t.Fatalf("Status = %q, want %q", component.Status, domain.StatusBroken)
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
	if descriptor.Category != "Core CLI" {
		t.Fatalf("Descriptor().Category = %q, want %q", descriptor.Category, "Core CLI")
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

type swappingCommandRunner struct {
	delegate       platform.CommandRunner
	alias          string
	replacement    string
	wantPinned     bool
	recordedPinned *os.File
}

func (runner *swappingCommandRunner) Run(
	ctx context.Context,
	request platform.CommandRequest,
) (platform.CommandResult, error) {
	runner.recordedPinned = request.PinnedExecutable
	if (request.PinnedExecutable != nil) != runner.wantPinned {
		return platform.CommandResult{ExitCode: -1}, errors.New("unexpected pinned executable mode")
	}
	if err := os.Remove(runner.alias); err != nil {
		return platform.CommandResult{ExitCode: -1}, err
	}
	if err := os.Symlink(runner.replacement, runner.alias); err != nil {
		return platform.CommandResult{ExitCode: -1}, err
	}
	return runner.delegate.Run(ctx, request)
}

type recordingCommandRunner struct {
	delegate platform.CommandRunner
	requests []platform.CommandRequest
}

func (runner *recordingCommandRunner) Run(
	ctx context.Context,
	request platform.CommandRequest,
) (platform.CommandResult, error) {
	runner.requests = append(runner.requests, request)
	return runner.delegate.Run(ctx, request)
}

type duringExecutionSwappingRunner struct {
	delegate    platform.CommandRunner
	alias       string
	replacement string
	marker      string
}

func (runner *duringExecutionSwappingRunner) Run(
	ctx context.Context,
	request platform.CommandRequest,
) (platform.CommandResult, error) {
	if request.PinnedExecutable != nil {
		return platform.CommandResult{ExitCode: -1}, errors.New("script unexpectedly pinned")
	}
	mutationErr := make(chan error, 1)
	go func() {
		deadline := time.Now().Add(2 * time.Second)
		for {
			if _, err := os.Stat(runner.marker); err == nil {
				break
			} else if !errors.Is(err, os.ErrNotExist) {
				mutationErr <- err
				return
			}
			if time.Now().After(deadline) {
				mutationErr <- errors.New("timed out waiting for script marker")
				return
			}
			time.Sleep(time.Millisecond)
		}
		if err := os.Remove(runner.alias); err != nil {
			mutationErr <- err
			return
		}
		mutationErr <- os.Symlink(runner.replacement, runner.alias)
	}()
	result, err := runner.delegate.Run(ctx, request)
	if mutationErr := <-mutationErr; mutationErr != nil {
		return platform.CommandResult{ExitCode: -1}, mutationErr
	}
	return result, err
}

type steppedCancelContext struct {
	context.Context
	cancelAt int32
	calls    atomic.Int32
	done     chan struct{}
	once     sync.Once
}

func newSteppedCancelContext(cancelAt int32) *steppedCancelContext {
	return &steppedCancelContext{
		Context:  context.Background(),
		cancelAt: cancelAt,
		done:     make(chan struct{}),
	}
}

func (ctx *steppedCancelContext) Done() <-chan struct{} { return ctx.done }

func (ctx *steppedCancelContext) Err() error {
	if ctx.calls.Add(1) >= ctx.cancelAt {
		ctx.once.Do(func() { close(ctx.done) })
		return context.Canceled
	}
	select {
	case <-ctx.done:
		return context.Canceled
	default:
		return nil
	}
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

func writeVersionScript(t *testing.T, directory, name, version, marker string) string {
	t.Helper()
	markerCommand := ""
	if marker != "" {
		markerCommand = "printf started > " + marker + "\nsleep 0.1\n"
	}
	path := filepath.Join(directory, name)
	contents := "#!/bin/sh\n" + markerCommand + "printf 'test-cli " + version + "'\n"
	if err := os.WriteFile(path, []byte(contents), 0o700); err != nil {
		t.Fatalf("write version script %q: %v", path, err)
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

func assertBrokenWithoutStaleInstallation(t *testing.T, component domain.Component) {
	t.Helper()
	if component.Status != domain.StatusBroken {
		t.Fatalf("Status = %q, want %q", component.Status, domain.StatusBroken)
	}
	if len(component.Installations) != 1 {
		t.Fatalf("Installations = %#v, want one broken candidate", component.Installations)
	}
	installation := component.Installations[0]
	if installation.Version != "" || installation.ResolvedPath != "" || installation.Managed || installation.Source != "unknown" {
		t.Fatalf("broken installation exposes stale evidence: %#v", installation)
	}
}

func TestCommandDetectorPinnedExecutableHelper(t *testing.T) {
	for index, arg := range os.Args {
		if arg == "--" && index+1 < len(os.Args) && os.Args[index+1] == "detector-version" {
			_, _ = os.Stdout.WriteString("test-cli 1.2.3")
			os.Exit(0)
		}
	}
}
