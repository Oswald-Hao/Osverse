//go:build windows

package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Oswald-Hao/Osverse/internal/domain"
	"github.com/Oswald-Hao/Osverse/internal/windowsremoval"
)

func TestWindowsRemovalBridgeRecoversFreshScannedUserCommand(t *testing.T) {
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	entry := filepath.Join(home, "AppData", "Roaming", "npm", "dsh.cmd")
	if err := os.MkdirAll(filepath.Dir(entry), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(entry, []byte("@echo off\r\nnode dsh %*\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	component := domain.Component{
		ID: "deepseek-harness", Name: "DeepSeek Harness", Category: "Core CLI", Status: domain.StatusInstalled,
		Installations: []domain.Installation{{Path: entry, ResolvedPath: entry, Source: "path", Version: "0.1.0-rc.6"}},
	}
	app := newAppWithServices(fakeScanner{scan: func(context.Context) (domain.EnvironmentSnapshot, error) {
		return domain.EnvironmentSnapshot{Components: []domain.Component{component}}, nil
	}}, &fakeProxyProber{})
	manager, err := windowsremoval.NewManager(home)
	if err != nil {
		t.Fatal(err)
	}
	app.removal = manager

	plan, err := app.CreateRemovalPlan(component.ID)
	if err != nil {
		t.Fatalf("CreateRemovalPlan() = %v", err)
	}
	if len(plan.Effects) != 2 || plan.Effects[0].Path != entry || plan.Effects[0].Action != "recover" {
		t.Fatalf("removal plan = %#v", plan)
	}
	result, err := app.RemoveComponent(plan.ID)
	if err != nil || !result.Removed {
		t.Fatalf("RemoveComponent() = (%#v, %v)", result, err)
	}
	if _, err := os.Lstat(entry); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("freshly scanned command entry remains: %v", err)
	}
}
