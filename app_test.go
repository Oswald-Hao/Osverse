package main

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Oswald-Hao/Osverse/internal/domain"
	historyservice "github.com/Oswald-Hao/Osverse/internal/history"
	"github.com/Oswald-Hao/Osverse/internal/install"
	"github.com/Oswald-Hao/Osverse/internal/profiles"
	proxyservice "github.com/Oswald-Hao/Osverse/internal/proxy"
	"github.com/Oswald-Hao/Osverse/internal/removal"
	"github.com/Oswald-Hao/Osverse/internal/selfupdate"
)

func TestNewAppRejectsNilScanner(t *testing.T) {
	if NewApp(nil) != nil {
		t.Fatal("NewApp(nil) returned non-nil")
	}
}

func TestScanEnvironmentUsesStartupContextAndReturnsSnapshot(t *testing.T) {
	contextKey := struct{}{}
	startupContext := context.WithValue(context.Background(), contextKey, "startup-value")
	want := domain.EnvironmentSnapshot{
		ScannedAt: time.Date(2026, time.August, 13, 1, 2, 3, 0, time.UTC),
		System:    domain.SystemInfo{Distribution: "Ubuntu 22.04"},
		Total:     8,
	}
	var gotContext context.Context
	app := NewApp(fakeScanner{scan: func(ctx context.Context) (domain.EnvironmentSnapshot, error) {
		gotContext = ctx
		return want, nil
	}})
	app.startup(startupContext)

	got, err := app.ScanEnvironment()

	if err != nil {
		t.Fatalf("ScanEnvironment() error = %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ScanEnvironment() = %#v, want %#v", got, want)
	}
	if gotContext != startupContext || gotContext.Value(contextKey) != "startup-value" {
		t.Errorf("scanner context = %#v, want Wails startup context", gotContext)
	}
}

func TestScanEnvironmentBeforeStartupUsesNonNilBackgroundContext(t *testing.T) {
	want := domain.EnvironmentSnapshot{Total: 8}
	var gotContext context.Context
	app := NewApp(fakeScanner{scan: func(ctx context.Context) (domain.EnvironmentSnapshot, error) {
		gotContext = ctx
		return want, nil
	}})

	got, err := app.ScanEnvironment()

	if err != nil {
		t.Fatalf("ScanEnvironment() error = %v", err)
	}
	if gotContext == nil {
		t.Fatal("scanner received nil context before startup")
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ScanEnvironment() = %#v, want %#v", got, want)
	}
}

func TestScanEnvironmentReturnsRedactedUnavailableErrorForNilState(t *testing.T) {
	tests := []struct {
		name string
		app  *App
	}{
		{name: "nil app", app: nil},
		{name: "nil scanner", app: &App{}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			snapshot, err := test.app.ScanEnvironment()
			if !reflect.DeepEqual(snapshot, domain.EnvironmentSnapshot{}) {
				t.Errorf("snapshot = %#v, want zero value", snapshot)
			}
			var public *domain.PublicError
			if !errors.As(err, &public) {
				t.Fatalf("error = %v (%T), want *domain.PublicError", err, err)
			}
			if public.Code != domain.ErrScanFailed {
				t.Errorf("error code = %q, want %q", public.Code, domain.ErrScanFailed)
			}
			if public.Message == "" {
				t.Error("public service-unavailable message is empty")
			}
		})
	}
}

func TestAppExposesOnlyFixedWailsOperations(t *testing.T) {
	typeOfApp := reflect.TypeOf((*App)(nil))
	want := []string{
		"ApplyAPIPlan", "CancelInstallTask", "CheckForAppUpdate", "ClearHistory", "CreateAPIApplyPlan", "CreateInstallPlan", "CreateRemovalPlan",
		"DeleteAPIProfile", "GetAPICompatibility", "GetInstallTask", "LaunchComponent",
		"ListAPIProfiles", "ListHistory", "ProbeAPIProfile", "ProbeProxy", "RemoveComponent", "SaveAPIProfile", "ScanEnvironment",
		"StartAppUpdate", "StartInstall", "UseDirectConnection",
	}
	if typeOfApp.NumMethod() != len(want) {
		t.Fatalf("exported App methods = %d, want %d fixed operations", typeOfApp.NumMethod(), len(want))
	}
	for index, name := range want {
		if method := typeOfApp.Method(index); method.Name != name {
			t.Fatalf("exported App method %d = %q, want %q", index, method.Name, name)
		}
	}
}

