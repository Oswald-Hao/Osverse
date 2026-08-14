package main

import (
	"embed"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var assets embed.FS

const (
	defaultWindowWidth  = 1280
	defaultWindowHeight = 800
)

func main() {
	if exitCode, privileged := privilegedInvocation(); privileged {
		platformExit(exitCode)
	}
	app := NewApp(platformScanner())

	err := wails.Run(&options.App{
		Title:  "Osverse",
		Width:  defaultWindowWidth,
		Height: defaultWindowHeight,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		OnStartup: app.startup,
		Bind: []interface{}{
			app,
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}
}
