package main

import (
	"testing"
	"time"

	"fyne.io/fyne/v2"
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

func TestThemesHaveVibrantPalettes(t *testing.T) {
	if len(themes) != 6 {
		t.Fatalf("len(themes) = %d, want 6", len(themes))
	}
	for _, item := range themes {
		if len(item.Colors) < 2 {
			t.Errorf("theme %q has %d colors, want at least 2", item.ID, len(item.Colors))
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

func TestEmojiAssetsCoverEveryTheme(t *testing.T) {
	for _, item := range themes {
		if emojiResource(item.Emoji) == nil {
			t.Errorf("theme %q has no bundled bitmap for %q; it would fall back to the "+
				"text font, which does not render every emoji", item.ID, item.Emoji)
		}
	}
}

// The slider fill is a raster, and Fyne allocates its backing image from the
// type of the colour returned for pixel 0,0. Pixel 0,0 is outside the rounded
// end of the track, so a bare color.Transparent there yields an alpha-only
// image and the purple gradient renders as a flat white bar.
func TestSliderFillKeepsItsColour(t *testing.T) {
	application := fynetest.NewApp()
	defer application.Quit()
	application.Settings().SetTheme(&glowTheme{})

	slider := newGlowSlider(0, 100)
	slider.Value = 100
	window := fynetest.NewWindow(slider)
	defer window.Close()
	window.SetPadded(false)
	window.Resize(fyne.NewSize(600, sliderTrackArea+sliderTickArea))

	image := window.Canvas().Capture()
	bounds := image.Bounds()
	red, green, blue, _ := image.At(bounds.Min.X+bounds.Dx()/2, bounds.Min.Y+bounds.Dy()*26/78).RGBA()
	if red>>8 > 0xf0 && green>>8 > 0xf0 && blue>>8 > 0xf0 {
		t.Fatalf("slider fill rendered white (%d,%d,%d), want the purple gradient",
			red>>8, green>>8, blue>>8)
	}
	if blue <= red {
		t.Fatalf("slider fill is not purple: r=%d b=%d", red>>8, blue>>8)
	}
}

func TestGetStatusFlagsMixedLevels(t *testing.T) {
	cfg := &Config{Devices: []string{"first", "second"}}
	broker := NewBroker(cfg)
	broker.state["first"] = &DeviceState{Name: "first", Percent: 100}
	broker.state["second"] = &DeviceState{Name: "second", Percent: 50}
	broker.seen["first"] = time.Now()
	broker.seen["second"] = time.Now()

	if status := (&App{broker: broker}).GetStatus(); !status.Mixed {
		t.Fatal("GetStatus().Mixed = false, want true when the strips report different levels")
	}

	broker.state["second"].Percent = 100
	if status := (&App{broker: broker}).GetStatus(); status.Mixed {
		t.Fatal("GetStatus().Mixed = true, want false when every strip reports the same level")
	}
}

func TestApplyStatusMarksMixedBrightness(t *testing.T) {
	application := fynetest.NewApp()
	defer application.Quit()
	ui := newPanelUI()

	ui.applyStatus(Status{HasPercent: true, Percent: 100, Mixed: true})
	if !ui.mixedNote.Visible() {
		t.Fatal("mixed note hidden, want it shown when the strips disagree")
	}
	if ui.brightnessLabel.Color == colorText {
		t.Fatal("brightness readout still at full strength, want the muted mixed treatment")
	}

	ui.applyStatus(Status{HasPercent: true, Percent: 100})
	if ui.mixedNote.Visible() {
		t.Fatal("mixed note shown, want it hidden when every strip agrees")
	}
}

func TestSliderRespondsToTapAndDrag(t *testing.T) {
	application := fynetest.NewApp()
	defer application.Quit()
	application.Settings().SetTheme(&glowTheme{})

	slider := newGlowSlider(0, 100)
	slider.Step = 5
	window := fynetest.NewWindow(slider)
	defer window.Close()
	window.SetPadded(false)
	window.Resize(fyne.NewSize(400, sliderTrackArea+sliderTickArea))

	slider.Tapped(&fyne.PointEvent{Position: fyne.NewPos(200, 26)})
	if slider.Value < 45 || slider.Value > 55 {
		t.Fatalf("tapping the middle of the track set %v, want roughly 50", slider.Value)
	}

	slider.Dragged(&fyne.DragEvent{PointEvent: fyne.PointEvent{Position: fyne.NewPos(400, 26)}})
	if slider.Value != 100 {
		t.Fatalf("dragging past the right end set %v, want 100", slider.Value)
	}
}
