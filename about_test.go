package main

import (
	"runtime/debug"
	"strings"
	"testing"
)

func rowValue(rows []AboutRow, label string) string {
	for _, row := range rows {
		if row.Label == label {
			return row.Value
		}
	}
	return ""
}

// The point of the About view is telling this build apart from the Fyne port,
// which draws the same controls. The toolkit has to come from what was linked.
func TestAboutReportsLinkedToolkit(t *testing.T) {
	cases := []struct {
		name string
		dep  string
		want string
	}{
		{"wails", toolkitWails, "Wails v2.13.0"},
		{"fyne", toolkitFyne, "Fyne v2.13.0"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			info := &debug.BuildInfo{Deps: []*debug.Module{
				{Path: "github.com/eclipse/paho.mqtt.golang", Version: "v1.5.1"},
				{Path: tc.dep, Version: "v2.13.0"},
			}}
			if got := rowValue(aboutRows("0.1.0", info), "Built with"); got != tc.want {
				t.Fatalf("Built with = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestAboutHandlesMissingBuildInfo(t *testing.T) {
	rows := aboutRows("0.1.0", nil)
	if got := rowValue(rows, "Built with"); got != "unknown" {
		t.Errorf("Built with = %q, want unknown", got)
	}
	if rowValue(rows, "Platform") == "" {
		t.Error("Platform row is missing")
	}
}

func TestAboutShowsRevision(t *testing.T) {
	info := &debug.BuildInfo{
		Deps: []*debug.Module{{Path: toolkitWails, Version: "v2.13.0"}},
		Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "ef825a9abcdef1234567890"},
			{Key: "vcs.modified", Value: "true"},
		},
	}
	revision := rowValue(aboutRows("0.1.0", info), "Revision")
	if !strings.HasPrefix(revision, "ef825a9") {
		t.Errorf("Revision = %q, want the short commit", revision)
	}
	if !strings.Contains(revision, "modified") {
		t.Errorf("Revision = %q, want a dirty-tree marker", revision)
	}
}

// The version comes from wails.json so the dialog and the bundle metadata
// cannot drift apart.
func TestVersionComesFromWailsConfig(t *testing.T) {
	if got := configuredVersion(wailsConfigJSON); got != "0.1.0" {
		t.Errorf("configuredVersion(embedded) = %q, want 0.1.0", got)
	}
	if got := configuredVersion([]byte(`{"info":{"productVersion":"9.9.9"}}`)); got != "9.9.9" {
		t.Errorf("configuredVersion = %q, want 9.9.9", got)
	}
	if got := configuredVersion([]byte("not json")); got != "dev" {
		t.Errorf("configuredVersion(garbage) = %q, want dev", got)
	}
}
