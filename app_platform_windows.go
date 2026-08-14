//go:build windows

package main

import (
	"errors"

	"github.com/Oswald-Hao/Osverse/internal/install"
	launchservice "github.com/Oswald-Hao/Osverse/internal/launch"
	platformwindows "github.com/Oswald-Hao/Osverse/internal/platform/windows"
)

func configurePlatformServices(app *App, _ string) {
	app.componentLauncher = launchservice.NewManager(platformwindows.NewDetachedStarter(), nil)
}

func isUnsupportedInstallError(err error) bool {
	return errors.Is(err, install.ErrUnknownComponent) || errors.Is(err, install.ErrUnsupportedTarget)
}
