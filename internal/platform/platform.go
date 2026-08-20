package platform

import (
	"context"
	"os"
	"time"

	"github.com/Oswald-Hao/Osverse/internal/domain"
)

// SystemProbe collects the host details used to determine scan support.
type SystemProbe interface {
	Probe(context.Context) (domain.SystemInfo, error)
}

// PathProbe discovers absolute command-search directories without executing shell profiles.
type PathProbe interface {
	Paths(context.Context) ([]string, error)
}

// CommandRequest describes one direct process invocation.
type CommandRequest struct {
	Path             string
	PinnedExecutable *os.File
	// ReleasePinnedAfterStart transfers ownership of PinnedExecutable to the
	// runner. The runner keeps it open through process creation and closes it
	// on every success or failure path. The zero value preserves caller ownership.
	ReleasePinnedAfterStart bool
	Args                    []string
	Env                     []string
	Timeout                 time.Duration
	OutputLimit             int
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

// LaunchRequest describes one detached user-visible component start. Paths
// come from a fresh backend scan, never from a frontend-supplied filesystem
// value.
type LaunchRequest struct {
	Path                 string
	ExpectedResolvedPath string
	Args                 []string
	Terminal             bool
	// LocalWeb asks the platform starter to allocate a loopback port, append it
	// to the command, verify the HTTP surface became ready, and open it in the
	// user's browser. It is reserved for fixed backend-owned component routes.
	LocalWeb bool
}

// ProcessStarter starts a detached component without involving a shell.
type ProcessStarter interface {
	Start(LaunchRequest) error
}
