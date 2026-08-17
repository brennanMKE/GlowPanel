package main

import (
	"runtime/debug"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
)

func rowValue(rows []aboutRow, label string) string {
	for _, row := range rows {
		if row.Label == label {
			return row.Value
		}
	}
	return ""
}

// The whole point of the About view is telling two builds that draw the same
// controls apart, so the toolkit has to come from what was actually linked.
func TestAboutReportsLinkedToolkit(t *testing.T) {
	cases := []struct {
		name string
		dep  string
		want string
	}{
		{"fyne", toolkitFyne, "Fyne v2.8.0"},
		{"wails", toolkitWails, "Wails v2.8.0"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			info := &debug.BuildInfo{Deps: []*debug.Module{
				{Path: "github.com/eclipse/paho.mqtt.golang", Version: "v1.5.1"},
				{Path: tc.dep, Version: "v2.8.0"},
			}}
			got := rowValue(aboutRows(fyne.AppMetadata{Version: "0.1.0"}, info), "Built with")
			if got != tc.want {
				t.Fatalf("Built with = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestAboutHandlesMissingBuildInfo(t *testing.T) {
	rows := aboutRows(fyne.AppMetadata{}, nil)
	if got := rowValue(rows, "Version"); got != "dev" {
		t.Errorf("Version = %q, want dev for an unpackaged build", got)
	}
	if got := rowValue(rows, "Built with"); got != "unknown" {
		t.Errorf("Built with = %q, want unknown", got)
	}
	if rowValue(rows, "Platform") == "" {
		t.Error("Platform row is missing")
	}
}

func TestAboutShowsVersionBuildAndRevision(t *testing.T) {
	info := &debug.BuildInfo{
		Deps: []*debug.Module{{Path: toolkitFyne, Version: "v2.8.0"}},
		Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "92360e4abcdef1234567890"},
			{Key: "vcs.modified", Value: "true"},
		},
	}
	rows := aboutRows(fyne.AppMetadata{Version: "0.1.0", Build: 3}, info)

	if got := rowValue(rows, "Version"); got != "0.1.0 (build 3)" {
		t.Errorf("Version = %q", got)
	}
	revision := rowValue(rows, "Revision")
	if !strings.HasPrefix(revision, "92360e4") {
		t.Errorf("Revision = %q, want the short commit", revision)
	}
	if !strings.Contains(revision, "modified") {
		t.Errorf("Revision = %q, want a dirty-tree marker", revision)
	}
}
