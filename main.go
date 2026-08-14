package main

import (
	"embed"
	"os"

	"github.com/Oswald-Hao/Osverse/internal/bootstrap"
	"github.com/Oswald-Hao/Osverse/internal/systeminstall"
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
	if systeminstall.IsPrivilegedInvocation(os.Args[1:]) {
		os.Exit(systeminstall.RunPrivileged(os.Args[1:]))
	}
	app := NewApp(bootstrap.NewLinuxScanner())

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
