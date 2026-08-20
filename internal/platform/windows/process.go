//go:build windows

package windows

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/Oswald-Hao/Osverse/internal/domain"
	"github.com/Oswald-Hao/Osverse/internal/platform"
	xwindows "golang.org/x/sys/windows"
)

const (
	defaultCommandTimeout = 3 * time.Second
	defaultOutputLimit    = 64 * 1024
)

var inheritedEnvironment = []string{
	"USERPROFILE", "DSH_HOME", "APPDATA", "LOCALAPPDATA", "PATH", "PATHEXT", "SystemRoot",
	"TEMP", "TMP", "ComSpec", "LANG", "TERM",
}

type execRunner struct{}

func NewExecRunner() platform.CommandRunner { return execRunner{} }

func (execRunner) Run(ctx context.Context, req platform.CommandRequest) (platform.CommandResult, error) {
	result := platform.CommandResult{ExitCode: -1}
	if req.ReleasePinnedAfterStart && req.PinnedExecutable == nil {
		return result, commandFailedError(errors.New("transferred pinned executable is missing"))
	}
	var transferredPinned *os.File
	if req.ReleasePinnedAfterStart {
		transferredPinned = req.PinnedExecutable
		defer func() {
			if transferredPinned != nil {
				_ = transferredPinned.Close()
			}
		}()
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if !safeExecutablePath(req.Path) {
		return result, commandFailedError(nil)
	}
	timeout := req.Timeout
	if timeout <= 0 {
		timeout = defaultCommandTimeout
	}
	limit := req.OutputLimit
	if limit <= 0 {
		limit = defaultOutputLimit
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if err := runCtx.Err(); err != nil {
		return classifyContextError(result, err, nil)
	}

	path, args, err := commandInvocation(req.Path, req.Args)
	if err != nil {
		return result, commandFailedError(err)
	}
	stdout, stderr := newCappedBuffer(limit), newCappedBuffer(limit)
	cmd := exec.CommandContext(context.WithoutCancel(runCtx), path, args...)
	cmd.Cancel = nil
	cmd.Env = commandEnvironment(req.Env)
	cmd.Stdout, cmd.Stderr = stdout, stderr
	cmd.Stdin = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: xwindows.CREATE_NEW_PROCESS_GROUP | xwindows.CREATE_NO_WINDOW}

	job, err := newKillOnCloseJob()
	if err != nil {
		return result, commandFailedError(err)
	}
	defer xwindows.CloseHandle(job)
	if err := cmd.Start(); err != nil {
		return result, commandFailedError(err)
	}
	if transferredPinned != nil {
		closeErr := transferredPinned.Close()
		transferredPinned = nil
		if closeErr != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return result, commandFailedError(closeErr)
		}
	}
	process, err := xwindows.OpenProcess(
		xwindows.PROCESS_SET_QUOTA|xwindows.PROCESS_TERMINATE|xwindows.PROCESS_QUERY_LIMITED_INFORMATION,
		false, uint32(cmd.Process.Pid),
	)
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return result, commandFailedError(err)
	}
	assignErr := xwindows.AssignProcessToJobObject(job, process)
	_ = xwindows.CloseHandle(process)
	if assignErr != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return result, commandFailedError(assignErr)
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	var waitErr error
	contextErr := error(nil)
	select {
	case waitErr = <-done:
	case <-runCtx.Done():
		contextErr = runCtx.Err()
		_ = xwindows.TerminateJobObject(job, 1)
		waitErr = <-done
	}
	result.Stdout, result.Stderr = stdout.String(), stderr.String()
	result.Truncated = stdout.truncated || stderr.truncated
	if cmd.ProcessState != nil {
		result.ExitCode = cmd.ProcessState.ExitCode()
	}
	if contextErr != nil {
		return classifyContextError(result, contextErr, waitErr)
	}
	if waitErr != nil {
		return result, commandFailedError(waitErr)
	}
	return result, nil
}

func newKillOnCloseJob() (xwindows.Handle, error) {
	job, err := xwindows.CreateJobObject(nil, nil)
	if err != nil {
		return 0, err
	}
	info := xwindows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = xwindows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := xwindows.SetInformationJobObject(job, xwindows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		_ = xwindows.CloseHandle(job)
		return 0, err
	}
	return job, nil
}

func commandInvocation(path string, args []string) (string, []string, error) {
	extension := strings.ToLower(filepath.Ext(path))
	if extension != ".cmd" && extension != ".bat" {
		return path, append([]string(nil), args...), nil
	}
	for _, value := range append([]string{path}, args...) {
		if strings.ContainsAny(value, "\x00\r\n&|<>^%!") {
			return "", nil, errors.New("unsafe command script argument")
		}
	}
	shell := comspec()
	if !safeExecutablePath(shell) || strings.ToLower(filepath.Ext(shell)) != ".exe" {
		return "", nil, errors.New("Windows command processor unavailable")
	}
	line := quoteCMD(path)
	for _, argument := range args {
		line += " " + quoteCMD(argument)
	}
	return shell, []string{"/d", "/s", "/c", line}, nil
}

func quoteCMD(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `\"`) + `"`
}

func safeExecutablePath(value string) bool {
	return value != "" && filepath.IsAbs(value) && filepath.Clean(value) == value &&
		!strings.ContainsAny(value, "\x00\r\n")
}

func classifyContextError(result platform.CommandResult, contextErr, cause error) (platform.CommandResult, error) {
	if errors.Is(contextErr, context.DeadlineExceeded) {
		result.TimedOut = true
		return result, domain.NewPublicError(domain.ErrCommandTimeout, "command timed out", cause)
	}
	return result, domain.NewPublicError(domain.ErrCommandFailed, "command canceled", cause)
}

func commandFailedError(cause error) error {
	return domain.NewPublicError(domain.ErrCommandFailed, "command failed", cause)
}

func commandEnvironment(overrides []string) []string {
	values := make(map[string]string, len(inheritedEnvironment)+len(overrides))
	order := make([]string, 0, len(inheritedEnvironment)+len(overrides))
	add := func(entry string) {
		name, _, ok := strings.Cut(entry, "=")
		if !ok || name == "" || strings.ContainsAny(name, "\x00\r\n=") {
			return
		}
		key := strings.ToUpper(name)
		if _, exists := values[key]; !exists {
			order = append(order, key)
		}
		values[key] = name + "=" + strings.TrimPrefix(entry, name+"=")
	}
	for _, name := range inheritedEnvironment {
		if value, ok := os.LookupEnv(name); ok {
			add(name + "=" + value)
		}
	}
	for _, entry := range overrides {
		add(entry)
	}
	result := make([]string, 0, len(order))
	for _, key := range order {
		result = append(result, values[key])
	}
	return result
}

type cappedBuffer struct {
	data      []byte
	limit     int
	truncated bool
}

func newCappedBuffer(limit int) *cappedBuffer {
	return &cappedBuffer{data: make([]byte, 0, limit), limit: limit}
}

func (buffer *cappedBuffer) Write(value []byte) (int, error) {
	available := buffer.limit - len(buffer.data)
	if available > 0 {
		keep := len(value)
		if keep > available {
			keep = available
		}
		buffer.data = append(buffer.data, value[:keep]...)
	}
	if len(value) > available {
		buffer.truncated = true
	}
	return len(value), nil
}

func (buffer *cappedBuffer) String() string { return string(buffer.data) }
