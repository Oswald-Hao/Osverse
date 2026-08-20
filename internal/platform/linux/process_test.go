//go:build linux

package linux

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Oswald-Hao/Osverse/internal/domain"
	"github.com/Oswald-Hao/Osverse/internal/platform"
	"golang.org/x/sys/unix"
)

func TestExecRunnerSuccess(t *testing.T) {
	result, err := NewExecRunner().Run(context.Background(), helperRequest("stdout", "hello"))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", result.ExitCode)
	}
	if result.Stdout != "hello" {
		t.Errorf("Stdout = %q, want %q", result.Stdout, "hello")
	}
	if result.Stderr != "" {
		t.Errorf("Stderr = %q, want empty", result.Stderr)
	}
	if result.TimedOut || result.Truncated {
		t.Errorf("TimedOut = %t, Truncated = %t; want both false", result.TimedOut, result.Truncated)
	}
}

func TestExecRunnerPinnedExecutableIgnoresReplacedPath(t *testing.T) {
	pinned, err := os.Open(os.Args[0])
	if err != nil {
		t.Fatalf("open test executable: %v", err)
	}
	defer pinned.Close()

	replacement := filepath.Join(t.TempDir(), "replaced-command")
	if err := os.WriteFile(replacement, []byte("#!/bin/sh\nprintf replacement"), 0o700); err != nil {
		t.Fatalf("write replacement executable: %v", err)
	}
	req := helperRequest("argv0")
	req.Path = replacement
	req.PinnedExecutable = pinned

	result, err := NewExecRunner().Run(context.Background(), req)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Stdout != replacement {
		t.Fatalf("Stdout = %q, want requested invocation identity %q", result.Stdout, replacement)
	}
}

