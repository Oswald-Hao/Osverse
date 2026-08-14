//go:build windows

package main

import (
	"errors"

	"github.com/Oswald-Hao/Osverse/internal/install"
	launchservice "github.com/Oswald-Hao/Osverse/internal/launch"
	platformwindows "github.com/Oswald-Hao/Osverse/internal/platform/windows"
	"github.com/Oswald-Hao/Osverse/internal/windowsinstall"
)

func configurePlatformServices(app *App, home string) {
	if manager, err := windowsinstall.NewManager(home); err == nil {
		app.installPlanner = manager
		app.installExecutor = manager
	}
	app.componentLauncher = launchservice.NewManager(platformwindows.NewDetachedStarter(), nil)
}

func isUnsupportedInstallError(err error) bool {
	return errors.Is(err, install.ErrUnknownComponent) || errors.Is(err, install.ErrUnsupportedTarget)
}
