package main

import (
	"embed"
	"log"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/linux"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	app := NewApp()

	err := wails.Run(&options.App{
		Title:  "GlowPanel",
		Width:  900,
		Height: 620,
		// The Pi's attached display is 1280x720 or smaller in most setups, so
		// keep the minimum small enough to fit without clipping the buttons.
		MinWidth:  640,
		MinHeight: 480,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 18, G: 16, B: 28, A: 1},
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		Bind: []interface{}{
			app,
		},
		Linux: &linux.Options{
			// WebviewGpuPolicy: the Pi 3B's VideoCore IV offers no useful
			// acceleration to WebKitGTK, and asking for it causes flicker and
			// occasional blank surfaces under labwc. Software rendering is both
			// faster and more stable here.
			WebviewGpuPolicy: linux.WebviewGpuPolicyNever,
			ProgramName:      "GlowPanel",
		},
	})
	if err != nil {
		log.Fatal(err)
	}
}