func TestExecRunnerClosesTransferredPinnedExecutable(t *testing.T) {
	pinned, err := os.Open(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	req := helperRequest("argv0")
	req.PinnedExecutable = pinned
	req.ReleasePinnedAfterStart = true
	if _, err := NewExecRunner().Run(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if _, err := pinned.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("transferred pinned executable remains open: %v", err)
	}
}

func TestExecRunnerDirectEnvShellScriptPreservesPathSiblingAndStdinEOF(t *testing.T) {
	directory := t.TempDir()
	commandPath := filepath.Join(directory, "command")
	resourcePath := filepath.Join(directory, "version")
	if err := os.WriteFile(resourcePath, []byte("script 1.2.3"), 0o600); err != nil {
		t.Fatalf("write sibling resource: %v", err)
	}
	contents := "#!/usr/bin/env sh\nresource=${0%/*}/version\nIFS= read -r version < \"$resource\"\nif IFS= read -r unexpected; then exit 9; fi\nprintf '%s %s %s' \"$0\" \"$version\" \"$1\"\n"
	if err := os.WriteFile(commandPath, []byte(contents), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	result, err := NewExecRunner().Run(context.Background(), platform.CommandRequest{
		Path: commandPath,
		Args: []string{"--version"},
	})
	if err != nil {
		t.Fatalf("Run() error = %v; result = %#v", err, result)
	}
	want := commandPath + " script 1.2.3 --version"
	if result.Stdout != want {
		t.Fatalf("Stdout = %q, want direct path, sibling resource, stdin EOF, and fixed argument %q", result.Stdout, want)
	}
}

func TestExecRunnerDirectNodeScriptPreservesOwnPathAndSibling(t *testing.T) {
	const nodePath = "/usr/bin/node"
	if _, err := os.Stat(nodePath); err != nil {
		t.Skipf("Node interpreter unavailable at %s: %v", nodePath, err)
	}
	directory := t.TempDir()
	commandPath := filepath.Join(directory, "command")
	resourcePath := filepath.Join(directory, "version")
	if err := os.WriteFile(resourcePath, []byte("node-script 2.3.4"), 0o600); err != nil {
		t.Fatalf("write sibling resource: %v", err)
	}
	contents := `#!/usr/bin/node
const fs = require("fs");
const path = require("path");
process.stdout.write(fs.readFileSync(path.join(__dirname, "version"), "utf8") + " " + process.argv[2]);
`
	if err := os.WriteFile(commandPath, []byte(contents), 0o700); err != nil {
		t.Fatalf("write pinned Node script: %v", err)
	}
	result, err := NewExecRunner().Run(context.Background(), platform.CommandRequest{
		Path: commandPath,
		Args: []string{"--version"},
	})
	if err != nil {
		t.Fatalf("Run() error = %v; result = %#v", err, result)
	}
	if result.Stdout != "node-script 2.3.4 --version" {
		t.Fatalf("Stdout = %q, want pinned Node sibling resource and fixed argument", result.Stdout)
	}
}

func TestExecRunnerNonZeroExitReturnsBoundedRedactedError(t *testing.T) {
	req := helperRequest("stderr-exit", "credential-in-output", "17")
	req.OutputLimit = 8
	result, err := NewExecRunner().Run(context.Background(), req)

	if result.ExitCode != 17 {
		t.Errorf("ExitCode = %d, want 17", result.ExitCode)
	}
	if result.Stderr != "credenti" {
		t.Errorf("Stderr = %q, want first 8 bytes", result.Stderr)
	}
	if len(result.Stdout) > 8 || len(result.Stderr) > 8 {
		t.Fatalf("output exceeded limit: stdout=%d stderr=%d", len(result.Stdout), len(result.Stderr))
	}
	if !result.Truncated {
		t.Error("Truncated = false, want true")
	}
	assertPublicError(t, err, domain.ErrCommandFailed, "credential-in-output")
}

func TestExecRunnerTimeoutKillsProcess(t *testing.T) {
	req := helperRequest("sleep", "10s")
	req.Timeout = 100 * time.Millisecond
	req.OutputLimit = 4
	result, err := NewExecRunner().Run(context.Background(), req)

	if !result.TimedOut {
		t.Error("TimedOut = false, want true")
	}
	if result.ExitCode == 0 {
		t.Error("ExitCode = 0 for timed-out process")
	}
	if len(result.Stdout) > 4 || len(result.Stderr) > 4 {
		t.Fatalf("output exceeded limit: stdout=%d stderr=%d", len(result.Stdout), len(result.Stderr))
	}
	assertPublicError(t, err, domain.ErrCommandTimeout, "10s")
}

func TestExecRunnerCallerCancellationKillsDescendantHoldingPipes(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "descendant.pid")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := helperRequest("spawn-descendant", marker)
	req.Timeout = 30 * time.Second

	type outcome struct {
		result platform.CommandResult
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, err := NewExecRunner().Run(ctx, req)
		done <- outcome{result: result, err: err}
	}()

	descendantPID := waitForPIDFile(t, marker)
	descendantPIDFD, err := unix.PidfdOpen(descendantPID, 0)
	if err != nil {
		t.Fatalf("open descendant pidfd: %v", err)
	}
	defer unix.Close(descendantPIDFD)
	descendantExited := false
	t.Cleanup(func() {
		if !descendantExited {
			_ = unix.PidfdSendSignal(descendantPIDFD, unix.SIGKILL, nil, 0)
		}
	})
	cancel()

	select {
	case got := <-done:
		if got.result.TimedOut {
			t.Error("TimedOut = true for caller cancellation")
		}
		assertPublicErrorMessage(t, got.err, domain.ErrCommandFailed, "command canceled", marker)
		waitForPIDFDReady(t, descendantPIDFD)
		descendantExited = true
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after caller cancellation; descendant retained output pipes")
	}
}

func TestClassifyCommandCompletionIndependentNonzeroExitWinsDeadline(t *testing.T) {
	// Reap a real nonzero child first so its completed state is independent of
	// the deadline supplied to the classifier below.
	cmd := exec.Command(os.Args[0], helperProcessArgs(os.Args, "stderr-exit", "independent-exit", "17")...)
	waitErr := cmd.Run()
	if waitErr == nil {
		t.Fatal("helper wait error = nil, want nonzero exit")
	}
	if cmd.ProcessState == nil || cmd.ProcessState.ExitCode() != 17 {
		t.Fatalf("helper exit state = %v, want exit code 17", cmd.ProcessState)
	}

	ctx, cancel := context.WithDeadline(context.Background(), time.Unix(1, 0))
	defer cancel()
	if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		t.Fatalf("context error = %v, want deadline exceeded", ctx.Err())
	}

	// Both facts are synchronously established before the decision boundary;
	// no goroutine or buffered result can let classification happen earlier.
	result, err := classifyCommandCompletion(commandCompletion{
		result:           platform.CommandResult{ExitCode: cmd.ProcessState.ExitCode()},
		processState:     cmd.ProcessState,
		contextTriggered: true,
		contextErr:       ctx.Err(),
		waitErr:          waitErr,
	})

	if result.ExitCode != 17 {
		t.Errorf("ExitCode = %d, want 17", result.ExitCode)
	}
	if result.TimedOut {
		t.Error("TimedOut = true for independently completed nonzero exit")
	}
	assertPublicError(t, err, domain.ErrCommandFailed, "independent-exit")
}

