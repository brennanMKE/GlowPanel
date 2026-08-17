package main

import (
	"fmt"
	"runtime"
	"runtime/debug"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// The About view answers one question: which build am I looking at? Two panels
// that draw the same controls are easy to confuse, so the toolkit is read out
// of the module graph rather than written down here - a binary cannot claim to
// be something it was not linked against.
const (
	toolkitFyne  = "fyne.io/fyne/v2"
	toolkitWails = "github.com/wailsapp/wails/v2"
)

type aboutRow struct {
	Label string
	Value string
}

// uiToolkit reports the UI library this binary was actually linked against.
func uiToolkit(info *debug.BuildInfo) (string, string) {
	if info == nil {
		return "unknown", ""
	}
	for _, dep := range info.Deps {
		switch dep.Path {
		case toolkitFyne:
			return "Fyne", dep.Version
		case toolkitWails:
			return "Wails", dep.Version
		}
	}
	return "unknown", ""
}

func buildSetting(info *debug.BuildInfo, key string) string {
	if info == nil {
		return ""
	}
	for _, setting := range info.Settings {
		if setting.Key == key {
			return setting.Value
		}
	}
	return ""
}

// aboutRows builds the display rows. Split out from the dialog so the values
// can be asserted without standing up a window.
func aboutRows(meta fyne.AppMetadata, info *debug.BuildInfo) []aboutRow {
	version := meta.Version
	if version == "" {
		version = "dev"
	}
	if meta.Build > 0 {
		version = fmt.Sprintf("%s (build %d)", version, meta.Build)
	}

	toolkit, toolkitVersion := uiToolkit(info)
	if toolkitVersion != "" {
		toolkit = fmt.Sprintf("%s %s", toolkit, toolkitVersion)
	}

	rows := []aboutRow{
		{Label: "Version", Value: version},
		{Label: "Built with", Value: toolkit},
	}

	if revision := buildSetting(info, "vcs.revision"); revision != "" {
		short := revision
		if len(short) > 7 {
			short = short[:7]
		}
		if buildSetting(info, "vcs.modified") == "true" {
			short += " (modified)"
		}
		rows = append(rows, aboutRow{Label: "Revision", Value: short})
	}

	rows = append(rows, aboutRow{
		Label: "Platform",
		Value: fmt.Sprintf("%s/%s, %s", runtime.GOOS, runtime.GOARCH, runtime.Version()),
	})

	return rows
}

func aboutContent(rows []aboutRow) fyne.CanvasObject {
	labels := make([]fyne.CanvasObject, 0, len(rows))
	values := make([]fyne.CanvasObject, 0, len(rows))
	for _, row := range rows {
		labels = append(labels, newText(row.Label, 14, false, colorMuted))
		values = append(values, newText(row.Value, 14, true, colorText))
	}

	return container.NewHBox(
		container.NewVBox(labels...),
		inset(container.NewVBox(values...), &insetLayout{left: 18}),
	)
}

// showAbout uses a modal popup rather than the dialog package: dialog drags in
// Fyne's file browser and a module that is not in go.sum, and a popup renders
// the same on the Pi, where a second window under labwc comes up undecorated.
func showAbout(window fyne.Window) {
	info, _ := debug.ReadBuildInfo()
	rows := aboutRows(fyne.CurrentApp().Metadata(), info)

	var popup *widget.PopUp
	dismiss := widget.NewButton("Close", func() {
		if popup != nil {
			popup.Hide()
		}
	})

	body := container.NewVBox(
		newText("About", 22, true, colorText),
		inset(aboutContent(rows), &insetLayout{top: 14, bottom: 16}),
		container.NewCenter(dismiss),
	)

	popup = widget.NewModalPopUp(
		inset(body, &insetLayout{top: 22, bottom: 22, left: 26, right: 26}),
		window.Canvas())
	popup.Show()
}