func TestRemovalBridgeRescansBeforePreviewAndConfirmation(t *testing.T) {
	component := domain.Component{
		ID: "codex-cli", Name: "Codex CLI", Category: "Core CLI", Status: domain.StatusInstalled,
		Installations: []domain.Installation{{Path: "/home/test/.local/bin/codex", ResolvedPath: "/home/test/.local/bin/codex"}},
	}
	scans := 0
	service := &fakeRemovalService{}
	service.create = func(ctx context.Context, got domain.Component) (removal.Plan, error) {
		if ctx == nil || !reflect.DeepEqual(got, component) {
			t.Fatalf("CreatePlan evidence = %#v", got)
		}
		return removal.Plan{ID: "remove-plan", ComponentID: component.ID}, nil
	}
	service.execute = func(ctx context.Context, id string, got domain.Component) (removal.Result, error) {
		if ctx == nil || id != "remove-plan" || !reflect.DeepEqual(got, component) {
			t.Fatalf("Execute(%q, %#v)", id, got)
		}
		return removal.Result{PlanID: id, ComponentID: component.ID, Removed: true}, nil
	}
	app := newAppWithServices(fakeScanner{scan: func(context.Context) (domain.EnvironmentSnapshot, error) {
		scans++
		return domain.EnvironmentSnapshot{Components: []domain.Component{component}}, nil
	}}, &fakeProxyProber{})
	app.removal = service
	plan, err := app.CreateRemovalPlan(component.ID)
	if err != nil || plan.ID != "remove-plan" {
		t.Fatalf("CreateRemovalPlan() = (%#v, %v)", plan, err)
	}
	result, err := app.RemoveComponent(plan.ID)
	if err != nil || !result.Removed || scans != 2 {
		t.Fatalf("RemoveComponent() = (%#v, %v), scans %d", result, err, scans)
	}

	service.create = func(context.Context, domain.Component) (removal.Plan, error) {
		return removal.Plan{}, errors.New("private removal diagnostic")
	}
	if _, err := app.CreateRemovalPlan(component.ID); err == nil || strings.Contains(err.Error(), "private") {
		t.Fatalf("removal failure was not redacted: %v", err)
	}
}

func TestLaunchComponentUsesOnlyFreshBackendScanEvidence(t *testing.T) {
	want := domain.Component{
		ID: "claude-code", Name: "Claude Code", Category: "Core CLI", Status: domain.StatusInstalled,
		Installations: []domain.Installation{{Path: "/home/test/.local/bin/claude", ResolvedPath: "/home/test/.local/share/claude/versions/2.1.232"}},
	}
	launcher := &fakeComponentLauncher{}
	app := newAppWithServices(fakeScanner{scan: func(context.Context) (domain.EnvironmentSnapshot, error) {
		return domain.EnvironmentSnapshot{Components: []domain.Component{want}}, nil
	}}, &fakeProxyProber{})
	app.componentLauncher = launcher

	if err := app.LaunchComponent("claude-code", want.Installations[0].Path); err != nil {
		t.Fatalf("LaunchComponent() error = %v", err)
	}
	if !reflect.DeepEqual(launcher.component, want) || launcher.context == nil {
		t.Fatalf("launcher received (%#v, %v)", launcher.component, launcher.context)
	}

	if err := app.LaunchComponent("unknown", "/tmp/payload"); err == nil {
		t.Fatal("unknown component launch succeeded")
	}
	launcher.err = errors.New("private launch diagnostic")
	if err := app.LaunchComponent("claude-code", want.Installations[0].Path); err == nil || strings.Contains(err.Error(), "private") {
		t.Fatalf("launch failure was not redacted: %v", err)
	}
}

