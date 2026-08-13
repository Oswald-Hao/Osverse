package linux

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Oswald-Hao/Osverse/internal/domain"
	"github.com/Oswald-Hao/Osverse/internal/platform"
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
	case "stderr-exit":
		_, _ = os.Stderr.WriteString(args[1])
		exitCode := 1
		_, _ = fmt.Sscan(args[2], &exitCode)
		os.Exit(exitCode)
	case "sleep":
		duration, _ := time.ParseDuration(args[1])
		time.Sleep(duration)
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

func helperRequest(mode string, args ...string) platform.CommandRequest {
	requestArgs := []string{"-test.run=^TestHelperProcess$", "--", mode}
	requestArgs = append(requestArgs, args...)
	return platform.CommandRequest{
		Path: os.Args[0],
		Args: requestArgs,
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
