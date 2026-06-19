package main

import (
	"embed"
	"os"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/linux"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

//go:embed all:dist
var assets embed.FS

func main() {
	app := NewApp()

	err := wails.Run(&options.App{
		Title:            "Phaze",
		Width:            1280,
		Height:           800,
		MinWidth:         900,
		MinHeight:        600,
		AssetServer:      &assetserver.Options{Assets: assets},
		Frameless:        true,
		BackgroundColour: &options.RGBA{R: 13, G: 13, B: 13, A: 255},
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		OnBeforeClose:    app.beforeClose,
		Bind:             []interface{}{app},
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId: "world.phazechat.desktop",
			OnSecondInstanceLaunch: func(data options.SecondInstanceData) {
				app.showWindow()
			},
		},
		Windows: &windows.Options{
			WebviewIsTransparent:              false,
			WindowIsTranslucent:               false,
			DisableWindowIcon:                 false,
			IsZoomControlEnabled:              false,
			EnableSwipeGestures:               false,
			Theme:                             windows.Dark,
		},
		Linux: &linux.Options{
			ProgramName: "Phaze",
		},
		// Restore window position between launches
		StartHidden: false,
	})

	if err != nil {
		os.Stderr.WriteString("Error: " + err.Error() + "\n")
		os.Exit(1)
	}
}
