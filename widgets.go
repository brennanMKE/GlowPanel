package main

import (
	"image/color"
	"math"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// ---------------------------------------------------------------------------
// shared drawing helpers

// roundedDistance is the signed distance from a point to the edge of a rounded
// rectangle, negative inside. The tiles and the slider fill are drawn as rasters
// because Fyne's gradients have no corner radius and it has no clipping, so the
// rounding - and the selection ring that has to sit exactly on that rounded edge
// rather than bleeding past it - has to happen in the same pass as the colour.
func roundedDistance(x, y, w, h, radius float64) float64 {
	if radius > w/2 {
		radius = w / 2
	}
	if radius > h/2 {
		radius = h / 2
	}
	dx := math.Abs(x-w/2) - (w/2 - radius)
	dy := math.Abs(y-h/2) - (h/2 - radius)
	if dx <= 0 && dy <= 0 {
		return math.Max(dx, dy) - radius
	}
	return math.Hypot(math.Max(dx, 0), math.Max(dy, 0)) - radius
}

// roundedCoverage turns that distance into antialiased 0..1 alpha.
func roundedCoverage(x, y, w, h, radius float64) float64 {
	return clamp01(0.5 - roundedDistance(x, y, w, h, radius))
}

// overNRGBA composites src onto dst, both straight alpha.
func overNRGBA(src, dst color.NRGBA) color.NRGBA {
	sa := float64(src.A) / 255
	da := float64(dst.A) / 255
	outA := sa + da*(1-sa)
	if outA <= 0 {
		return color.NRGBA{}
	}
	ch := func(s, d uint8) uint8 {
		return uint8((float64(s)*sa + float64(d)*da*(1-sa)) / outA)
	}
	return color.NRGBA{R: ch(src.R, dst.R), G: ch(src.G, dst.G), B: ch(src.B, dst.B), A: uint8(outA * 255)}
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func lerpColor(a, b color.NRGBA, t float64) color.NRGBA {
	t = clamp01(t)
	mix := func(x, y uint8) uint8 { return uint8(float64(x) + (float64(y)-float64(x))*t) }
	return color.NRGBA{R: mix(a.R, b.R), G: mix(a.G, b.G), B: mix(a.B, b.B), A: mix(a.A, b.A)}
}

// centerIn positions a canvas object at its own minimum size, centred on a point.
func centerIn(o fyne.CanvasObject, cx, cy float32) {
	size := o.MinSize()
	o.Resize(size)
	o.Move(fyne.NewPos(cx-size.Width/2, cy-size.Height/2))
}

func newText(content string, size float32, bold bool, c color.Color) *canvas.Text {
	t := canvas.NewText(content, c)
	t.TextSize = size
	t.TextStyle = fyne.TextStyle{Bold: bold}
	return t
}

// ---------------------------------------------------------------------------
// layouts

// insetLayout pads a single child by an explicit amount per edge. Fyne's padded
// container only offers one uniform theme-sized inset, which is not enough to
// give the cards the breathing room the design needs.
type insetLayout struct{ top, right, bottom, left float32 }

func uniformInset(v float32) *insetLayout {
	return &insetLayout{top: v, right: v, bottom: v, left: v}
}

func (l *insetLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	var size fyne.Size
	for _, o := range objects {
		size = size.Max(o.MinSize())
	}
	return fyne.NewSize(size.Width+l.left+l.right, size.Height+l.top+l.bottom)
}

func (l *insetLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	inner := fyne.NewSize(size.Width-l.left-l.right, size.Height-l.top-l.bottom)
	for _, o := range objects {
		o.Move(fyne.NewPos(l.left, l.top))
		o.Resize(inner)
	}
}

func inset(child fyne.CanvasObject, l *insetLayout) *fyne.Container {
	return container.New(l, child)
}

// withMinHeight reserves vertical room for content that is empty when the window
// is first laid out. The device chips only arrive once the strips report in, and
// without a floor the row would collapse and the panel would jump when the first
// message lands.
func withMinHeight(child fyne.CanvasObject, height float32) *fyne.Container {
	spacer := canvas.NewRectangle(color.Transparent)
	spacer.SetMinSize(fyne.NewSize(1, height))
	return container.NewStack(spacer, child)
}

// trackingLayout lays out one canvas object per character so headings can carry
// letter spacing. Fyne has no tracking control on canvas.Text, and the small
// uppercase headings look cramped and unlabelled without it.
type trackingLayout struct{ spacing float32 }

func (l *trackingLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	var size fyne.Size
	for i, o := range objects {
		child := o.MinSize()
		size.Width += child.Width
		if i > 0 {
			size.Width += l.spacing
		}
		size.Height = max32(size.Height, child.Height)
	}
	return size
}

func (l *trackingLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	x := float32(0)
	for _, o := range objects {
		child := o.MinSize()
		o.Resize(child)
		o.Move(fyne.NewPos(x, (size.Height-child.Height)/2))
		x += child.Width + l.spacing
	}
}

func max32(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}

// ---------------------------------------------------------------------------
// cards and headings

// newCaption renders a section heading the way the design wants them: small,
// uppercase, muted and letter spaced, so it labels the panel without competing
// with the content inside it.
func newCaption(text string) *fyne.Container {
	objects := make([]fyne.CanvasObject, 0, len(text))
	for _, r := range text {
		if r == ' ' {
			spacer := canvas.NewRectangle(color.Transparent)
			spacer.SetMinSize(fyne.NewSize(5, 1))
			objects = append(objects, spacer)
			continue
		}
		objects = append(objects, newText(string(r), 13, true, colorMuted))
	}
	return container.New(&trackingLayout{spacing: 2.5}, objects...)
}

// newCard wraps content in the middle surface layer: a rounded, slightly lighter
// panel with its own padding and an optional heading.
func newCard(heading string, content fyne.CanvasObject) fyne.CanvasObject {
	background := canvas.NewRectangle(colorCard)
	background.CornerRadius = cardRadius

	var body fyne.CanvasObject = content
	if heading != "" {
		body = container.NewBorder(
			inset(newCaption(heading), &insetLayout{bottom: 12}),
			nil, nil, nil, content)
	}
	return container.NewStack(background, inset(body, uniformInset(cardPadding)))
}

// ---------------------------------------------------------------------------
// connection indicator

// connectionDot carries the broker state as colour, not just as words: green
// when the panel is talking to the broker, red when it is not. From across the
// room the dot is the only part of this that is readable.
type connectionDot struct {
	dot   *canvas.Circle
	label *canvas.Text
	view  fyne.CanvasObject
}

func newConnectionDot() *connectionDot {
	dot := canvas.NewCircle(colorOffline)
	dot.Resize(fyne.NewSize(12, 12))
	holder := container.NewGridWrap(fyne.NewSize(12, 12), dot)
	label := newText("reconnecting…", 14, false, colorMuted)
	c := &connectionDot{dot: dot, label: label}
	c.view = container.New(&trackingLayout{spacing: 9}, container.NewCenter(holder), container.NewCenter(label))
	return c
}

func (c *connectionDot) set(connected bool) {
	if connected {
		c.dot.FillColor = colorOnline
		c.label.Text = "connected"
	} else {
		c.dot.FillColor = colorOffline
		c.label.Text = "reconnecting…"
	}
	c.dot.Refresh()
	c.label.Refresh()
}

// ---------------------------------------------------------------------------
// power toggle

// powerToggle is a switch rather than a checkbox. A checkbox reads as "tick this
// option"; the lights being on or off is a state, and a sliding switch that goes
// green is the control people already know for that.
type powerToggle struct {
	widget.BaseWidget
	on        bool
	OnChanged func(bool)
}

func newPowerToggle(onChanged func(bool)) *powerToggle {
	t := &powerToggle{OnChanged: onChanged}
	t.ExtendBaseWidget(t)
	return t
}

// SetOn moves the switch without reporting it, for state pushed in from MQTT.
func (t *powerToggle) SetOn(on bool) {
	if t.on == on {
		return
	}
	t.on = on
	t.Refresh()
}

func (t *powerToggle) Tapped(*fyne.PointEvent) {
	t.on = !t.on
	t.Refresh()
	if t.OnChanged != nil {
		t.OnChanged(t.on)
	}
}

func (t *powerToggle) AccessibilityLabel() string             { return "Lights on" }
func (t *powerToggle) AccessibilityRole() fyne.AccessibleRole { return fyne.AccessibleRoleButton }

func (t *powerToggle) CreateRenderer() fyne.WidgetRenderer {
	pill := canvas.NewRectangle(colorControl)
	pill.CornerRadius = 22
	track := canvas.NewRectangle(colorTrack)
	track.CornerRadius = 14
	knob := canvas.NewCircle(color.White)
	label := newText("Off", 16, true, colorText)
	r := &powerToggleRenderer{toggle: t, pill: pill, track: track, knob: knob, label: label}
	r.objects = []fyne.CanvasObject{pill, track, knob, label}
	return r
}

type powerToggleRenderer struct {
	toggle  *powerToggle
	pill    *canvas.Rectangle
	track   *canvas.Rectangle
	knob    *canvas.Circle
	label   *canvas.Text
	objects []fyne.CanvasObject
	size    fyne.Size
}

func (r *powerToggleRenderer) MinSize() fyne.Size { return fyne.NewSize(118, 44) }

func (r *powerToggleRenderer) Layout(size fyne.Size) {
	r.size = size
	r.pill.Resize(size)
	r.pill.CornerRadius = size.Height / 2

	trackHeight := size.Height - 16
	trackWidth := trackHeight * 1.8
	r.track.Move(fyne.NewPos(8, 8))
	r.track.Resize(fyne.NewSize(trackWidth, trackHeight))
	r.track.CornerRadius = trackHeight / 2

	knobSize := trackHeight - 6
	knobX := float32(8 + 3)
	if r.toggle.on {
		knobX = 8 + trackWidth - knobSize - 3
	}
	r.knob.Resize(fyne.NewSize(knobSize, knobSize))
	r.knob.Move(fyne.NewPos(knobX, 8+3))

	centerIn(r.label, 8+trackWidth+8+r.label.MinSize().Width/2, size.Height/2)
}

func (r *powerToggleRenderer) Refresh() {
	if r.toggle.on {
		r.track.FillColor = colorOnline
		r.label.Text = "On"
	} else {
		r.track.FillColor = colorTrack
		r.label.Text = "Off"
	}
	r.track.Refresh()
	r.label.Refresh()
	r.Layout(r.size)
	canvas.Refresh(r.toggle)
}

func (r *powerToggleRenderer) Objects() []fyne.CanvasObject { return r.objects }
func (r *powerToggleRenderer) Destroy()                     {}

// ---------------------------------------------------------------------------
// circular button

// circleButton is the round − / + pair flanking the slider. They are big round
// targets rather than bare glyphs because they are the fallback for anyone who
// cannot land a drag on the slider handle.
type circleButton struct {
	widget.BaseWidget
	glyph  string
	onTap  func()
	radius float32
}

func newCircleButton(glyph string, radius float32, onTap func()) *circleButton {
	b := &circleButton{glyph: glyph, radius: radius, onTap: onTap}
	b.ExtendBaseWidget(b)
	return b
}

func (b *circleButton) Tapped(*fyne.PointEvent) {
	if b.onTap != nil {
		b.onTap()
	}
}

func (b *circleButton) AccessibilityLabel() string             { return b.glyph }
func (b *circleButton) AccessibilityRole() fyne.AccessibleRole { return fyne.AccessibleRoleButton }

func (b *circleButton) CreateRenderer() fyne.WidgetRenderer {
	circle := canvas.NewCircle(colorControl)
	glyph := newText(b.glyph, 30, true, colorText)
	return &circleButtonRenderer{
		button:  b,
		circle:  circle,
		glyph:   glyph,
		objects: []fyne.CanvasObject{circle, glyph},
	}
}

type circleButtonRenderer struct {
	button  *circleButton
	circle  *canvas.Circle
	glyph   *canvas.Text
	objects []fyne.CanvasObject
}

func (r *circleButtonRenderer) MinSize() fyne.Size {
	return fyne.NewSize(r.button.radius*2, r.button.radius*2)
}

func (r *circleButtonRenderer) Layout(size fyne.Size) {
	side := min32(size.Width, size.Height)
	r.circle.Resize(fyne.NewSize(side, side))
	r.circle.Move(fyne.NewPos((size.Width-side)/2, (size.Height-side)/2))
	centerIn(r.glyph, size.Width/2, size.Height/2)
}

func (r *circleButtonRenderer) Refresh()                     { canvas.Refresh(r.button) }
func (r *circleButtonRenderer) Objects() []fyne.CanvasObject { return r.objects }
func (r *circleButtonRenderer) Destroy()                     {}

func min32(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}

// ---------------------------------------------------------------------------
// device chips

// deviceChip shows one strip as a pill: the name in white, its level in green.
// A run of plain text gave no way to tell where one device ended and the next
// began, and no way to see at a glance that a strip had gone quiet.
type deviceChip struct {
	widget.BaseWidget
	name  string
	value string
	tone  color.NRGBA
}

func newDeviceChip(name, value string, tone color.NRGBA) *deviceChip {
	c := &deviceChip{name: name, value: value, tone: tone}
	c.ExtendBaseWidget(c)
	return c
}

func (c *deviceChip) CreateRenderer() fyne.WidgetRenderer {
	background := canvas.NewRectangle(colorCard)
	background.CornerRadius = 16
	background.StrokeColor = colorHairline
	background.StrokeWidth = 1
	name := newText(c.name, 14, true, colorText)
	value := newText(c.value, 14, true, c.tone)
	return &deviceChipRenderer{
		chip:       c,
		background: background,
		name:       name,
		value:      value,
		objects:    []fyne.CanvasObject{background, name, value},
	}
}

type deviceChipRenderer struct {
	chip       *deviceChip
	background *canvas.Rectangle
	name       *canvas.Text
	value      *canvas.Text
	objects    []fyne.CanvasObject
}

const chipPadX, chipGap, chipHeight = 16, 7, 32

func (r *deviceChipRenderer) MinSize() fyne.Size {
	width := r.name.MinSize().Width + chipGap + r.value.MinSize().Width + chipPadX*2
	return fyne.NewSize(width, chipHeight)
}

func (r *deviceChipRenderer) Layout(size fyne.Size) {
	r.background.Resize(size)
	r.background.CornerRadius = size.Height / 2
	nameWidth := r.name.MinSize().Width
	centerIn(r.name, chipPadX+nameWidth/2, size.Height/2)
	centerIn(r.value, chipPadX+nameWidth+chipGap+r.value.MinSize().Width/2, size.Height/2)
}

func (r *deviceChipRenderer) Refresh()                     { canvas.Refresh(r.chip) }
func (r *deviceChipRenderer) Objects() []fyne.CanvasObject { return r.objects }
func (r *deviceChipRenderer) Destroy()                     {}
