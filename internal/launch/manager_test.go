package launch

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"github.com/Oswald-Hao/Osverse/internal/domain"
	"github.com/Oswald-Hao/Osverse/internal/platform"
)

func TestManagerLaunchesDetectedCLIInTerminalAndExternalDesktopDirectly(t *testing.T) {
	starter := &fakeStarter{}
	manager := NewManager(starter, nil)

	cli := installedComponent("claude-code", "Core CLI", launchTestPath("home", "test", ".local", "bin", "claude"), launchTestPath("home", "test", ".local", "share", "claude", "versions", "2.1.232"), false)
	if err := manager.Launch(context.Background(), cli, cli.Installations[0].Path); err != nil {
		t.Fatalf("Launch(CLI) error = %v", err)
	}
	desktopPath := launchTestPath("opt", "cockpit", "cockpit-tools")
	desktop := installedComponent("cockpit-tools", "Management Tools", desktopPath, desktopPath, false)
	if err := manager.Launch(context.Background(), desktop, desktop.Installations[0].Path); err != nil {
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

func TestManagerLaunchesDeepSeekHarnessWebProfile(t *testing.T) {
	starter := &fakeStarter{}
	manager := NewManager(starter, nil)
	component := installedComponent("deepseek-harness", "Core CLI", launchTestPath("home", "test", ".local", "bin", "dsh"), launchTestPath("home", "test", ".local", "share", "osverse", "tools", "deepseek-harness", "current", "bin", "dsh"), true)

	if err := manager.Launch(context.Background(), component, component.Installations[0].Path); err != nil {
		t.Fatal(err)
	}
	if len(starter.requests) != 1 || !reflect.DeepEqual(starter.requests[0].Args, []string{"web"}) ||
		!starter.requests[0].Terminal || !starter.requests[0].LocalWeb {
		t.Fatalf("DeepSeek Harness launch request = %#v", starter.requests)
	}
}

func TestManagerLaunchesDetectedGitHubCopilotInTerminal(t *testing.T) {
	starter := &fakeStarter{}
	manager := NewManager(starter, nil)
	copilotPath := launchTestPath("home", "test", ".local", "bin", "copilot")
	component := installedComponent("github-copilot-cli", "Core CLI", copilotPath, copilotPath, false)

	if err := manager.Launch(context.Background(), component, component.Installations[0].Path); err != nil {
		t.Fatal(err)
	}
	if len(starter.requests) != 1 || !starter.requests[0].Terminal || len(starter.requests[0].Args) != 0 {
		t.Fatalf("GitHub Copilot launch request = %#v", starter.requests)
	}
}

func TestManagerLaunchesDetectedKimiCodeInTerminal(t *testing.T) {
	starter := &fakeStarter{}
	manager := NewManager(starter, nil)
	kimiPath := launchTestPath("home", "test", ".local", "bin", "kimi")
	component := installedComponent("kimi-code", "Core CLI", kimiPath, kimiPath, true)

	if err := manager.Launch(context.Background(), component, component.Installations[0].Path); err != nil {
		t.Fatal(err)
	}
	if len(starter.requests) != 1 || !starter.requests[0].Terminal || len(starter.requests[0].Args) != 0 {
		t.Fatalf("Kimi Code launch request = %#v", starter.requests)
	}
}

func TestManagerKeepsVerifiedOsverseDesktopLaunch(t *testing.T) {
	starter := &fakeStarter{}
	managed := &fakeManagedLauncher{}
	manager := NewManager(starter, managed)
	component := installedComponent("cc-switch", "Management Tools", launchTestPath("home", "test", ".local", "bin", "cc-switch"), launchTestPath("home", "test", ".local", "share", "osverse", "apps", "cc-switch", "3.19.2", "application.AppImage"), true)

	if err := manager.Launch(context.Background(), component, component.Installations[0].Path); err != nil {
		t.Fatal(err)
	}
	if managed.componentID != "cc-switch" || len(starter.requests) != 0 {
		t.Fatalf("managed ID = %q; direct starts = %d", managed.componentID, len(starter.requests))
	}
}

func TestManagerRejectsFrontendControlledOrAmbiguousLaunches(t *testing.T) {
	tests := []struct {
		name      string
		component domain.Component
	}{
		{name: "unknown ID", component: installedComponent("arbitrary", "Core CLI", "/tmp/payload", "/tmp/payload", false)},
		{name: "missing", component: domain.Component{ID: "codex-cli", Category: "Core CLI", Status: domain.StatusMissing}},
		{name: "no installation", component: domain.Component{ID: "codex-cli", Category: "Core CLI", Status: domain.StatusInstalled}},
		{name: "relative path", component: installedComponent("codex-cli", "Core CLI", "codex", "/usr/bin/codex", false)},
		{name: "category mismatch", component: installedComponent("codex-cli", "Management Tools", "/usr/bin/codex", "/usr/bin/codex", false)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			starter := &fakeStarter{}
			if err := NewManager(starter, nil).Launch(context.Background(), test.component, ""); !errors.Is(err, ErrLaunchUnavailable) {
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
	opencodePath := launchTestPath("usr", "bin", "opencode")
	component := installedComponent("opencode-cli", "Core CLI", opencodePath, opencodePath, false)
	if err := NewManager(&fakeStarter{}, nil).Launch(ctx, component, component.Installations[0].Path); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Launch() error = %v", err)
	}

	starter := &fakeStarter{err: errors.New("private process diagnostic")}
	if err := NewManager(starter, nil).Launch(context.Background(), component, component.Installations[0].Path); !errors.Is(err, ErrLaunchFailed) || err.Error() != ErrLaunchFailed.Error() {
		t.Fatalf("starter error = %v, want redacted ErrLaunchFailed", err)
	}
}

func TestManagerUsesPathOnlyAsFreshEvidenceSelectorForConflicts(t *testing.T) {
	component := installedComponent("codex-cli", "Core CLI", launchTestPath("usr", "bin", "codex"), launchTestPath("usr", "lib", "codex"), false)
	component.Status = domain.StatusConflict
	secondPath := launchTestPath("home", "test", ".local", "bin", "codex")
	component.Installations = append(component.Installations, domain.Installation{Path: secondPath, ResolvedPath: secondPath})
	starter := &fakeStarter{}
	manager := NewManager(starter, nil)
	if err := manager.Launch(context.Background(), component, component.Installations[1].Path); err != nil {
		t.Fatal(err)
	}
	if len(starter.requests) != 1 || starter.requests[0].Path != component.Installations[1].Path {
		t.Fatalf("selected request = %#v", starter.requests)
	}
	if err := manager.Launch(context.Background(), component, launchTestPath("tmp", "frontend-path")); !errors.Is(err, ErrLaunchUnavailable) {
		t.Fatalf("unscanned selector error = %v", err)
	}
}

func launchTestPath(parts ...string) string {
	root := string(filepath.Separator)
	if runtime.GOOS == "windows" {
		root = `C:\`
	}
	return filepath.Join(append([]string{root}, parts...)...)
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
