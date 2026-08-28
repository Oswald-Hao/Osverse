package geminiinstall

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Oswald-Hao/Osverse/internal/install"
	proxyservice "github.com/Oswald-Hao/Osverse/internal/proxy"
)

func TestManagerPlanAndTaskLifecycle(t *testing.T) {
	manager := newTestManager(t)
	if _, err := manager.CreatePlan(context.Background(), "codex-cli"); !errors.Is(err, install.ErrUnknownComponent) {
		t.Fatalf("unknown component error = %v", err)
	}
	plan, err := manager.CreatePlan(context.Background(), componentID)
	if err != nil {
		t.Fatal(err)
	}
	if plan.ComponentID != componentID || plan.Command != commandName || plan.Version != geminiVersion || plan.DownloadBytes != 77_569_489 || len(plan.Changes) != 3 {
		t.Fatalf("plan = %#v", plan)
	}
	manager.executeFn = func(_ context.Context, _ storedPlan, _ proxyservice.Protocol, _ int, progress func(string, int, string)) error {
		progress("downloading", 20, "download")
		progress("verifying", 90, "verify")
		return nil
	}
	task, err := manager.Start(context.Background(), plan.ID, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	completed := waitTask(t, manager, task.ID, "completed")
	if completed.Progress != 100 || completed.ErrorCode != "" || completed.FinishedAt.IsZero() {
		t.Fatalf("completed task = %#v", completed)
	}
	if _, err := manager.Start(context.Background(), plan.ID, "", 0); !errors.Is(err, install.ErrPlanUnavailable) {
		t.Fatalf("reused plan error = %v", err)
	}
}

func TestManagerCancellationReleasesActiveComponent(t *testing.T) {
	manager := newTestManager(t)
	started := make(chan struct{})
	manager.executeFn = func(ctx context.Context, _ storedPlan, _ proxyservice.Protocol, _ int, _ func(string, int, string)) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}
	first, _ := manager.CreatePlan(context.Background(), componentID)
	second, _ := manager.CreatePlan(context.Background(), componentID)
	task, err := manager.Start(context.Background(), first.ID, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	<-started
	if _, err := manager.Start(context.Background(), second.ID, "", 0); !errors.Is(err, install.ErrInstallActive) {
		t.Fatalf("concurrent start error = %v", err)
	}
	if err := manager.Cancel(task.ID); err != nil {
		t.Fatal(err)
	}
	canceled := waitTask(t, manager, task.ID, "canceled")
	if canceled.ErrorCode != "INSTALL_CANCELED" {
		t.Fatalf("canceled task = %#v", canceled)
	}
	manager.executeFn = func(context.Context, storedPlan, proxyservice.Protocol, int, func(string, int, string)) error {
		return nil
	}
	if _, err := manager.Start(context.Background(), second.ID, "", 0); err != nil {
		t.Fatalf("unused rejected plan did not remain usable: %v", err)
	}
}

func newTestManager(t *testing.T) *Manager {
	t.Helper()
	runtimeItem, pack, err := artifactsForTarget("linux", "amd64")
	if err != nil {
		t.Fatal(err)
	}
	_ = runtimeItem
	_ = pack
	sequence := 0
	manager := &Manager{
		home: t.TempDir(), goos: "linux", goarch: "amd64", now: time.Now,
		randomID: func() (string, error) { sequence++; return "gemini-test-" + itoa(sequence), nil },
		client:   proxyservice.NewHTTPClient, plans: make(map[string]*storedPlan), tasks: make(map[string]*taskState), active: make(map[string]string),
	}
	manager.executeFn = manager.execute
	return manager
}

func waitTask(t *testing.T, manager *Manager, id, phase string) install.Task {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		task, err := manager.Task(id)
		if err != nil {
			t.Fatal(err)
		}
		if task.Phase == phase {
			return task
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("task %s did not reach %s", id, phase)
	return install.Task{}
}

func itoa(value int) string {
	if value < 10 {
		return string(rune('0' + value))
	}
	return "many"
}
