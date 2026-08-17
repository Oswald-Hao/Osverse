//go:build !windows

package harnessinstall

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Oswald-Hao/Osverse/internal/install"
	proxyservice "github.com/Oswald-Hao/Osverse/internal/proxy"
)

func TestManagerPlansAndCompletesHarnessTask(t *testing.T) {
	home := t.TempDir()
	now := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	ids := []string{"plan", "task"}
	manager := &Manager{
		home: home, goos: "linux", goarch: "amd64", now: func() time.Time { return now },
		randomID: func() (string, error) { id := ids[0]; ids = ids[1:]; return id, nil },
		client:   func(proxyservice.Protocol, int) (*http.Client, error) { return &http.Client{}, nil },
		plans:    make(map[string]*storedPlan), tasks: make(map[string]*taskState), active: make(map[string]string),
	}
	finished := make(chan struct{})
	manager.executeFn = func(context.Context, storedPlan, proxyservice.Protocol, int, func(string, int, string)) error {
		close(finished)
		return nil
	}
	plan, err := manager.CreatePlan(context.Background(), componentID)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Command != "dsh" || plan.Version != harnessVer || plan.DownloadBytes <= 100_000_000 {
		t.Fatalf("unexpected plan %#v", plan)
	}
	task, err := manager.Start(context.Background(), plan.ID, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	<-finished
	deadline := time.Now().Add(time.Second)
	for {
		current, err := manager.Task(task.ID)
		if err != nil {
			t.Fatal(err)
		}
		if current.Phase == "completed" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("task did not complete: %#v", current)
		}
		time.Sleep(time.Millisecond)
	}
	if _, err := manager.Start(context.Background(), plan.ID, "", 0); !errors.Is(err, install.ErrPlanUnavailable) {
		t.Fatalf("reused plan error = %v", err)
	}
}

func TestUnixActivationCreatesOwnedCommandAndProfile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix activation")
	}
	t.Setenv("SHELL", "/bin/sh")
	home := t.TempDir()
	paths := managedPathsFor(home, "linux", harnessVer)
	for _, directory := range []string{paths.toolRoot, paths.finalRoot, paths.binRoot} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Dir(paths.wrapperPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.wrapperPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := activateHarnessCommand(home, paths, "linux"); err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(paths.shimPath)
	if err != nil || resolved != paths.wrapperPath {
		t.Fatalf("resolved=%q err=%v", resolved, err)
	}
	profile, err := os.ReadFile(filepath.Join(home, ".profile"))
	if err != nil || !strings.Contains(string(profile), "Osverse user commands") {
		t.Fatalf("profile=%q err=%v", profile, err)
	}
}
