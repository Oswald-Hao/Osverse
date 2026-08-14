//go:build windows

package main

import (
	"errors"

	"github.com/Oswald-Hao/Osverse/internal/install"
	launchservice "github.com/Oswald-Hao/Osverse/internal/launch"
	platformwindows "github.com/Oswald-Hao/Osverse/internal/platform/windows"
	"github.com/Oswald-Hao/Osverse/internal/windowsapps"
	"github.com/Oswald-Hao/Osverse/internal/windowsinstall"
	"github.com/Oswald-Hao/Osverse/internal/windowsremoval"
)

func configurePlatformServices(app *App, home string) {
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
