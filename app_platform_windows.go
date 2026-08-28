//go:build windows

package main

import (
	"errors"

	"github.com/Oswald-Hao/Osverse/internal/copilotinstall"
	"github.com/Oswald-Hao/Osverse/internal/geminiinstall"
	"github.com/Oswald-Hao/Osverse/internal/harnessinstall"
	"github.com/Oswald-Hao/Osverse/internal/install"
	"github.com/Oswald-Hao/Osverse/internal/kimiinstall"
	launchservice "github.com/Oswald-Hao/Osverse/internal/launch"
	platformwindows "github.com/Oswald-Hao/Osverse/internal/platform/windows"
	"github.com/Oswald-Hao/Osverse/internal/qweninstall"
	"github.com/Oswald-Hao/Osverse/internal/windowsapps"
	"github.com/Oswald-Hao/Osverse/internal/windowsinstall"
	"github.com/Oswald-Hao/Osverse/internal/windowsremoval"
)

func configurePlatformServices(app *App, home string) {
	if manager, err := harnessinstall.NewManager(home); err == nil {
		app.harnessPlanner = manager
		app.harnessExecutor = manager
	}
	if manager, err := qweninstall.NewManager(home); err == nil {
		app.qwenPlanner = manager
		app.qwenExecutor = manager
	}
	if manager, err := kimiinstall.NewManager(home); err == nil {
		app.kimiPlanner = manager
		app.kimiExecutor = manager
	}
	if manager, err := copilotinstall.NewManager(home); err == nil {
		app.copilotPlanner = manager
		app.copilotExecutor = manager
	}
	if manager, err := geminiinstall.NewManager(home); err == nil {
		app.geminiPlanner = manager
		app.geminiExecutor = manager
	}
	if manager, err := windowsinstall.NewManager(home); err == nil {
		app.installPlanner = manager
		app.installExecutor = manager
	}
	if manager, err := windowsapps.NewManager(home); err == nil {
		app.appPlanner = manager
		app.appExecutor = manager
	}
	app.removal, _ = windowsremoval.NewManager(home)
	app.componentLauncher = launchservice.NewManager(platformwindows.NewDetachedStarter(), nil)
}

func isUnsupportedInstallError(err error) bool {
	return errors.Is(err, install.ErrUnknownComponent) || errors.Is(err, install.ErrUnsupportedTarget)
}
