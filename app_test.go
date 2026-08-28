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
		"CurrentProxySelection", "DeleteAPIProfile", "GetAPICompatibility", "GetInstallTask", "LaunchComponent",
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

func TestRemovalBridgeReturnsActionableComponentInUseError(t *testing.T) {
	component := domain.Component{
		ID: "deepseek-harness", Name: "DeepSeek Harness", Category: "Core CLI", Status: domain.StatusInstalled,
		Installations: []domain.Installation{{Path: `C:\Users\test\.local\bin\dsh.cmd`, Managed: true}},
	}
	service := &fakeRemovalService{
		create: func(context.Context, domain.Component) (removal.Plan, error) {
			return removal.Plan{ID: "remove-running-harness", ComponentID: component.ID}, nil
		},
		execute: func(context.Context, string, domain.Component) (removal.Result, error) {
			return removal.Result{}, removal.ErrComponentInUse
		},
	}
	app := newAppWithServices(fakeScanner{scan: func(context.Context) (domain.EnvironmentSnapshot, error) {
		return domain.EnvironmentSnapshot{Components: []domain.Component{component}}, nil
	}}, &fakeProxyProber{})
	app.removal = service
	plan, err := app.CreateRemovalPlan(component.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = app.RemoveComponent(plan.ID)
	var public *domain.PublicError
	if !errors.As(err, &public) {
		t.Fatalf("RemoveComponent() error = %v, want public error", err)
	}
	if public.Code != domain.ErrRemovalInUse {
		t.Errorf("error code = %q, want %q", public.Code, domain.ErrRemovalInUse)
	}
	for _, text := range []string{"正在运行", "关闭", "重试", "原安装保持不变"} {
		if !strings.Contains(public.Message, text) {
			t.Errorf("public message %q does not contain %q", public.Message, text)
		}
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

func TestHarnessInstallPlansAndTasksStayOnHarnessManager(t *testing.T) {
	cli := &fakeInstallManager{fakeInstallPlanner: fakeInstallPlanner{create: func(context.Context, string) (install.Plan, error) {
		t.Fatal("Harness plan reached generic CLI manager")
		return install.Plan{}, nil
	}}}
	harness := &fakeInstallManager{}
	harness.create = func(_ context.Context, id string) (install.Plan, error) {
		if id != "deepseek-harness" {
			t.Fatalf("Harness CreatePlan(%q)", id)
		}
		return install.Plan{ID: "harness-plan", ComponentID: id}, nil
	}
	harness.start = func(_ context.Context, id string, _ proxyservice.Protocol, _ int) (install.Task, error) {
		return install.Task{ID: "harness-task", PlanID: id, ComponentID: "deepseek-harness"}, nil
	}
	harness.task = func(id string) (install.Task, error) {
		return install.Task{ID: id, ComponentID: "deepseek-harness", Phase: "completed"}, nil
	}
	harness.cancel = func(string) error { return nil }
	app := newAppWithAllServices(fakeScanner{scan: func(context.Context) (domain.EnvironmentSnapshot, error) {
		return domain.EnvironmentSnapshot{}, nil
	}}, &fakeProxyProber{}, cli)
	app.harnessPlanner, app.harnessExecutor = harness, harness
	plan, err := app.CreateInstallPlan("deepseek-harness")
	if err != nil || app.planOwners[plan.ID] != "harness" {
		t.Fatalf("CreateInstallPlan() = (%#v, %v), owner=%q", plan, err, app.planOwners[plan.ID])
	}
	task, err := app.StartInstall(plan.ID)
	if err != nil || task.ID != "harness-task" {
		t.Fatalf("StartInstall() = (%#v, %v)", task, err)
	}
	if current, err := app.GetInstallTask(task.ID); err != nil || current.Phase != "completed" {
		t.Fatalf("GetInstallTask() = (%#v, %v)", current, err)
	}
	if err := app.CancelInstallTask(task.ID); err != nil {
		t.Fatal(err)
	}
}

func TestQwenInstallPlansAndTasksStayOnQwenManager(t *testing.T) {
	cli := &fakeInstallManager{fakeInstallPlanner: fakeInstallPlanner{create: func(context.Context, string) (install.Plan, error) {
		t.Fatal("Qwen plan reached generic CLI manager")
		return install.Plan{}, nil
	}}}
	qwen := &fakeInstallManager{}
	qwen.create = func(_ context.Context, id string) (install.Plan, error) {
		if id != "qwen-code" {
			t.Fatalf("Qwen CreatePlan(%q)", id)
		}
		return install.Plan{ID: "qwen-plan", ComponentID: id}, nil
	}
	qwen.start = func(_ context.Context, id string, _ proxyservice.Protocol, _ int) (install.Task, error) {
		return install.Task{ID: "qwen-task", PlanID: id, ComponentID: "qwen-code"}, nil
	}
	qwen.task = func(id string) (install.Task, error) {
		return install.Task{ID: id, ComponentID: "qwen-code", Phase: "completed"}, nil
	}
	qwen.cancel = func(string) error { return nil }
	app := newAppWithAllServices(fakeScanner{scan: func(context.Context) (domain.EnvironmentSnapshot, error) {
		return domain.EnvironmentSnapshot{}, nil
	}}, &fakeProxyProber{}, cli)
	app.qwenPlanner, app.qwenExecutor = qwen, qwen
	plan, err := app.CreateInstallPlan("qwen-code")
	if err != nil || app.planOwners[plan.ID] != "qwen" {
		t.Fatalf("CreateInstallPlan() = (%#v, %v), owner=%q", plan, err, app.planOwners[plan.ID])
	}
	task, err := app.StartInstall(plan.ID)
	if err != nil || task.ID != "qwen-task" {
		t.Fatalf("StartInstall() = (%#v, %v)", task, err)
	}
	if current, err := app.GetInstallTask(task.ID); err != nil || current.Phase != "completed" {
		t.Fatalf("GetInstallTask() = (%#v, %v)", current, err)
	}
	if err := app.CancelInstallTask(task.ID); err != nil {
		t.Fatal(err)
	}
}

func TestCopilotInstallPlansAndTasksStayOnCopilotManager(t *testing.T) {
	cli := &fakeInstallManager{fakeInstallPlanner: fakeInstallPlanner{create: func(context.Context, string) (install.Plan, error) {
		t.Fatal("Copilot plan reached generic CLI manager")
		return install.Plan{}, nil
	}}}
	copilot := &fakeInstallManager{}
	copilot.create = func(_ context.Context, id string) (install.Plan, error) {
		if id != "github-copilot-cli" {
			t.Fatalf("Copilot CreatePlan(%q)", id)
		}
		return install.Plan{ID: "copilot-plan", ComponentID: id}, nil
	}
	copilot.start = func(_ context.Context, id string, _ proxyservice.Protocol, _ int) (install.Task, error) {
		return install.Task{ID: "copilot-task", PlanID: id, ComponentID: "github-copilot-cli"}, nil
	}
	copilot.task = func(id string) (install.Task, error) {
		return install.Task{ID: id, ComponentID: "github-copilot-cli", Phase: "completed"}, nil
	}
	copilot.cancel = func(string) error { return nil }
	app := newAppWithAllServices(fakeScanner{scan: func(context.Context) (domain.EnvironmentSnapshot, error) {
		return domain.EnvironmentSnapshot{}, nil
	}}, &fakeProxyProber{}, cli)
	app.copilotPlanner, app.copilotExecutor = copilot, copilot
	plan, err := app.CreateInstallPlan("github-copilot-cli")
	if err != nil || app.planOwners[plan.ID] != "copilot" {
		t.Fatalf("CreateInstallPlan() = (%#v, %v), owner=%q", plan, err, app.planOwners[plan.ID])
	}
	task, err := app.StartInstall(plan.ID)
	if err != nil || task.ID != "copilot-task" {
		t.Fatalf("StartInstall() = (%#v, %v)", task, err)
	}
	if current, err := app.GetInstallTask(task.ID); err != nil || current.Phase != "completed" {
		t.Fatalf("GetInstallTask() = (%#v, %v)", current, err)
	}
	if err := app.CancelInstallTask(task.ID); err != nil {
		t.Fatal(err)
	}
}

func TestGeminiInstallPlansAndTasksStayOnGeminiManager(t *testing.T) {
	cli := &fakeInstallManager{fakeInstallPlanner: fakeInstallPlanner{create: func(context.Context, string) (install.Plan, error) {
		t.Fatal("Gemini plan reached generic CLI manager")
		return install.Plan{}, nil
	}}}
	gemini := &fakeInstallManager{}
	gemini.create = func(_ context.Context, id string) (install.Plan, error) {
		if id != "gemini-cli" {
			t.Fatalf("Gemini CreatePlan(%q)", id)
		}
		return install.Plan{ID: "gemini-plan", ComponentID: id}, nil
	}
	gemini.start = func(_ context.Context, id string, _ proxyservice.Protocol, _ int) (install.Task, error) {
		return install.Task{ID: "gemini-task", PlanID: id, ComponentID: "gemini-cli"}, nil
	}
	gemini.task = func(id string) (install.Task, error) {
		return install.Task{ID: id, ComponentID: "gemini-cli", Phase: "completed"}, nil
	}
	gemini.cancel = func(string) error { return nil }
	app := newAppWithAllServices(fakeScanner{scan: func(context.Context) (domain.EnvironmentSnapshot, error) {
		return domain.EnvironmentSnapshot{}, nil
	}}, &fakeProxyProber{}, cli)
	app.geminiPlanner, app.geminiExecutor = gemini, gemini
	plan, err := app.CreateInstallPlan("gemini-cli")
	if err != nil || app.planOwners[plan.ID] != "gemini" {
		t.Fatalf("CreateInstallPlan() = (%#v, %v), owner=%q", plan, err, app.planOwners[plan.ID])
	}
	task, err := app.StartInstall(plan.ID)
	if err != nil || task.ID != "gemini-task" {
		t.Fatalf("StartInstall() = (%#v, %v)", task, err)
	}
	if current, err := app.GetInstallTask(task.ID); err != nil || current.Phase != "completed" {
		t.Fatalf("GetInstallTask() = (%#v, %v)", current, err)
	}
	if err := app.CancelInstallTask(task.ID); err != nil {
		t.Fatal(err)
	}
}

func TestKimiInstallPlansAndTasksStayOnKimiManager(t *testing.T) {
	cli := &fakeInstallManager{fakeInstallPlanner: fakeInstallPlanner{create: func(context.Context, string) (install.Plan, error) {
		t.Fatal("Kimi plan reached generic CLI manager")
		return install.Plan{}, nil
	}}}
	kimi := &fakeInstallManager{}
	kimi.create = func(_ context.Context, id string) (install.Plan, error) {
		if id != "kimi-code" {
			t.Fatalf("Kimi CreatePlan(%q)", id)
		}
		return install.Plan{ID: "kimi-plan", ComponentID: id}, nil
	}
	kimi.start = func(_ context.Context, id string, _ proxyservice.Protocol, _ int) (install.Task, error) {
		return install.Task{ID: "kimi-task", PlanID: id, ComponentID: "kimi-code"}, nil
	}
	kimi.task = func(id string) (install.Task, error) {
		return install.Task{ID: id, ComponentID: "kimi-code", Phase: "completed"}, nil
	}
	kimi.cancel = func(string) error { return nil }
	app := newAppWithAllServices(fakeScanner{scan: func(context.Context) (domain.EnvironmentSnapshot, error) {
		return domain.EnvironmentSnapshot{}, nil
	}}, &fakeProxyProber{}, cli)
	app.kimiPlanner, app.kimiExecutor = kimi, kimi
	plan, err := app.CreateInstallPlan("kimi-code")
	if err != nil || app.planOwners[plan.ID] != "kimi" {
		t.Fatalf("CreateInstallPlan() = (%#v, %v), owner=%q", plan, err, app.planOwners[plan.ID])
	}
	task, err := app.StartInstall(plan.ID)
	if err != nil || task.ID != "kimi-task" {
		t.Fatalf("StartInstall() = (%#v, %v)", task, err)
	}
	if current, err := app.GetInstallTask(task.ID); err != nil || current.Phase != "completed" {
		t.Fatalf("GetInstallTask() = (%#v, %v)", current, err)
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

	if err := app.UseDirectConnection(); err != nil {
		t.Fatal(err)
	}

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

	if err := app.UseDirectConnection(); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("ProbeProxy() error = %v", err)
	}
	if selection := app.currentProxy(); selection != (proxySelection{}) {
		t.Fatalf("stale probe restored proxy: %#v", selection)
	}
}

func TestProxySelectionStoreRestoresPersistsAndExposesCurrentSelection(t *testing.T) {
	initial := proxyservice.Selection{Protocol: proxyservice.ProtocolSOCKS5, Port: 7897}
	store := &fakeProxySelectionStore{loaded: initial}
	app := newAppWithServices(fakeScanner{scan: func(context.Context) (domain.EnvironmentSnapshot, error) {
		return domain.EnvironmentSnapshot{}, nil
	}}, &fakeProxyProber{result: proxyservice.Result{
		Port: 2080, Reachable: true, Recommended: proxyservice.ProtocolHTTPSConnect,
	}})

	attachProxySelectionStore(app, store)
	if selection := app.CurrentProxySelection(); selection != initial {
		t.Fatalf("restored selection = %#v, want %#v", selection, initial)
	}
	if _, err := app.ProbeProxy(2080); err != nil {
		t.Fatal(err)
	}
	want := proxyservice.Selection{Protocol: proxyservice.ProtocolHTTPSConnect, Port: 2080}
	if len(store.saved) != 1 || store.saved[0] != want || app.CurrentProxySelection() != want {
		t.Fatalf("saved = %#v, current = %#v, want %#v", store.saved, app.CurrentProxySelection(), want)
	}
	if err := app.UseDirectConnection(); err != nil {
		t.Fatal(err)
	}
	if !store.cleared || app.CurrentProxySelection() != (proxyservice.Selection{}) {
		t.Fatalf("cleared = %t, current = %#v", store.cleared, app.CurrentProxySelection())
	}
}

func TestProxySelectionStoreFailuresCannotCreateFalseUIState(t *testing.T) {
	secret := errors.New("private filesystem detail")
	store := &fakeProxySelectionStore{saveErr: secret}
	app := newAppWithServices(fakeScanner{scan: func(context.Context) (domain.EnvironmentSnapshot, error) {
		return domain.EnvironmentSnapshot{}, nil
	}}, &fakeProxyProber{result: proxyservice.Result{
		Port: 7897, Reachable: true, Recommended: proxyservice.ProtocolSOCKS5,
	}})
	attachProxySelectionStore(app, store)
	_, err := app.ProbeProxy(7897)
	var public *domain.PublicError
	if !errors.As(err, &public) || public.Code != domain.ErrProxyProbeFailed || strings.Contains(public.Error(), secret.Error()) {
		t.Fatalf("ProbeProxy() error = %v", err)
	}
	if app.CurrentProxySelection() != (proxyservice.Selection{}) {
		t.Fatalf("failed save exposed selection %#v", app.CurrentProxySelection())
	}

	store.saveErr = nil
	if _, err := app.ProbeProxy(7897); err != nil {
		t.Fatal(err)
	}
	store.clearErr = secret
	if err := app.UseDirectConnection(); err == nil {
		t.Fatal("UseDirectConnection() accepted failed persistent clear")
	}
	if app.CurrentProxySelection().Port != 7897 {
		t.Fatalf("failed clear discarded active selection %#v", app.CurrentProxySelection())
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

type fakeProxySelectionStore struct {
	loaded   proxyservice.Selection
	loadErr  error
	saved    []proxyservice.Selection
	saveErr  error
	clearErr error
	cleared  bool
}

func (store *fakeProxySelectionStore) Load() (proxyservice.Selection, error) {
	return store.loaded, store.loadErr
}

func (store *fakeProxySelectionStore) Save(selection proxyservice.Selection) error {
	store.saved = append(store.saved, selection)
	return store.saveErr
}

func (store *fakeProxySelectionStore) Clear() error {
	store.cleared = true
	return store.clearErr
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
	checkErr        error
	appliedPlan     string
	result          selfupdate.ApplyResult
	applyErr        error
}

func (updater *fakeAppUpdater) Check(_ context.Context, protocol proxyservice.Protocol, port int) (selfupdate.Info, error) {
	updater.checkedProtocol, updater.checkedPort = protocol, port
	if updater.checkErr != nil {
		return selfupdate.Info{}, updater.checkErr
	}
	return selfupdate.Info{Available: true, CanInstall: true, PlanID: "update-plan", CurrentVersion: "1.0.0", LatestVersion: "1.1.0"}, nil
}

func TestAppUpdateBridgeDistinguishesRateLimitFromNetworkFailure(t *testing.T) {
	app := newAppWithServices(fakeScanner{scan: func(context.Context) (domain.EnvironmentSnapshot, error) {
		return domain.EnvironmentSnapshot{}, nil
	}}, &fakeProxyProber{})
	updater := &fakeAppUpdater{checkErr: selfupdate.ErrRateLimited}
	app.updater = updater
	_, err := app.CheckForAppUpdate()
	var public *domain.PublicError
	if !errors.As(err, &public) || public.Message != "GitHub 更新检查频率受限，请稍后重试" {
		t.Fatalf("rate-limit error = %v", err)
	}
	updater.checkErr = selfupdate.ErrInvalidReply
	_, err = app.CheckForAppUpdate()
	if !errors.As(err, &public) || public.Message != "更新信息校验失败，请稍后重试" {
		t.Fatalf("metadata error = %v", err)
	}
}

func (updater *fakeAppUpdater) Apply(_ context.Context, planID string, protocol proxyservice.Protocol, port int) (selfupdate.ApplyResult, error) {
	updater.appliedPlan, updater.checkedProtocol, updater.checkedPort = planID, protocol, port
	return updater.result, updater.applyErr
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

func TestAppUpdateBridgeReportsAnotherUpdatingInstance(t *testing.T) {
	app := newAppWithServices(fakeScanner{scan: func(context.Context) (domain.EnvironmentSnapshot, error) {
		return domain.EnvironmentSnapshot{}, nil
	}}, &fakeProxyProber{})
	app.updater = &fakeAppUpdater{applyErr: selfupdate.ErrUpdateInProgress}
	_, err := app.StartAppUpdate("update-plan")
	var public *domain.PublicError
	if !errors.As(err, &public) {
		t.Fatalf("StartAppUpdate() error = %v, want public error", err)
	}
	if public.Code != domain.ErrUpdateInProgress {
		t.Errorf("error code = %q, want %q", public.Code, domain.ErrUpdateInProgress)
	}
	for _, text := range []string{"另一 Osverse 实例", "更新", "稍后重试"} {
		if !strings.Contains(public.Message, text) {
			t.Errorf("public message %q does not contain %q", public.Message, text)
		}
	}
}

func (scanner fakeScanner) Scan(ctx context.Context) (domain.EnvironmentSnapshot, error) {
	return scanner.scan(ctx)
}
