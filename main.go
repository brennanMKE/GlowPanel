package main

import (
	"fmt"
	"image/color"
	"log"
	"strings"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

const brightnessDebounce = 180 * time.Millisecond

type panelUI struct {
	backend *App

	brightness      *widget.Slider
	brightnessLabel *widget.Label
	power           *widget.Check
	connection      *widget.Label
	errorLabel      *widget.Label
	devices         *fyne.Container
	themeButtons    map[string]*widget.Button
	deviceSignature string
	activeTheme     string

	brightnessTimer *time.Timer
	holdUntil       time.Time
	suppressActions bool
	closed          atomic.Bool
}

func main() {
	fyneApp := app.NewWithID("com.brennanmke.glowpanel")
	fyneApp.Settings().SetTheme(theme.DarkTheme())

	window := fyneApp.NewWindow("Glow Panel")
	window.Resize(fyne.NewSize(900, 660))

	ui := newPanelUI()
	backend := NewApp(func(status Status) {
		if ui.closed.Load() {
			return
		}
		// This callback can run on either a background goroutine or Fyne's main
		// goroutine. Keep this non-blocking: replacing Do with DoAndWait would
		// deadlock when a foreground callback originates on the main goroutine.
		fyne.Do(func() { ui.applyStatus(status) })
	})
	ui.backend = backend
	window.SetContent(ui.content())
	window.SetOnClosed(ui.shutdown)

	fyneApp.Lifecycle().SetOnEnteredForeground(backend.RefreshStatus)
	go backend.Start()
	window.ShowAndRun()
	backend.Shutdown()
}

func newPanelUI() *panelUI {
	u := &panelUI{
		themeButtons: make(map[string]*widget.Button, len(themes)),
	}

	u.brightness = widget.NewSlider(0, 100)
	u.brightness.Step = 5
	u.brightness.Value = 50
	u.brightnessLabel = widget.NewLabelWithStyle("50%", fyne.TextAlignTrailing, fyne.TextStyle{Bold: true})
	u.power = widget.NewCheck("On", nil)
	u.connection = widget.NewLabel("● reconnecting…")
	u.errorLabel = widget.NewLabel("")
	u.errorLabel.Wrapping = fyne.TextWrapWord
	u.errorLabel.Hide()
	u.devices = container.New(layout.NewRowWrapLayout())

	u.brightness.OnChanged = func(value float64) {
		if u.suppressActions || u.backend == nil {
			return
		}
		percent := int(value)
		u.brightnessLabel.SetText(fmt.Sprintf("%d%%", percent))
		u.holdUntil = time.Now().Add(2500 * time.Millisecond)
		if u.brightnessTimer != nil {
			u.brightnessTimer.Stop()
		}
		u.brightnessTimer = time.AfterFunc(brightnessDebounce, func() {
			u.runCommand(func() string { return u.backend.SetBrightness(percent) })
		})
	}

	u.power.OnChanged = func(on bool) {
		if u.suppressActions || u.backend == nil {
			return
		}
		u.holdUntil = time.Now().Add(1200 * time.Millisecond)
		u.runCommand(func() string { return u.backend.SetPower(on) })
	}

	return u
}

func (u *panelUI) content() fyne.CanvasObject {
	title := canvas.NewText("Glow Panel", color.White)
	title.TextSize = 22
	title.TextStyle = fyne.TextStyle{Bold: true}

	header := container.NewBorder(nil, nil, title, nil,
		container.NewHBox(u.power, u.connection))

	themeObjects := make([]fyne.CanvasObject, 0, len(themes))
	for _, item := range themes {
		item := item
		button := widget.NewButton(item.Emoji+"\n"+item.Label, func() {
			u.selectTheme(item.ID)
		})
		u.themeButtons[item.ID] = button
		themeObjects = append(themeObjects, button)
	}
	themeGrid := container.NewGridWrap(fyne.NewSize(250, 92), themeObjects...)
	themePanel := widget.NewCard("Pick a Look", "", themeGrid)

	minus := widget.NewButton("−", func() { u.nudgeBrightness(-5) })
	plus := widget.NewButton("+", func() { u.nudgeBrightness(5) })
	minus.Importance = widget.LowImportance
	plus.Importance = widget.LowImportance
	minusBox := container.NewGridWrap(fyne.NewSize(64, 64), minus)
	plusBox := container.NewGridWrap(fyne.NewSize(64, 64), plus)
	sliderRow := container.NewBorder(nil, nil, minusBox, plusBox, u.brightness)
	brightnessHeader := container.NewBorder(nil, nil,
		widget.NewLabelWithStyle("BRIGHTNESS", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		u.brightnessLabel, nil)
	brightnessPanel := widget.NewCard("", "", container.NewVBox(brightnessHeader, sliderRow))

	middle := container.NewVBox(u.errorLabel, themePanel, brightnessPanel)
	scroll := container.NewVScroll(middle)
	footer := widget.NewCard("", "", u.devices)

	return container.NewBorder(
		container.NewPadded(header),
		container.NewPadded(footer),
		nil,
		nil,
		container.NewPadded(scroll),
	)
}

func (u *panelUI) nudgeBrightness(delta float64) {
	next := u.brightness.Value + delta
	if next < u.brightness.Min {
		next = u.brightness.Min
	}
	if next > u.brightness.Max {
		next = u.brightness.Max
	}
	u.brightness.SetValue(next)
}

func (u *panelUI) selectTheme(id string) {
	if u.backend == nil {
		return
	}
	u.setActiveTheme(id)
	u.suppressActions = true
	u.power.SetChecked(true)
	u.suppressActions = false
	u.holdUntil = time.Now().Add(1200 * time.Millisecond)
	u.runCommand(func() string { return u.backend.SetTheme(id) })
}

func (u *panelUI) runCommand(command func() string) {
	if u.closed.Load() {
		return
	}
	go func() {
		errText := command()
		if u.closed.Load() {
			return
		}
		fyne.Do(func() { u.showError(errText) })
	}()
}

func (u *panelUI) shutdown() {
	u.closed.Store(true)
	if u.brightnessTimer != nil {
		u.brightnessTimer.Stop()
	}
}

func (u *panelUI) showError(message string) {
	if message == "" {
		u.errorLabel.Hide()
		return
	}
	u.errorLabel.SetText(message)
	u.errorLabel.Show()
}

func (u *panelUI) applyStatus(status Status) {
	if status.Connected {
		u.connection.SetText("● connected")
	} else {
		u.connection.SetText("● reconnecting…")
	}
	if status.Error != "" {
		u.showError(status.Error)
	}
	u.renderDevices(status.Devices)

	if time.Now().Before(u.holdUntil) {
		return
	}

	u.suppressActions = true
	u.power.SetChecked(status.AnyOn)
	if status.HasPercent {
		u.brightness.SetValue(float64(status.Percent))
		u.brightnessLabel.SetText(fmt.Sprintf("%d%%", status.Percent))
	}
	u.suppressActions = false

	for _, device := range status.Devices {
		if device.LastSeenAgo >= 0 && device.Theme != "" {
			for _, item := range themes {
				if themeMatches(item.ID, device.Theme) {
					u.setActiveTheme(item.ID)
					return
				}
			}
		}
	}
}

func (u *panelUI) renderDevices(devices []DeviceState) {
	var signature strings.Builder
	for _, device := range devices {
		fmt.Fprintf(&signature, "%s:%d:%t:%t;", device.Name, device.Percent, device.Enabled, device.LastSeenAgo < 0)
	}
	if signature.String() == u.deviceSignature {
		return
	}
	u.deviceSignature = signature.String()

	objects := make([]fyne.CanvasObject, 0, len(devices))
	for _, device := range devices {
		text := device.Name + " — no reply"
		if device.LastSeenAgo >= 0 {
			state := "off"
			if device.Enabled {
				state = fmt.Sprintf("%d%%", device.Percent)
			}
			text = device.Name + "  " + state
		}
		objects = append(objects, widget.NewLabel(text))
	}
	u.devices.Objects = objects
	u.devices.Refresh()
}

func (u *panelUI) setActiveTheme(id string) {
	if id == u.activeTheme {
		return
	}
	u.activeTheme = id
	for themeID, button := range u.themeButtons {
		button.Importance = widget.MediumImportance
		if themeID == id {
			button.Importance = widget.HighImportance
		}
		button.Refresh()
	}
}

func themeMatches(id, reported string) bool {
	normalize := func(value string) string {
		return strings.Map(func(r rune) rune {
			if r >= 'A' && r <= 'Z' {
				return r
			}
			if r >= 'a' && r <= 'z' {
				return r - ('a' - 'A')
			}
			return -1
		}, value)
	}
	a, b := normalize(id), normalize(reported)
	return a != "" && b != "" && (strings.HasPrefix(a, b) || strings.HasPrefix(b, a))
}

func init() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
}