func TestAPIProfileBridgeUsesOnlyRedactedResultsAndPublicErrors(t *testing.T) {
	service := &fakeProfileService{}
	service.save = func(ctx context.Context, input profiles.Input) (profiles.Profile, error) {
		if ctx == nil || input.APIKey != "secret-value" {
			t.Fatalf("Save context/input = (%v, %#v)", ctx, input)
		}
		return profiles.Profile{ID: "profile", Name: input.Name, KeyHint: "••••alue"}, nil
	}
	service.list = func(context.Context) ([]profiles.Profile, error) {
		return []profiles.Profile{{ID: "profile", KeyHint: "••••alue"}}, nil
	}
	service.probe = func(context.Context, string) (profiles.ProbeResult, error) {
		return profiles.ProbeResult{ProfileID: "profile", Reachable: true}, nil
	}
	service.compatibility = func(string) ([]profiles.TargetCompatibility, error) {
		return []profiles.TargetCompatibility{{Target: profiles.TargetCodex, Compatible: true}}, nil
	}
	service.createPlan = func(context.Context, string, []string) (profiles.ApplyPlan, error) {
		return profiles.ApplyPlan{ID: "apply-plan"}, nil
	}
	service.apply = func(context.Context, string) (profiles.ApplyBatchResult, error) {
		return profiles.ApplyBatchResult{PlanID: "apply-plan", Succeeded: 1}, nil
	}
	service.delete = func(context.Context, string) error { return nil }
	app := newAppWithServiceBundle(fakeScanner{scan: func(context.Context) (domain.EnvironmentSnapshot, error) {
		return domain.EnvironmentSnapshot{}, nil
	}}, &fakeProxyProber{}, nil, service)

	profile, err := app.SaveAPIProfile(profiles.Input{Name: "Work", APIKey: "secret-value", BaseURL: "https://api.example", Model: "model"})
	if err != nil || profile.KeyHint != "••••alue" {
		t.Fatalf("SaveAPIProfile() = (%#v, %v)", profile, err)
	}
	listed, err := app.ListAPIProfiles()
	if err != nil || len(listed) != 1 {
		t.Fatalf("ListAPIProfiles() = (%#v, %v)", listed, err)
	}
	if result, err := app.ProbeAPIProfile("profile"); err != nil || !result.Reachable {
		t.Fatalf("ProbeAPIProfile() = (%#v, %v)", result, err)
	}
	if result, err := app.GetAPICompatibility("profile"); err != nil || !result[0].Compatible {
		t.Fatalf("GetAPICompatibility() = (%#v, %v)", result, err)
	}
	plan, err := app.CreateAPIApplyPlan("profile", []string{profiles.TargetCodex})
	if err != nil || plan.ID != "apply-plan" {
		t.Fatalf("CreateAPIApplyPlan() = (%#v, %v)", plan, err)
	}
	if result, err := app.ApplyAPIPlan(plan.ID); err != nil || result.Succeeded != 1 {
		t.Fatalf("ApplyAPIPlan() = (%#v, %v)", result, err)
	}
	if err := app.DeleteAPIProfile("profile"); err != nil {
		t.Fatalf("DeleteAPIProfile() error = %v", err)
	}

	service.save = func(context.Context, profiles.Input) (profiles.Profile, error) {
		return profiles.Profile{}, errors.New("secret backend diagnostic")
	}
	_, err = app.SaveAPIProfile(profiles.Input{})
	var public *domain.PublicError
	if !errors.As(err, &public) || public.Code != domain.ErrProfileFailed || strings.Contains(public.Error(), "secret") {
		t.Fatalf("profile public error = %v", err)
	}
}

