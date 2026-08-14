//go:build windows

package windows

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Oswald-Hao/Osverse/internal/domain"
	"github.com/Oswald-Hao/Osverse/internal/platform"
)

func TestExecRunnerCapturesBoundedOutputAndExit(t *testing.T) {
	result, err := NewExecRunner().Run(context.Background(), platform.CommandRequest{
		Path: os.Args[0], Args: []string{"-test.run=TestWindowsRunnerHelper", "--", "output"},
		Env: []string{"OSVERSE_WINDOWS_RUNNER_HELPER=1"}, Timeout: time.Second, OutputLimit: 8,
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
	default:
		os.Exit(3)
	}
}
