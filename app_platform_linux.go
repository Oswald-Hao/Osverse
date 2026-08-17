//go:build linux

package main

import (
	"context"
	"errors"

	appservice "github.com/Oswald-Hao/Osverse/internal/apps"
	"github.com/Oswald-Hao/Osverse/internal/harnessinstall"
	"github.com/Oswald-Hao/Osverse/internal/install"
	launchservice "github.com/Oswald-Hao/Osverse/internal/launch"
	platformlinux "github.com/Oswald-Hao/Osverse/internal/platform/linux"
	"github.com/Oswald-Hao/Osverse/internal/qweninstall"
	"github.com/Oswald-Hao/Osverse/internal/removal"
	"github.com/Oswald-Hao/Osverse/internal/systeminstall"
)

func isUnsupportedInstallError(err error) bool {
	return errors.Is(err, install.ErrUnknownComponent) || errors.Is(err, install.ErrUnsupportedTarget) ||
		errors.Is(err, appservice.ErrUnknownComponent) || errors.Is(err, appservice.ErrUnsupportedTarget) ||
		errors.Is(err, systeminstall.ErrUnknownComponent) || errors.Is(err, systeminstall.ErrUnsupportedTarget)
}

func configurePlatformServices(app *App, home string) {
	if manager, err := harnessinstall.NewManager(home); err == nil {
		app.harnessPlanner = manager
		app.harnessExecutor = manager
	}
	if manager, err := qweninstall.NewManager(home); err == nil {
		app.qwenPlanner = manager
		app.qwenExecutor = manager
	}
	if manager, err := install.NewManager(home); err == nil {
		app.installPlanner = manager
		app.installExecutor = manager
	}
	if appManager, err := appservice.NewManager(home); err == nil {
		app.appPlanner = appManager
		app.appExecutor = appManager
		app.appLauncher = appManager
	}
	var systemRemover interface {
		Remove(context.Context, string) error
	}
	if systemManager, err := systeminstall.NewManager(); err == nil {
		app.systemPlanner = systemManager
		app.systemExecutor = systemManager
		systemRemover = systemManager
	}
	app.componentLauncher = launchservice.NewManager(platformlinux.NewDetachedStarter(), app.appLauncher)
	app.removal, _ = removal.NewManager(home, systemRemover)
}