func TestInstallTaskBridgeUsesInternalProxyAndRedactsErrors(t *testing.T) {
	manager := &fakeInstallManager{}
	manager.create = func(context.Context, string) (install.Plan, error) {
		return install.Plan{ID: "plan"}, nil
	}
	manager.start = func(_ context.Context, planID string, protocol proxyservice.Protocol, port int) (install.Task, error) {
		if planID != "plan" || protocol != proxyservice.ProtocolSOCKS5 || port != 7890 {
			t.Fatalf("Start(%q, %q, %d)", planID, protocol, port)
		}
		return install.Task{ID: "task", PlanID: planID}, nil
	}
	manager.task = func(id string) (install.Task, error) {
		return install.Task{ID: id, Phase: "downloading"}, nil
	}
	manager.cancel = func(id string) error {
		if id != "task" {
			t.Fatalf("Cancel(%q)", id)
		}
		return nil
	}
	app := newAppWithAllServices(fakeScanner{scan: func(context.Context) (domain.EnvironmentSnapshot, error) {
		return domain.EnvironmentSnapshot{}, nil
	}}, &fakeProxyProber{}, manager)
	app.proxySelection = proxySelection{Protocol: proxyservice.ProtocolSOCKS5, Port: 7890}

	started, err := app.StartInstall("plan")
	if err != nil || started.ID != "task" {
		t.Fatalf("StartInstall() = (%#v, %v)", started, err)
	}
	progress, err := app.GetInstallTask("task")
	if err != nil || progress.Phase != "downloading" {
		t.Fatalf("GetInstallTask() = (%#v, %v)", progress, err)
	}
	if err := app.CancelInstallTask("task"); err != nil {
		t.Fatalf("CancelInstallTask() error = %v", err)
	}

	secret := "artifact-url-secret"
	manager.start = func(context.Context, string, proxyservice.Protocol, int) (install.Task, error) {
		return install.Task{}, errors.New(secret)
	}
	_, err = app.StartInstall("plan")
	var public *domain.PublicError
	if !errors.As(err, &public) || public.Code != domain.ErrInstallTaskFailed || strings.Contains(public.Error(), secret) {
		t.Fatalf("public start error = %v", err)
	}
}

func TestDesktopInstallPlansAndTasksStayOnDesktopManager(t *testing.T) {
	cli := &fakeInstallManager{}
	cli.create = func(context.Context, string) (install.Plan, error) {
		t.Fatal("desktop plan reached CLI manager")
		return install.Plan{}, nil
	}
	desktop := &fakeInstallManager{}
	desktop.create = func(_ context.Context, id string) (install.Plan, error) {
		if id != "cc-switch" {
			t.Fatalf("desktop CreatePlan(%q)", id)
		}
		return install.Plan{ID: "app-plan", ComponentID: id}, nil
	}
	desktop.start = func(_ context.Context, id string, _ proxyservice.Protocol, _ int) (install.Task, error) {
		if id != "app-plan" {
			t.Fatalf("desktop Start(%q)", id)
		}
		return install.Task{ID: "app-task", PlanID: id}, nil
	}
	desktop.task = func(id string) (install.Task, error) { return install.Task{ID: id, Phase: "completed"}, nil }
	desktop.cancel = func(id string) error {
		if id != "app-task" {
			t.Fatalf("desktop Cancel(%q)", id)
		}
		return nil
	}
	app := newAppWithAllServices(fakeScanner{scan: func(context.Context) (domain.EnvironmentSnapshot, error) {
		return domain.EnvironmentSnapshot{}, nil
	}}, &fakeProxyProber{}, cli)
	app.appPlanner, app.appExecutor = desktop, desktop
	plan, err := app.CreateInstallPlan("cc-switch")
	if err != nil || plan.ID != "app-plan" {
		t.Fatalf("CreateInstallPlan() = (%#v, %v)", plan, err)
	}
	task, err := app.StartInstall(plan.ID)
	if err != nil || task.ID != "app-task" {
		t.Fatalf("StartInstall() = (%#v, %v)", task, err)
	}
	if task, err = app.GetInstallTask(task.ID); err != nil || task.Phase != "completed" {
		t.Fatalf("GetInstallTask() = (%#v, %v)", task, err)
	}
	if err := app.CancelInstallTask(task.ID); err != nil {
		t.Fatal(err)
	}
}