func TestExecRunnerExpiredContextWithoutStartedProcessIsCommandFailure(t *testing.T) {
	ctx, cancel := context.WithDeadline(context.Background(), time.Unix(1, 0))
	defer cancel()

	result, err := NewExecRunner().Run(ctx, helperRequest("stderr-exit", "not-started", "17"))

	if result.TimedOut {
		t.Error("TimedOut = true although cancellation did not terminate a started process")
	}
	assertPublicError(t, err, domain.ErrCommandFailed, "not-started")
}

func TestExecRunnerTruncatesStdoutAndStderrSeparately(t *testing.T) {
	req := helperRequest("both", "abcdefghijklmnop", "QRSTUVWXYZabcdef")
	req.OutputLimit = 7
	result, err := NewExecRunner().Run(context.Background(), req)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if result.Stdout != "abcdefg" {
		t.Errorf("Stdout = %q, want %q", result.Stdout, "abcdefg")
	}
	if result.Stderr != "QRSTUVW" {
		t.Errorf("Stderr = %q, want %q", result.Stderr, "QRSTUVW")
	}
	if !result.Truncated {
		t.Error("Truncated = false, want true")
	}
}

func TestExecRunnerUsesAllowlistedEnvironmentAndOverrides(t *testing.T) {
	t.Setenv("HOME", "/inherited/home")
	t.Setenv("PATH", "/inherited/path")
	t.Setenv("LANG", "inherited-lang")
	t.Setenv("LC_ALL", "inherited-lc-all")
	t.Setenv("TERM", "inherited-term")
	t.Setenv("OSVERSE_UNRELATED_SECRET", "must-not-be-inherited")

	req := helperRequest("environment")
	req.Env = []string{"HOME=/request/home", "EXTRA=request-value"}
	result, err := NewExecRunner().Run(context.Background(), req)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	var got []string
	if err := json.Unmarshal([]byte(result.Stdout), &got); err != nil {
		t.Fatalf("decode helper environment %q: %v", result.Stdout, err)
	}
	sort.Strings(got)
	want := []string{
		"EXTRA=request-value",
		"HOME=/request/home",
		"LANG=inherited-lang",
		"LC_ALL=inherited-lc-all",
		"PATH=/inherited/path",
		"TERM=inherited-term",
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("environment = %q, want %q", got, want)
	}
}

func TestExecRunnerRejectsEmptyPathWithoutLeakingEnvironment(t *testing.T) {
	result, err := NewExecRunner().Run(context.Background(), platform.CommandRequest{
		Env: []string{"TOKEN=environment-secret"},
	})
	if result.ExitCode == 0 {
		t.Error("ExitCode = 0 for rejected empty path")
	}
	assertPublicError(t, err, domain.ErrCommandFailed, "environment-secret")
}

func TestHelperProcess(t *testing.T) {
	separator := -1
	for i, arg := range os.Args {
		if arg == "--" {
			separator = i
			break
		}
	}
	if separator < 0 || separator+1 >= len(os.Args) {
		return
	}

	args := os.Args[separator+1:]
	switch args[0] {
	case "stdout":
		_, _ = os.Stdout.WriteString(args[1])
	case "argv0":
		_, _ = os.Stdout.WriteString(os.Args[0])
	case "stderr-exit":
		_, _ = os.Stderr.WriteString(args[1])
		exitCode := 1
		_, _ = fmt.Sscan(args[2], &exitCode)
		os.Exit(exitCode)
	case "sleep":
		duration, _ := time.ParseDuration(args[1])
		time.Sleep(duration)
	case "spawn-descendant":
		cmd := startPipeHoldingDescendant(args[1])
		_ = cmd
		time.Sleep(30 * time.Second)
	case "hold-pipes":
		time.Sleep(30 * time.Second)
	case "both":
		_, _ = os.Stdout.WriteString(args[1])
		_, _ = os.Stderr.WriteString(args[2])
	case "environment":
		environment := os.Environ()
		sort.Strings(environment)
		_ = json.NewEncoder(os.Stdout).Encode(environment)
	default:
		os.Exit(2)
	}
	os.Exit(0)
}

