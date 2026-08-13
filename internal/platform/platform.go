package platform

import (
	"context"
	"time"

	"github.com/Oswald-Hao/Osverse/internal/domain"
)

// SystemProbe collects the host details used to determine scan support.
type SystemProbe interface {
	Probe(context.Context) (domain.SystemInfo, error)
}

// CommandRequest describes one direct process invocation.
type CommandRequest struct {
	Path        string
	Args        []string
	Env         []string
	Timeout     time.Duration
	OutputLimit int
}

// CommandResult contains bounded process output and termination details.
type CommandResult struct {
	ExitCode  int
	Stdout    string
	Stderr    string
	TimedOut  bool
	Truncated bool
}

// CommandRunner runs a process without involving a command shell.
type CommandRunner interface {
	Run(context.Context, CommandRequest) (CommandResult, error)
}