func TestClaudeDesktopInstallPlansStayOnSystemManager(t *testing.T) {
	cli := &fakeInstallPlanner{create: func(context.Context, string) (install.Plan, error) {
		t.Fatal("system plan reached CLI manager")
		return install.Plan{}, nil
	}}
	system := &fakeInstallPlanner{create: func(_ context.Context, id string) (install.Plan, error) {
		if id != "claude-desktop" {
			t.Fatalf("system CreatePlan(%q)", id)
		}
		return install.Plan{ID: "system-plan", ComponentID: id}, nil
	}}
	app := newAppWithAllServices(fakeScanner{scan: func(context.Context) (domain.EnvironmentSnapshot, error) {
		return domain.EnvironmentSnapshot{}, nil
	}}, &fakeProxyProber{}, cli)
	app.systemPlanner = system
	plan, err := app.CreateInstallPlan("claude-desktop")
	if err != nil || plan.ID != "system-plan" || app.planOwners[plan.ID] != "system" {
		t.Fatalf("CreateInstallPlan() = (%#v, %v), owner %q", plan, err, app.planOwners[plan.ID])
	}
}

func TestHistoryBridgeRecordsTerminalInstallExactlyOnceAndClears(t *testing.T) {
	manager := &fakeInstallManager{}
	manager.create = func(context.Context, string) (install.Plan, error) { return install.Plan{}, nil }
	manager.task = func(id string) (install.Task, error) {
		return install.Task{ID: id, ComponentID: "codex-cli", Phase: "completed", Message: "安装完成"}, nil
	}
	manager.cancel = func(string) error { return nil }
	ledger := &fakeHistoryService{}
	app := newAppWithAllServices(fakeScanner{scan: func(context.Context) (domain.EnvironmentSnapshot, error) { return domain.EnvironmentSnapshot{}, nil }}, &fakeProxyProber{}, manager)
	app.history = ledger
	if _, err := app.GetInstallTask("task"); err != nil {
		t.Fatal(err)
	}
	if _, err := app.GetInstallTask("task"); err != nil {
		t.Fatal(err)
	}
	if len(ledger.appended) != 1 || ledger.appended[0].ComponentID != "codex-cli" || ledger.appended[0].Name != "Codex CLI" {
		t.Fatalf("history = %#v", ledger.appended)
	}
	ledger.entries = []historyservice.Entry{{ID: "entry", ComponentID: "codex-cli"}}
	entries, err := app.ListHistory()
	if err != nil || len(entries) != 1 {
		t.Fatalf("ListHistory() = (%#v, %v)", entries, err)
	}
	if err := app.ClearHistory(); err != nil || !ledger.cleared {
		t.Fatalf("ClearHistory() = %v, cleared %v", err, ledger.cleared)
	}
}

func TestCreateInstallPlanUsesStartupContextAndMapsFailures(t *testing.T) {
	startup := context.WithValue(context.Background(), struct{}{}, "value")
	want := install.Plan{ID: "plan", ComponentID: "codex-cli"}
	planner := &fakeInstallPlanner{create: func(ctx context.Context, id string) (install.Plan, error) {
		if ctx != startup || id != "codex-cli" {
			t.Fatalf("CreatePlan(%p, %q), want startup context and codex-cli", ctx, id)
		}
		return want, nil
	}}
	app := newAppWithAllServices(fakeScanner{scan: func(context.Context) (domain.EnvironmentSnapshot, error) {
		return domain.EnvironmentSnapshot{}, nil
	}}, &fakeProxyProber{}, planner)
	app.startup(startup)

	got, err := app.CreateInstallPlan("codex-cli")
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("CreateInstallPlan() = (%#v, %v), want %#v", got, err, want)
	}

	planner.create = func(context.Context, string) (install.Plan, error) {
		return install.Plan{}, install.ErrUnknownComponent
	}
	_, err = app.CreateInstallPlan("other")
	var public *domain.PublicError
	if !errors.As(err, &public) || public.Code != domain.ErrInstallPlanFailed {
		t.Fatalf("unknown plan error = %v", err)
	}
}