func startPipeHoldingDescendant(marker string) *exec.Cmd {
	cmd := exec.Command(os.Args[0], helperProcessArgs(os.Args, "hold-pipes")...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		os.Exit(3)
	}
	if err := os.WriteFile(marker, []byte(strconv.Itoa(cmd.Process.Pid)), 0o600); err != nil {
		_ = cmd.Process.Kill()
		os.Exit(4)
	}
	return cmd
}

func waitForPIDFile(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		data, err := os.ReadFile(path)
		if err == nil {
			pid, err := strconv.Atoi(string(data))
			if err != nil {
				t.Fatalf("parse descendant PID %q: %v", data, err)
			}
			return pid
		}
		if !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("read descendant PID marker: %v", err)
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for descendant PID marker")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func waitForPIDFDReady(t *testing.T, pidfd int) {
	t.Helper()
	fds := []unix.PollFd{{Fd: int32(pidfd), Events: unix.POLLIN}}
	n, err := unix.Poll(fds, 5000)
	if err != nil {
		t.Fatalf("poll pidfd: %v", err)
	}
	if n != 1 || fds[0].Revents&unix.POLLIN == 0 {
		t.Fatalf("pidfd did not report process exit: n=%d revents=%#x", n, fds[0].Revents)
	}
}

func helperRequest(mode string, args ...string) platform.CommandRequest {
	return platform.CommandRequest{
		Path: os.Args[0],
		Args: helperProcessArgs(os.Args, mode, args...),
		Env:  helperCoverageEnvironment(os.Environ()),
	}
}

func helperProcessArgs(processArgs []string, mode string, args ...string) []string {
	requestArgs := []string{"-test.run=^TestHelperProcess$"}
	for _, arg := range processArgs[1:] {
		if strings.HasPrefix(arg, "-test.gocoverdir=") {
			requestArgs = append(requestArgs, arg)
			break
		}
	}
	requestArgs = append(requestArgs, "--", mode)
	return append(requestArgs, args...)
}

func helperCoverageEnvironment(processEnvironment []string) []string {
	for _, entry := range processEnvironment {
		name, value, found := strings.Cut(entry, "=")
		if found && name == "GOCOVERDIR" && value != "" {
			return []string{entry}
		}
	}
	return nil
}

func TestHelperProcessArgsPropagateOnlyCoverageDirectory(t *testing.T) {
	t.Parallel()

	got := helperProcessArgs(
		[]string{"linux.test", "-test.v", "-test.gocoverdir=/tmp/coverage", "-test.timeout=1s"},
		"stdout",
		"hello",
	)
	want := []string{
		"-test.run=^TestHelperProcess$",
		"-test.gocoverdir=/tmp/coverage",
		"--",
		"stdout",
		"hello",
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("helper args = %q, want %q", got, want)
	}

	gotEnvironment := helperCoverageEnvironment([]string{
		"TOKEN=must-not-leak",
		"GOCOVERDIR=/tmp/coverage",
		"PATH=/must-not-inherit",
	})
	wantEnvironment := []string{"GOCOVERDIR=/tmp/coverage"}
	if fmt.Sprint(gotEnvironment) != fmt.Sprint(wantEnvironment) {
		t.Fatalf("helper environment = %q, want %q", gotEnvironment, wantEnvironment)
	}
}

func assertPublicError(t *testing.T, err error, wantCode domain.ErrorCode, forbidden string) {
	t.Helper()
	if err == nil {
		t.Fatal("Run() error = nil")
	}
	var public *domain.PublicError
	if !errors.As(err, &public) {
		t.Fatalf("Run() error type = %T, want *domain.PublicError", err)
	}
	if public.Code != wantCode {
		t.Errorf("error code = %q, want %q", public.Code, wantCode)
	}
	if strings.Contains(err.Error(), forbidden) {
		t.Errorf("public error leaked %q: %q", forbidden, err)
	}
}

func assertPublicErrorMessage(t *testing.T, err error, wantCode domain.ErrorCode, wantMessage, forbidden string) {
	t.Helper()
	assertPublicError(t, err, wantCode, forbidden)
	var public *domain.PublicError
	if errors.As(err, &public) && public.Message != wantMessage {
		t.Errorf("error message = %q, want %q", public.Message, wantMessage)
	}
}
