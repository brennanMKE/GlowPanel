package main

import (
	"fmt"
	"log"
	"strings"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

const brightnessDebounce = 180 * time.Millisecond

type panelUI struct {
	backend *App
	window  fyne.Window // owner for the About popup

	brightness      *glowSlider
	brightnessLabel *canvas.Text
	mixedNote       *canvas.Text
	power           *powerToggle
	connection      *connectionDot
	errorLabel      *widget.Label
	errorBox        *fyne.Container
	devices         *fyne.Container
	themeButtons    map[string]*themeButton
	deviceSignature string
	activeTheme     string

	brightnessTimer *time.Timer
	holdUntil       time.Time
	suppressActions bool
	closed          atomic.Bool
}

func main() {
	fyneApp := app.NewWithID("com.brennanmke.glowpanel")
	fyneApp.Settings().SetTheme(&glowTheme{})

	window := fyneApp.NewWindow("Glow Panel")
	window.Resize(fyne.NewSize(940, 660))
	// The panel supplies its own even inset on all four sides; Fyne's window
	// padding would add an uneven extra margin on top of it and stop the header
	// rule from reaching the window edges.
	window.SetPadded(false)

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
	ui.window = window
	window.SetContent(ui.content())
	window.SetOnClosed(ui.shutdown)

	fyneApp.Lifecycle().SetOnEnteredForeground(backend.RefreshStatus)
	go backend.Start()
	window.ShowAndRun()
	backend.Shutdown()
}

func newPanelUI() *panelUI {
	u := &panelUI{
		themeButtons: make(map[string]*themeButton, len(themes)),
	}

	u.brightness = newGlowSlider(0, 100)
	u.brightness.Step = 5
	u.brightness.Value = 50
	u.brightnessLabel = newText("50%", 40, true, colorText)
	u.mixedNote = newText("mixed", 12, true, colorMuted)
	u.mixedNote.Hide()
	u.power = newPowerToggle(nil)
	u.connection = newConnectionDot()
	u.errorLabel = widget.NewLabel("")
	u.errorLabel.Wrapping = fyne.TextWrapWord
	u.errorLabel.Hide()
	u.devices = container.New(&trackingLayout{spacing: 12})

	u.brightness.OnChanged = func(value float64) {
		if u.suppressActions || u.backend == nil {
			return
		}
		percent := int(value)
		u.setBrightnessText(percent)
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
	return container.NewBorder(u.header(), u.footer(), nil, nil, u.body())
}

// header keeps the title alone on the left and gathers the two controls on the
// right, so the eye reads "what this is" and "what it is doing" as two separate
// things rather than one crowded run.
func (u *panelUI) header() fyne.CanvasObject {
	title := newText("Glow Panel", 24, true, colorText)

	about := newCircleButton("i", 15, func() {
		if u.window != nil {
			showAbout(u.window)
		}
	})

	controls := container.New(&trackingLayout{spacing: 26},
		u.power, u.connection.view, container.NewCenter(about))

	row := container.NewBorder(nil, nil, container.NewCenter(title), controls)

	rule := canvas.NewRectangle(colorHairline)
	rule.SetMinSize(fyne.NewSize(1, 1))

	return container.NewBorder(nil, rule, nil, nil,
		inset(row, &insetLayout{top: 16, bottom: 16, left: outerInset, right: outerInset}))
}

func (u *panelUI) body() fyne.CanvasObject {
	tiles := make([]fyne.CanvasObject, 0, len(themes))
	for _, item := range themes {
		item := item
		button := newThemeButton(item, func() { u.selectTheme(item.ID) })
		u.themeButtons[item.ID] = button
		tiles = append(tiles, button)
	}
	themeCard := newCard("PICK A LOOK",
		container.New(&tileGrid{columns: 3, gutter: tileGutter}, tiles...))

	// The error line is wrapped in its own box that is hidden as a whole. Hiding
	// only the label inside it would leave the border layout still reserving a
	// band of empty space for a message nobody is being shown.
	u.errorBox = inset(u.errorLabel, &insetLayout{bottom: 12})
	u.errorBox.Hide()

	// The theme card takes the slack so the window has no dead band in it: the
	// brightness panel and the device chips stay pinned under it whatever the
	// window height is.
	stack := container.NewBorder(
		u.errorBox,
		inset(u.brightnessCard(), &insetLayout{top: 16}),
		nil, nil, themeCard)

	return inset(stack, &insetLayout{top: outerInset, left: outerInset, right: outerInset})
}

func (u *panelUI) brightnessCard() fyne.CanvasObject {
	heading := container.NewBorder(nil, nil,
		container.NewHBox(
			container.NewCenter(newCaption("BRIGHTNESS")),
			container.NewCenter(u.mixedNote)),
		container.NewCenter(u.brightnessLabel))

	minus := newCircleButton("−", 30, func() { u.nudgeBrightness(-5) })
	plus := newCircleButton("+", 30, func() { u.nudgeBrightness(5) })
	row := container.NewBorder(nil, nil,
		container.NewCenter(minus), container.NewCenter(plus),
		inset(u.brightness, &insetLayout{left: 12, right: 12}))

	return newCard("", container.NewBorder(inset(heading, &insetLayout{bottom: 6}), nil, nil, nil, row))
}

func (u *panelUI) footer() fyne.CanvasObject {
	return inset(withMinHeight(u.devices, chipHeight),
		&insetLayout{top: 14, bottom: outerInset, left: outerInset, right: outerInset})
}

func (u *panelUI) setBrightnessText(percent int) {
	u.brightnessLabel.Text = fmt.Sprintf("%d%%", percent)
	u.brightnessLabel.Refresh()
}

func (u *panelUI) nudgeBrightness(delta float64) {
	u.brightness.SetValue(u.brightness.Value + delta)
}

func (u *panelUI) selectTheme(id string) {
	if u.backend == nil {
		return
	}
	u.setActiveTheme(id)
	u.suppressActions = true
	u.power.SetOn(true)
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
		if u.errorBox != nil {
			u.errorBox.Hide()
		}
		return
	}
	u.errorLabel.SetText(message)
	u.errorLabel.Show()
	if u.errorBox != nil {
		u.errorBox.Show()
	}
}

func (u *panelUI) applyStatus(status Status) {
	u.connection.set(status.Connected)
	if status.Error != "" {
		u.showError(status.Error)
	}
	u.renderDevices(status.Devices)

	if time.Now().Before(u.holdUntil) {
		return
	}

	// One number cannot honestly stand for several strips at different levels.
	// The slider still sets everything at once, but when the strips disagree the
	// readout is dimmed and flagged rather than quietly showing one of them.
	u.setMixed(status.Mixed)

	u.suppressActions = true
	u.power.SetOn(status.AnyOn)
	if status.HasPercent {
		u.brightness.SetValue(float64(status.Percent))
		u.setBrightnessText(status.Percent)
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

func (u *panelUI) setMixed(mixed bool) {
	u.brightness.SetMixed(mixed)
	if mixed {
		u.brightnessLabel.Color = colorMuted
		u.mixedNote.Show()
	} else {
		u.brightnessLabel.Color = colorText
		u.mixedNote.Hide()
	}
	u.brightnessLabel.Refresh()
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
		value, tone := "no reply", colorMuted
		switch {
		case device.LastSeenAgo < 0:
		case !device.Enabled:
			value, tone = "off", colorMuted
		default:
			value, tone = fmt.Sprintf("%d%%", device.Percent), colorOnline
		}
		objects = append(objects, newDeviceChip(device.Name, value, tone))
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
		button.SetActive(themeID == id)
	}
}

// tileGrid lays the six looks out in even rows and columns that stretch to fill
// whatever room the card has. Fyne's grid uses the theme padding as its gutter,
// which left the tiles all but touching.
type tileGrid struct {
	columns int
	gutter  float32
}

func (g *tileGrid) rows(count int) int {
	if g.columns < 1 {
		return count
	}
	return (count + g.columns - 1) / g.columns
}

func (g *tileGrid) MinSize(objects []fyne.CanvasObject) fyne.Size {
	var cell fyne.Size
	for _, o := range objects {
		cell = cell.Max(o.MinSize())
	}
	rows := g.rows(len(objects))
	return fyne.NewSize(
		cell.Width*float32(g.columns)+g.gutter*float32(g.columns-1),
		cell.Height*float32(rows)+g.gutter*float32(rows-1))
}

func (g *tileGrid) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	rows := g.rows(len(objects))
	if rows == 0 || g.columns < 1 {
		return
	}
	cellWidth := (size.Width - g.gutter*float32(g.columns-1)) / float32(g.columns)
	cellHeight := (size.Height - g.gutter*float32(rows-1)) / float32(rows)
	for index, o := range objects {
		column, row := index%g.columns, index/g.columns
		o.Resize(fyne.NewSize(cellWidth, cellHeight))
		o.Move(fyne.NewPos(
			float32(column)*(cellWidth+g.gutter),
			float32(row)*(cellHeight+g.gutter)))
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