func TestCreateInstallPlanUnavailableIsRedacted(t *testing.T) {
	app := newAppWithServices(fakeScanner{scan: func(context.Context) (domain.EnvironmentSnapshot, error) {
		return domain.EnvironmentSnapshot{}, nil
	}}, &fakeProxyProber{})

	_, err := app.CreateInstallPlan("codex-cli")
	var public *domain.PublicError
	if !errors.As(err, &public) || public.Code != domain.ErrInstallUnavailable {
		t.Fatalf("unavailable plan error = %v", err)
	}
}

func TestProbeProxyStoresOnlyHTTPSCapableSuccessfulSelection(t *testing.T) {
	prober := &fakeProxyProber{result: proxyservice.Result{
		Port: 7890, Reachable: true, Recommended: proxyservice.ProtocolSOCKS5,
	}}
	app := newAppWithServices(fakeScanner{scan: func(context.Context) (domain.EnvironmentSnapshot, error) {
		return domain.EnvironmentSnapshot{}, nil
	}}, prober)

	result, err := app.ProbeProxy(7890)

	if err != nil || !reflect.DeepEqual(result, prober.result) {
		t.Fatalf("ProbeProxy() = (%#v, %v), want %#v", result, err, prober.result)
	}
	selection := app.currentProxy()
	if selection.Port != 7890 || selection.Protocol != proxyservice.ProtocolSOCKS5 {
		t.Fatalf("stored proxy = %#v", selection)
	}

	prober.result = proxyservice.Result{Port: 8080}
	if _, err := app.ProbeProxy(8080); err != nil {
		t.Fatalf("failed-protocol ProbeProxy() error = %v", err)
	}
	if selection := app.currentProxy(); selection != (proxySelection{}) {
		t.Fatalf("unusable probe retained selection: %#v", selection)
	}
}

func TestProbeProxyMapsInvalidInputAndRedactsBackendFailure(t *testing.T) {
	secret := "proxy-password-secret"
	tests := []struct {
		name string
		port int
		err  error
		code domain.ErrorCode
	}{
		{name: "invalid port", port: 0, err: proxyservice.ErrInvalidPort, code: domain.ErrInvalidInput},
		{name: "backend failure", port: 7890, err: errors.New(secret), code: domain.ErrProxyProbeFailed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := newAppWithServices(fakeScanner{scan: func(context.Context) (domain.EnvironmentSnapshot, error) {
				return domain.EnvironmentSnapshot{}, nil
			}}, &fakeProxyProber{err: tt.err})

			_, err := app.ProbeProxy(tt.port)

			var public *domain.PublicError
			if !errors.As(err, &public) || public.Code != tt.code {
				t.Fatalf("error = %v, want public code %q", err, tt.code)
			}
			if strings.Contains(public.Error(), secret) {
				t.Fatalf("public error leaked backend failure: %v", public)
			}
		})
	}
}

func TestUseDirectConnectionClearsProxySelection(t *testing.T) {
	app := newAppWithServices(fakeScanner{scan: func(context.Context) (domain.EnvironmentSnapshot, error) {
		return domain.EnvironmentSnapshot{}, nil
	}}, &fakeProxyProber{result: proxyservice.Result{
		Port: 7890, Reachable: true, Recommended: proxyservice.ProtocolHTTPSConnect,
	}})
	if _, err := app.ProbeProxy(7890); err != nil {
		t.Fatalf("ProbeProxy() error = %v", err)
	}

	app.UseDirectConnection()

	if selection := app.currentProxy(); selection != (proxySelection{}) {
		t.Fatalf("proxy selection after direct mode = %#v", selection)
	}
}

