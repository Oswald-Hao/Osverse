package kimiinstall

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/Oswald-Hao/Osverse/internal/install"
	proxyservice "github.com/Oswald-Hao/Osverse/internal/proxy"
)

func TestManagerCompletesAndConsumesSingleUsePlan(t *testing.T) {
	now := time.Date(2026, 8, 17, 14, 0, 0, 0, time.UTC)
	ids := []string{"plan", "task"}
	manager := newTestManager(t, now, &ids)
	manager.executeFn = func(context.Context, storedPlan, proxyservice.Protocol, int, func(string, int, string)) error {
		return nil
	}
	plan, err := manager.CreatePlan(context.Background(), componentID)
	if err != nil {
		t.Fatal(err)
	}
	if plan.ComponentID != componentID || plan.Command != "kimi" || plan.Version != kimiVersion {
		t.Fatalf("plan = %#v", plan)
	}
	task, err := manager.Start(context.Background(), plan.ID, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	waitForPhase(t, manager, task.ID, "completed")
	if _, err := manager.Start(context.Background(), plan.ID, "", 0); !errors.Is(err, install.ErrPlanUnavailable) {
		t.Fatalf("reused plan error = %v", err)
	}
}

func TestManagerCancellationReleasesActiveSlot(t *testing.T) {
	now := time.Date(2026, 8, 17, 14, 0, 0, 0, time.UTC)
	ids := []string{"plan-one", "task-one", "plan-two", "task-two"}
	manager := newTestManager(t, now, &ids)
	manager.executeFn = func(ctx context.Context, _ storedPlan, _ proxyservice.Protocol, _ int, _ func(string, int, string)) error {
		<-ctx.Done()
		return ctx.Err()
	}
	plan, _ := manager.CreatePlan(context.Background(), componentID)
	task, err := manager.Start(context.Background(), plan.ID, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Cancel(task.ID); err != nil {
		t.Fatal(err)
	}
	waitForPhase(t, manager, task.ID, "canceled")
	manager.executeFn = func(context.Context, storedPlan, proxyservice.Protocol, int, func(string, int, string)) error {
		return nil
	}
	secondPlan, _ := manager.CreatePlan(context.Background(), componentID)
	second, err := manager.Start(context.Background(), secondPlan.ID, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	waitForPhase(t, manager, second.ID, "completed")
}

func newTestManager(t *testing.T, now time.Time, ids *[]string) *Manager {
	t.Helper()
	return &Manager{
		home: t.TempDir(), goos: "linux", goarch: "amd64", now: func() time.Time { return now },
		randomID: func() (string, error) {
			if len(*ids) == 0 {
				return "", errors.New("no test ids")
			}
			id := (*ids)[0]
			*ids = (*ids)[1:]
			return id, nil
		},
		client: func(proxyservice.Protocol, int) (*http.Client, error) { return &http.Client{}, nil },
		plans:  make(map[string]*storedPlan), tasks: make(map[string]*taskState), active: make(map[string]string),
	}
}

func waitForPhase(t *testing.T, manager *Manager, taskID, phase string) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		task, err := manager.Task(taskID)
		if err != nil {
			t.Fatal(err)
		}
		if task.Phase == phase {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("task = %#v, want phase %q", task, phase)
		}
		time.Sleep(time.Millisecond)
	}
}
