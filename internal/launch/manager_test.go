package launch

import (
	"context"
	"errors"
	"testing"

	"github.com/Oswald-Hao/Osverse/internal/domain"
	"github.com/Oswald-Hao/Osverse/internal/platform"
)

func TestManagerLaunchesDetectedCLIInTerminalAndExternalDesktopDirectly(t *testing.T) {
	starter := &fakeStarter{}
	manager := NewManager(starter, nil)

	cli := installedComponent("claude-code", "Core CLI", "/home/test/.local/bin/claude", "/home/test/.local/share/claude/versions/2.1.232", false)
	if err := manager.Launch(context.Background(), cli); err != nil {
		t.Fatalf("Launch(CLI) error = %v", err)
	}
	desktop := installedComponent("cockpit-tools", "Management Tools", "/opt/cockpit/cockpit-tools", "/opt/cockpit/cockpit-tools", false)
	if err := manager.Launch(context.Background(), desktop); err != nil {
		t.Fatalf("Launch(desktop) error = %v", err)
	}

	if len(starter.requests) != 2 {
		t.Fatalf("starter requests = %d, want 2", len(starter.requests))
	}
	if got := starter.requests[0]; got.Path != cli.Installations[0].Path || got.ExpectedResolvedPath != cli.Installations[0].ResolvedPath || !got.Terminal {
		t.Fatalf("CLI request = %#v", got)
	}
	if got := starter.requests[1]; got.Path != desktop.Installations[0].Path || got.ExpectedResolvedPath != desktop.Installations[0].ResolvedPath || got.Terminal {
		t.Fatalf("desktop request = %#v", got)
	}
}

func TestManagerKeepsVerifiedOsverseDesktopLaunch(t *testing.T) {
	starter := &fakeStarter{}
	managed := &fakeManagedLauncher{}
	manager := NewManager(starter, managed)
	component := installedComponent("cc-switch", "Management Tools", "/home/test/.local/bin/cc-switch", "/home/test/.local/share/osverse/apps/cc-switch/3.19.2/application.AppImage", true)

	if err := manager.Launch(context.Background(), component); err != nil {
		t.Fatal(err)
	}
	if managed.componentID != "cc-switch" || len(starter.requests) != 0 {
		t.Fatalf("managed ID = %q; direct starts = %d", managed.componentID, len(starter.requests))
	}
}

func TestManagerRejectsFrontendControlledOrAmbiguousLaunches(t *testing.T) {
	valid := installedComponent("codex-cli", "Core CLI", "/usr/bin/codex", "/usr/lib/codex", false)
	tests := []struct {
		name      string
		component domain.Component
	}{
		{name: "unknown ID", component: installedComponent("arbitrary", "Core CLI", "/tmp/payload", "/tmp/payload", false)},
		{name: "missing", component: domain.Component{ID: "codex-cli", Category: "Core CLI", Status: domain.StatusMissing}},
		{name: "no installation", component: domain.Component{ID: "codex-cli", Category: "Core CLI", Status: domain.StatusInstalled}},
		{name: "multiple installations", component: func() domain.Component {
			value := valid
			value.Installations = append(value.Installations, domain.Installation{Path: "/other/codex", ResolvedPath: "/other/codex"})
			return value
		}()},
		{name: "relative path", component: installedComponent("codex-cli", "Core CLI", "codex", "/usr/bin/codex", false)},
		{name: "category mismatch", component: installedComponent("codex-cli", "Management Tools", "/usr/bin/codex", "/usr/bin/codex", false)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			starter := &fakeStarter{}
			if err := NewManager(starter, nil).Launch(context.Background(), test.component); !errors.Is(err, ErrLaunchUnavailable) {
				t.Fatalf("Launch() error = %v, want ErrLaunchUnavailable", err)
			}
			if len(starter.requests) != 0 {
				t.Fatal("rejected component reached process starter")
			}
		})
	}
}

func TestManagerHonorsCancellationAndRedactsStarterFailures(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	component := installedComponent("opencode-cli", "Core CLI", "/usr/bin/opencode", "/usr/bin/opencode", false)
	if err := NewManager(&fakeStarter{}, nil).Launch(ctx, component); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Launch() error = %v", err)
	}

	starter := &fakeStarter{err: errors.New("private process diagnostic")}
	if err := NewManager(starter, nil).Launch(context.Background(), component); !errors.Is(err, ErrLaunchFailed) || err.Error() != ErrLaunchFailed.Error() {
		t.Fatalf("starter error = %v, want redacted ErrLaunchFailed", err)
	}
}

func installedComponent(id, category, path, resolved string, managed bool) domain.Component {
	return domain.Component{
		ID: id, Name: id, Category: category, Status: domain.StatusInstalled,
		Installations: []domain.Installation{{Path: path, ResolvedPath: resolved, Managed: managed}},
	}
}

type fakeStarter struct {
	requests []platform.LaunchRequest
	err      error
}

func (starter *fakeStarter) Start(request platform.LaunchRequest) error {
	starter.requests = append(starter.requests, request)
	return starter.err
}

type fakeManagedLauncher struct{ componentID string }

func (launcher *fakeManagedLauncher) Launch(componentID string) error {
	launcher.componentID = componentID
	return nil
}
