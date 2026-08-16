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
		Title: "Glow Panel",
		Width: 900,
		// The layout needs about 600px for everything to be visible at once.
		// 660 leaves a little headroom for font differences between WebKitGTK
		// and WKWebView, and still fits a 1280x720 Pi display once the titlebar
		// is accounted for.
		Height: 660,
		// The Pi's attached display is 1280x720 or smaller in most setups, so
		// keep the minimum small enough to fit. Below the layout's natural
		// height the header and the status chips stay put and the middle
		// scrolls, so nothing becomes unreachable.
		MinWidth:  640,
		MinHeight: 420,
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
