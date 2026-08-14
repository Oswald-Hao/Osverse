package linux

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/Oswald-Hao/Osverse/internal/domain"
	"github.com/Oswald-Hao/Osverse/internal/platform"
	"golang.org/x/sys/unix"
)

const (
	defaultCommandTimeout = 3 * time.Second
	defaultOutputLimit    = 64 * 1024
	commandWaitDelay      = 250 * time.Millisecond
	pinnedExecutablePath  = "/proc/self/fd/3"
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
	if err := runCtx.Err(); err != nil {
		if errors.Is(err, context.Canceled) {
			return result, commandCanceledError(err)
		}
		return result, commandFailedError(err)
	}

	stdout := newCappedBuffer(outputLimit)
	stderr := newCappedBuffer(outputLimit)
	// Coordinate cancellation here instead of allowing os/exec to reap the
	// leader concurrently. Until cmd.Wait starts, its PID (and therefore the
	// process-group ID we created) cannot be recycled for an unrelated group.
	commandPath := req.Path
	if req.PinnedExecutable != nil {
		commandPath = pinnedExecutablePath
	}
	cmd := exec.CommandContext(context.WithoutCancel(runCtx), commandPath, req.Args...)
	if req.PinnedExecutable != nil {
		cmd.Args[0] = req.Path
		// ExtraFiles maps the already-open executable to child FD 3. Executing
		// through procfs binds execve to that file object even if req.Path moves.
		cmd.ExtraFiles = []*os.File{req.PinnedExecutable}
	}
	cmd.Env = commandEnvironment(req.Env)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	pidfd := -1
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, PidFD: &pidfd}
	cmd.WaitDelay = commandWaitDelay
	cmd.Cancel = nil

	if err := cmd.Start(); err != nil {
		return result, commandFailedError(err)
	}
	if pidfd < 0 {
		terminationErr := terminateOwnedProcessGroup(cmd.Process)
		waitErr := cmd.Wait()
		setCommandResult(&result, cmd, stdout, stderr)
		return result, commandFailedError(errors.Join(errors.New("pidfd unavailable"), terminationErr, waitErr))
	}

	exitReady := make(chan error, 1)
	go func() {
		// Pidfd readiness observes exit without reaping, preserving the PID/PGID
		// ownership invariant until the cancellation decision is complete.
		exitReady <- waitForPIDFD(pidfd)
	}()

	contextTriggered := false
	exitObserved := false
	var observationErr error
	select {
	case observationErr = <-exitReady:
		exitObserved = true
	case <-runCtx.Done():
		contextTriggered = true
		select {
		case observationErr = <-exitReady:
			exitObserved = true
		default:
		}
	}

	terminationErr := terminateOwnedProcessGroup(cmd.Process)
	err := cmd.Wait()
	if !exitObserved {
		observationErr = <-exitReady
	}
	_ = unix.Close(pidfd)
	setCommandResult(&result, cmd, stdout, stderr)

	return classifyCommandCompletion(commandCompletion{
		result:           result,
		processState:     cmd.ProcessState,
		contextTriggered: contextTriggered,
		contextErr:       runCtx.Err(),
		observationErr:   observationErr,
		terminationErr:   terminationErr,
		waitErr:          err,
	})
}

type commandCompletion struct {
	result           platform.CommandResult
	processState     *os.ProcessState
	contextTriggered bool
	contextErr       error
	observationErr   error
	terminationErr   error
	waitErr          error
}

func classifyCommandCompletion(completion commandCompletion) (platform.CommandResult, error) {
	result := completion.result
	if completion.observationErr != nil {
		return result, commandFailedError(errors.Join(completion.observationErr, completion.terminationErr, completion.waitErr))
	}
	if completion.waitErr == nil {
		return result, nil
	}
	if completion.contextTriggered && processWasKilled(completion.processState) {
		if errors.Is(completion.contextErr, context.DeadlineExceeded) {
			result.TimedOut = true
			return result, domain.NewPublicError(domain.ErrCommandTimeout, "command timed out", completion.waitErr)
		}
		if errors.Is(completion.contextErr, context.Canceled) {
			return result, commandCanceledError(completion.waitErr)
		}
	}
	return result, commandFailedError(errors.Join(completion.terminationErr, completion.waitErr))
}

func setCommandResult(result *platform.CommandResult, cmd *exec.Cmd, stdout, stderr *cappedBuffer) {
	result.Stdout = stdout.String()
	result.Stderr = stderr.String()
	result.Truncated = stdout.truncated || stderr.truncated
	if cmd.ProcessState != nil {
		result.ExitCode = cmd.ProcessState.ExitCode()
	}
}

func waitForPIDFD(pidfd int) error {
	fds := []unix.PollFd{{Fd: int32(pidfd), Events: unix.POLLIN}}
	for {
		_, err := unix.Poll(fds, -1)
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		return err
	}
}

func terminateOwnedProcessGroup(process *os.Process) error {
	// The caller must not begin Process.Wait or Cmd.Wait before this call. That
	// keeps process.Pid allocated and prevents the negative PID from naming a
	// recycled, unrelated process group.
	err := syscall.Kill(-process.Pid, syscall.SIGKILL)
	if err == nil {
		return nil
	}
	leaderErr := process.Kill()
	if errors.Is(leaderErr, os.ErrProcessDone) {
		leaderErr = nil
	}
	return errors.Join(err, leaderErr)
}

func processWasKilled(state *os.ProcessState) bool {
	if state == nil {
		return false
	}
	status, ok := state.Sys().(syscall.WaitStatus)
	return ok && status.Signaled() && status.Signal() == syscall.SIGKILL
}

func commandFailedError(cause error) error {
	return domain.NewPublicError(domain.ErrCommandFailed, "command failed", cause)
}

func commandCanceledError(cause error) error {
	return domain.NewPublicError(domain.ErrCommandFailed, "command canceled", cause)
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
