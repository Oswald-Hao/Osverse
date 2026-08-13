package linux

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/Oswald-Hao/Osverse/internal/domain"
	"github.com/Oswald-Hao/Osverse/internal/platform"
)

const (
	defaultCommandTimeout = 3 * time.Second
	defaultOutputLimit    = 64 * 1024
	commandWaitDelay      = 250 * time.Millisecond
)

var inheritedEnvironment = []string{"HOME", "PATH", "LANG", "LC_ALL", "TERM"}

type execRunner struct{}

// NewExecRunner returns a runner that executes explicit paths without a shell.
func NewExecRunner() platform.CommandRunner {
	return execRunner{}
}

func (execRunner) Run(ctx context.Context, req platform.CommandRequest) (platform.CommandResult, error) {
	result := platform.CommandResult{ExitCode: -1}
	if req.Path == "" {
		return result, commandFailedError(nil)
	}

	timeout := req.Timeout
	if timeout <= 0 {
		timeout = defaultCommandTimeout
	}
	outputLimit := req.OutputLimit
	if outputLimit <= 0 {
		outputLimit = defaultOutputLimit
	}

	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	stdout := newCappedBuffer(outputLimit)
	stderr := newCappedBuffer(outputLimit)
	cmd := exec.CommandContext(runCtx, req.Path, req.Args...)
	cmd.Env = commandEnvironment(req.Env)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = commandWaitDelay
	var terminationInitiated atomic.Bool
	cmd.Cancel = func() error {
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if err == nil {
			terminationInitiated.Store(true)
			return nil
		}
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}

	err := cmd.Run()
	result.Stdout = stdout.String()
	result.Stderr = stderr.String()
	result.Truncated = stdout.truncated || stderr.truncated
	if cmd.ProcessState != nil {
		result.ExitCode = cmd.ProcessState.ExitCode()
	}

	if err == nil {
		return result, nil
	}
	if terminationInitiated.Load() {
		result.TimedOut = true
		return result, domain.NewPublicError(domain.ErrCommandTimeout, "command timed out", err)
	}
	return result, commandFailedError(err)
}

func commandFailedError(cause error) error {
	return domain.NewPublicError(domain.ErrCommandFailed, "command failed", cause)
}

func commandEnvironment(overrides []string) []string {
	values := make(map[string]string, len(inheritedEnvironment)+len(overrides))
	order := make([]string, 0, len(inheritedEnvironment)+len(overrides))
	for _, name := range inheritedEnvironment {
		if value, present := os.LookupEnv(name); present {
			values[name] = name + "=" + value
			order = append(order, name)
		}
	}
	for _, entry := range overrides {
		name, _, _ := strings.Cut(entry, "=")
		if _, present := values[name]; !present {
			order = append(order, name)
		}
		values[name] = entry
	}

	environment := make([]string, 0, len(order))
	for _, name := range order {
		environment = append(environment, values[name])
	}
	return environment
}

type cappedBuffer struct {
	data      []byte
	limit     int
	truncated bool
}

func newCappedBuffer(limit int) *cappedBuffer {
	return &cappedBuffer{
		data:  make([]byte, 0, limit),
		limit: limit,
	}
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	available := b.limit - len(b.data)
	if available > 0 {
		keep := len(p)
		if keep > available {
			keep = available
		}
		b.data = append(b.data, p[:keep]...)
	}
	if len(p) > available {
		b.truncated = true
	}
	return len(p), nil
}

func (b *cappedBuffer) String() string {
	return string(b.data)
}