func TestUseDirectConnectionInvalidatesAnOlderProbeResult(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	prober := &fakeProxyProber{probe: func(context.Context, int) (proxyservice.Result, error) {
		close(started)
		<-release
		return proxyservice.Result{
			Port: 7890, Reachable: true, Recommended: proxyservice.ProtocolHTTPSConnect,
		}, nil
	}}
	app := newAppWithServices(fakeScanner{scan: func(context.Context) (domain.EnvironmentSnapshot, error) {
		return domain.EnvironmentSnapshot{}, nil
	}}, prober)
	done := make(chan error, 1)
	go func() {
		_, err := app.ProbeProxy(7890)
		done <- err
	}()
	<-started

	app.UseDirectConnection()
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("ProbeProxy() error = %v", err)
	}
	if selection := app.currentProxy(); selection != (proxySelection{}) {
		t.Fatalf("stale probe restored proxy: %#v", selection)
	}
}

type fakeScanner struct {
	scan func(context.Context) (domain.EnvironmentSnapshot, error)
}

type fakeProxyProber struct {
	result proxyservice.Result
	err    error
	probe  func(context.Context, int) (proxyservice.Result, error)
}

type fakeInstallPlanner struct {
	create func(context.Context, string) (install.Plan, error)
}

func (planner *fakeInstallPlanner) CreatePlan(ctx context.Context, id string) (install.Plan, error) {
	return planner.create(ctx, id)
}

type fakeInstallManager struct {
	fakeInstallPlanner
	start  func(context.Context, string, proxyservice.Protocol, int) (install.Task, error)
	task   func(string) (install.Task, error)
	cancel func(string) error
}

type fakeProfileService struct {
	save          func(context.Context, profiles.Input) (profiles.Profile, error)
	list          func(context.Context) ([]profiles.Profile, error)
	delete        func(context.Context, string) error
	probe         func(context.Context, string) (profiles.ProbeResult, error)
	compatibility func(string) ([]profiles.TargetCompatibility, error)
	createPlan    func(context.Context, string, []string) (profiles.ApplyPlan, error)
	apply         func(context.Context, string) (profiles.ApplyBatchResult, error)
}

type fakeHistoryService struct {
	appended []historyservice.Input
	entries  []historyservice.Entry
	cleared  bool
}

type fakeComponentLauncher struct {
	context   context.Context
	component domain.Component
	selector  string
	err       error
}

type fakeRemovalService struct {
	create  func(context.Context, domain.Component) (removal.Plan, error)
	execute func(context.Context, string, domain.Component) (removal.Result, error)
}

func (service *fakeRemovalService) CreatePlan(ctx context.Context, component domain.Component) (removal.Plan, error) {
	return service.create(ctx, component)
}

func (service *fakeRemovalService) Execute(ctx context.Context, id string, component domain.Component) (removal.Result, error) {
	return service.execute(ctx, id, component)
}

func (launcher *fakeComponentLauncher) Launch(ctx context.Context, component domain.Component, selector string) error {
	launcher.context, launcher.component, launcher.selector = ctx, component, selector
	return launcher.err
}

func (service *fakeHistoryService) Append(_ context.Context, input historyservice.Input) (historyservice.Entry, error) {
	service.appended = append(service.appended, input)
	return historyservice.Entry{ID: "entry"}, nil
}
func (service *fakeHistoryService) List(context.Context) ([]historyservice.Entry, error) {
	return service.entries, nil
}
func (service *fakeHistoryService) Clear(context.Context) error { service.cleared = true; return nil }

