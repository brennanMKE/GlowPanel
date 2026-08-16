package main

import (
	"testing"
	"time"

	fynetest "fyne.io/fyne/v2/test"
)

func TestThemeMatches(t *testing.T) {
	tests := []struct {
		id       string
		reported string
		want     bool
	}{
		{id: "PINK_PONY", reported: "Pink Pony Club", want: true},
		{id: "OCEAN_WAVES", reported: "Ocean Waves", want: true},
		{id: "RAINBOW", reported: "rainbow", want: true},
		{id: "GREEN", reported: "Forest", want: false},
		{id: "", reported: "Forest", want: false},
	}

	for _, test := range tests {
		if got := themeMatches(test.id, test.reported); got != test.want {
			t.Errorf("themeMatches(%q, %q) = %v, want %v", test.id, test.reported, got, test.want)
		}
	}
}

func TestGetStatusUsesFirstReportingDeviceBrightness(t *testing.T) {
	cfg := &Config{Devices: []string{"first", "second"}}
	broker := NewBroker(cfg)
	broker.state["first"] = &DeviceState{Name: "first", Percent: 0}
	broker.state["second"] = &DeviceState{Name: "second", Percent: 80}
	broker.seen["first"] = time.Now()
	broker.seen["second"] = time.Now()

	application := &App{broker: broker}
	status := application.GetStatus()
	if got := status.Percent; got != 0 {
		t.Fatalf("GetStatus().Percent = %d, want first reporting device value 0", got)
	}
	if !status.HasPercent {
		t.Fatal("GetStatus().HasPercent = false, want true for a reporting device at 0%")
	}
}

func TestApplyStatusDisplaysZeroBrightness(t *testing.T) {
	application := fynetest.NewApp()
	defer application.Quit()
	ui := newPanelUI()
	ui.applyStatus(Status{HasPercent: true, Percent: 0})
	if got := ui.brightness.Value; got != 0 {
		t.Fatalf("brightness.Value = %v, want 0", got)
	}
	if got := ui.brightnessLabel.Text; got != "0%" {
		t.Fatalf("brightnessLabel.Text = %q, want %q", got, "0%")
	}
}
