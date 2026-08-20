//go:build windows

package windows

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Oswald-Hao/Osverse/internal/domain"
	"github.com/Oswald-Hao/Osverse/internal/platform"
)

func TestExecRunnerCapturesBoundedOutputAndExit(t *testing.T) {
	result, err := NewExecRunner().Run(context.Background(), platform.CommandRequest{
		Path: os.Args[0], Args: []string{"-test.run=TestWindowsRunnerHelper", "--", "output"},
		Env: []string{"OSVERSE_WINDOWS_RUNNER_HELPER=1"}, Timeout: 10 * time.Second, OutputLimit: 8,
	})
	if err != nil || result.ExitCode != 0 || result.Stdout != "01234567" || !result.Truncated {
		t.Fatalf("Run() = (%#v, %v)", result, err)
	}
}

func TestExecRunnerTimesOutAndKillsOwnedJob(t *testing.T) {
	result, err := NewExecRunner().Run(context.Background(), platform.CommandRequest{
		Path: os.Args[0], Args: []string{"-test.run=TestWindowsRunnerHelper", "--", "sleep"},
		Env: []string{"OSVERSE_WINDOWS_RUNNER_HELPER=1"}, Timeout: 40 * time.Millisecond,
	})
	var public *domain.PublicError
	if !result.TimedOut || !errors.As(err, &public) || public.Code != domain.ErrCommandTimeout {
		t.Fatalf("Run() = (%#v, %v)", result, err)
	}
}

func TestCommandInvocationRejectsScriptMetacharacters(t *testing.T) {
	if _, _, err := commandInvocation(`C:\Users\Alice\tool.cmd`, []string{"ok&whoami"}); err == nil {
		t.Fatal("commandInvocation accepted shell metacharacters")
	}
}

func TestCommandEnvironmentPreservesHarnessHome(t *testing.T) {
	dshHome := `C:\Users\Alice\Harness Profile`
	t.Setenv("DSH_HOME", dshHome)

	found := false
	for _, entry := range commandEnvironment(nil) {
		if strings.EqualFold(entry, "DSH_HOME="+dshHome) {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("command environment dropped DSH_HOME")
	}
}

func TestExecRunnerReleasesTransferredEvidenceAfterProcessStart(t *testing.T) {
	root := t.TempDir()
	lockedPath := filepath.Join(root, "uninstaller-lock.exe")
	if err := os.WriteFile(lockedPath, []byte("pinned identity"), 0o600); err != nil {
		t.Fatal(err)
	}
	evidence, err := OpenExecutableEvidence(lockedPath)
	if err != nil {
		t.Fatal(err)
	}
	pinned := evidence.TakeFile()
	if pinned == nil {
		t.Fatal("TakeFile() returned nil")
	}
	marker := filepath.Join(root, "started")
	type runResult struct {
		result platform.CommandResult
		err    error
	}
	done := make(chan runResult, 1)
	go func() {
		result, runErr := NewExecRunner().Run(context.Background(), platform.CommandRequest{
			Path: os.Args[0], Args: []string{"-test.run=TestWindowsRunnerHelper", "--", "pinned-release"},
			Env:     []string{"OSVERSE_WINDOWS_RUNNER_HELPER=1", "OSVERSE_WINDOWS_RUNNER_MARKER=" + marker},
			Timeout: 10 * time.Second, PinnedExecutable: pinned, ReleasePinnedAfterStart: true,
		})
		done <- runResult{result: result, err: runErr}
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("helper did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := os.Rename(lockedPath, lockedPath+".moved"); err != nil {
		t.Fatalf("transferred evidence remained locked after process start: %v", err)
	}
	got := <-done
	if got.err != nil || got.result.ExitCode != 0 {
		t.Fatalf("Run() = (%#v, %v)", got.result, got.err)
	}
}

func TestExecRunnerReleasesTransferredEvidenceWhenStartFails(t *testing.T) {
	root := t.TempDir()
	lockedPath := filepath.Join(root, "failed-start-lock.exe")
	if err := os.WriteFile(lockedPath, []byte("pinned identity"), 0o600); err != nil {
		t.Fatal(err)
	}
	evidence, err := OpenExecutableEvidence(lockedPath)
	if err != nil {
		t.Fatal(err)
	}
	_, runErr := NewExecRunner().Run(context.Background(), platform.CommandRequest{
		Path: "relative.exe", PinnedExecutable: evidence.TakeFile(), ReleasePinnedAfterStart: true,
	})
	if runErr == nil {
		t.Fatal("Run() unexpectedly succeeded")
	}
	if err := os.Rename(lockedPath, lockedPath+".moved"); err != nil {
		t.Fatalf("transferred evidence remained locked after start failure: %v", err)
	}
}

func TestWindowsRunnerHelper(t *testing.T) {
	if os.Getenv("OSVERSE_WINDOWS_RUNNER_HELPER") != "1" {
		return
	}
	mode := ""
	for index, value := range os.Args {
		if value == "--" && index+1 < len(os.Args) {
			mode = os.Args[index+1]
		}
	}
	switch mode {
	case "output":
		fmt.Fprint(os.Stdout, strings.Repeat("0123456789", 4))
		os.Exit(0)
	case "sleep":
		time.Sleep(10 * time.Second)
		os.Exit(0)
	case "pinned-release":
		if err := os.WriteFile(os.Getenv("OSVERSE_WINDOWS_RUNNER_MARKER"), []byte("started"), 0o600); err != nil {
			os.Exit(4)
		}
		time.Sleep(300 * time.Millisecond)
		os.Exit(0)
	default:
		os.Exit(3)
	}
}