func (service *fakeProfileService) Save(ctx context.Context, input profiles.Input) (profiles.Profile, error) {
	return service.save(ctx, input)
}
func (service *fakeProfileService) List(ctx context.Context) ([]profiles.Profile, error) {
	return service.list(ctx)
}
func (service *fakeProfileService) Delete(ctx context.Context, id string) error {
	return service.delete(ctx, id)
}
func (service *fakeProfileService) Probe(ctx context.Context, id string) (profiles.ProbeResult, error) {
	return service.probe(ctx, id)
}
func (service *fakeProfileService) Compatibility(id string) ([]profiles.TargetCompatibility, error) {
	return service.compatibility(id)
}
func (service *fakeProfileService) CreateApplyPlan(ctx context.Context, id string, targets []string) (profiles.ApplyPlan, error) {
	return service.createPlan(ctx, id, targets)
}
func (service *fakeProfileService) Apply(ctx context.Context, id string) (profiles.ApplyBatchResult, error) {
	return service.apply(ctx, id)
}

func (manager *fakeInstallManager) Start(ctx context.Context, id string, protocol proxyservice.Protocol, port int) (install.Task, error) {
	return manager.start(ctx, id, protocol, port)
}

func (manager *fakeInstallManager) Task(id string) (install.Task, error) {
	return manager.task(id)
}

func (manager *fakeInstallManager) Cancel(id string) error {
	return manager.cancel(id)
}

func (prober *fakeProxyProber) Probe(ctx context.Context, port int) (proxyservice.Result, error) {
	if prober.probe != nil {
		return prober.probe(ctx, port)
	}
	return prober.result, prober.err
}

func TestDefaultWindowMatchesCockpitTools(t *testing.T) {
	if defaultWindowWidth != 1280 || defaultWindowHeight != 800 {
		t.Fatalf("default window = %dx%d, want Cockpit Tools 1280x800",
			defaultWindowWidth, defaultWindowHeight)
	}
}

type fakeAppUpdater struct {
	checkedProtocol proxyservice.Protocol
	checkedPort     int
	appliedPlan     string
	result          selfupdate.ApplyResult
}

func (updater *fakeAppUpdater) Check(_ context.Context, protocol proxyservice.Protocol, port int) (selfupdate.Info, error) {
	updater.checkedProtocol, updater.checkedPort = protocol, port
	return selfupdate.Info{Available: true, CanInstall: true, PlanID: "update-plan", CurrentVersion: "1.0.0", LatestVersion: "1.1.0"}, nil
}

func (updater *fakeAppUpdater) Apply(_ context.Context, planID string, protocol proxyservice.Protocol, port int) (selfupdate.ApplyResult, error) {
	updater.appliedPlan, updater.checkedProtocol, updater.checkedPort = planID, protocol, port
	return updater.result, nil
}

func TestAppUpdateBridgeUsesSelectedProxyAndOpaquePlan(t *testing.T) {
	app := newAppWithServices(fakeScanner{scan: func(context.Context) (domain.EnvironmentSnapshot, error) { return domain.EnvironmentSnapshot{}, nil }}, &fakeProxyProber{})
	updater := &fakeAppUpdater{result: selfupdate.ApplyResult{Started: true, Message: "started"}}
	app.updater = updater
	app.proxySelection = proxySelection{Protocol: proxyservice.ProtocolSOCKS5, Port: 7890}
	info, err := app.CheckForAppUpdate()
	if err != nil || info.PlanID != "update-plan" || updater.checkedProtocol != proxyservice.ProtocolSOCKS5 || updater.checkedPort != 7890 {
		t.Fatalf("CheckForAppUpdate() = (%#v, %v), proxy=(%q,%d)", info, err, updater.checkedProtocol, updater.checkedPort)
	}
	result, err := app.StartAppUpdate(info.PlanID)
	if err != nil || !result.Started || updater.appliedPlan != "update-plan" {
		t.Fatalf("StartAppUpdate() = (%#v, %v), plan=%q", result, err, updater.appliedPlan)
	}
}

func (scanner fakeScanner) Scan(ctx context.Context) (domain.EnvironmentSnapshot, error) {
	return scanner.scan(ctx)
}
