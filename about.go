package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"runtime"
	"runtime/debug"
)

// The About view answers one question: which build am I looking at? The Fyne
// port draws the same controls as this one, and during that evaluation the two
// were mistaken for each other more than once. So the toolkit is read out of
// the module graph rather than written down here - a binary cannot claim to be
// something it was not linked against.
const (
	toolkitWails = "github.com/wailsapp/wails/v2"
	toolkitFyne  = "fyne.io/fyne/v2"
)

// wailsConfig is embedded so the version has one source of truth. Editing
// productVersion in wails.json changes the bundle metadata and this view
// together, rather than leaving a constant here to drift out of step.
//
//go:embed wails.json
var wailsConfigJSON []byte

type wailsConfig struct {
	Info struct {
		ProductVersion string `json:"productVersion"`
	} `json:"info"`
}

// AboutRow is one label/value line in the dialog.
type AboutRow struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

func configuredVersion(raw []byte) string {
	var cfg wailsConfig
	if err := json.Unmarshal(raw, &cfg); err != nil || cfg.Info.ProductVersion == "" {
		return "dev"
	}
	return cfg.Info.ProductVersion
}

// uiToolkit reports the UI library this binary was actually linked against.
func uiToolkit(info *debug.BuildInfo) (string, string) {
	if info == nil {
		return "unknown", ""
	}
	for _, dep := range info.Deps {
		switch dep.Path {
		case toolkitWails:
			return "Wails", dep.Version
		case toolkitFyne:
			return "Fyne", dep.Version
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

// aboutRows builds the display rows. Split out from the binding so the values
// can be asserted without standing up a window.
func aboutRows(version string, info *debug.BuildInfo) []AboutRow {
	toolkit, toolkitVersion := uiToolkit(info)
	if toolkitVersion != "" {
		toolkit = fmt.Sprintf("%s %s", toolkit, toolkitVersion)
	}

	rows := []AboutRow{
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
		rows = append(rows, AboutRow{Label: "Revision", Value: short})
	}

	rows = append(rows, AboutRow{
		Label: "Platform",
		Value: fmt.Sprintf("%s/%s, %s", runtime.GOOS, runtime.GOARCH, runtime.Version()),
	})

	return rows
}

// GetAbout is bound to the frontend.
func (a *App) GetAbout() []AboutRow {
	info, _ := debug.ReadBuildInfo()
	return aboutRows(configuredVersion(wailsConfigJSON), info)
}
