package main

import (
	"image/color"
	"math"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/widget"
)

const (
	sliderTrackHeight = 20
	sliderKnobRadius  = 19
	sliderTrackArea   = 52 // vertical band the track and knob live in
	sliderTickArea    = 26 // strip below it for the 0/25/50/75/100 labels
)

var sliderTicks = []float64{0, 25, 50, 75, 100}

// glowSlider replaces widget.Slider, which draws a hairline groove and a small
// handle. This panel is operated by a child, often with a fingertip, so the
// track is thick, the filled part is coloured so the current level is readable
// without finding the handle, and the handle carries a glow ring so it looks
// like something you are meant to grab.
type glowSlider struct {
	widget.BaseWidget

	Min, Max, Step float64
	Value          float64
	OnChanged      func(float64)

	// Mixed dims the fill when the strips are not all at the same level, so the
	// control does not claim to be showing one number for everything.
	Mixed bool

	width float32 // last laid-out width, needed to turn a pointer x into a value
}

func newGlowSlider(minimum, maximum float64) *glowSlider {
	s := &glowSlider{Min: minimum, Max: maximum, Step: 1, Value: minimum}
	s.ExtendBaseWidget(s)
	return s
}

func (s *glowSlider) SetValue(value float64) {
	value = s.clampAndSnap(value)
	if value == s.Value {
		return
	}
	s.Value = value
	s.Refresh()
	if s.OnChanged != nil {
		s.OnChanged(value)
	}
}

func (s *glowSlider) SetMixed(mixed bool) {
	if s.Mixed == mixed {
		return
	}
	s.Mixed = mixed
	s.Refresh()
}

func (s *glowSlider) clampAndSnap(value float64) float64 {
	if s.Step > 0 {
		value = s.Min + math.Round((value-s.Min)/s.Step)*s.Step
	}
	return math.Max(s.Min, math.Min(s.Max, value))
}

func (s *glowSlider) fraction() float64 {
	if s.Max <= s.Min {
		return 0
	}
	return clamp01((s.Value - s.Min) / (s.Max - s.Min))
}

// valueAt maps a pointer position to a value using the same travel the knob
// centre uses, so the handle lands under the finger instead of beside it.
func (s *glowSlider) valueAt(x float32) float64 {
	travel := s.width - 2*sliderKnobRadius
	if travel <= 0 {
		return s.Value
	}
	f := clamp01(float64((x - sliderKnobRadius) / travel))
	return s.Min + f*(s.Max-s.Min)
}

func (s *glowSlider) Tapped(e *fyne.PointEvent)  { s.SetValue(s.valueAt(e.Position.X)) }
func (s *glowSlider) Dragged(e *fyne.DragEvent)  { s.SetValue(s.valueAt(e.Position.X)) }
func (s *glowSlider) DragEnd()                   {}
func (s *glowSlider) AccessibilityLabel() string { return "Brightness" }
func (s *glowSlider) AccessibilityRole() fyne.AccessibleRole {
	return fyne.AccessibleRoleText
}

func (s *glowSlider) CreateRenderer() fyne.WidgetRenderer {
	track := canvas.NewRectangle(colorTrack)
	track.CornerRadius = sliderTrackHeight / 2
	halo := canvas.NewCircle(color.NRGBA{R: 0xa7, G: 0x9d, B: 0xea, A: 0x40})
	knob := canvas.NewCircle(color.White)
	knob.StrokeColor = color.NRGBA{R: 0xc9, G: 0xc1, B: 0xff, A: 0xff}
	knob.StrokeWidth = 2

	r := &glowSliderRenderer{slider: s, track: track, halo: halo, knob: knob}
	r.fill = canvas.NewRasterWithPixels(r.fillPixel)
	for _, tick := range sliderTicks {
		r.ticks = append(r.ticks, newText(formatPercentTick(tick), 12, false, colorMuted))
	}

	r.objects = []fyne.CanvasObject{track, r.fill, halo, knob}
	for _, tick := range r.ticks {
		r.objects = append(r.objects, tick)
	}
	return r
}

func formatPercentTick(v float64) string {
	switch v {
	case 0:
		return "0"
	case 25:
		return "25"
	case 50:
		return "50"
	case 75:
		return "75"
	}
	return "100"
}

type glowSliderRenderer struct {
	slider *glowSlider
	track  *canvas.Rectangle
	fill   *canvas.Raster
	halo   *canvas.Circle
	knob   *canvas.Circle
	ticks  []*canvas.Text

	// fillFraction is how much of the full track the drawn fill covers, so the
	// gradient inside the fill stays anchored to the whole track rather than
	// restretching every time the value moves.
	fillFraction float64

	objects []fyne.CanvasObject
	size    fyne.Size
}

func (r *glowSliderRenderer) MinSize() fyne.Size {
	return fyne.NewSize(180, sliderTrackArea+sliderTickArea)
}

func (r *glowSliderRenderer) Layout(size fyne.Size) {
	r.size = size
	r.slider.width = size.Width

	trackY := (sliderTrackArea - float32(sliderTrackHeight)) / 2
	r.track.Move(fyne.NewPos(0, trackY))
	r.track.Resize(fyne.NewSize(size.Width, sliderTrackHeight))

	fraction := r.slider.fraction()
	fillWidth := max32(sliderTrackHeight, float32(fraction)*size.Width)
	r.fillFraction = 0
	if size.Width > 0 {
		r.fillFraction = float64(fillWidth / size.Width)
	}
	r.fill.Move(fyne.NewPos(0, trackY))
	r.fill.Resize(fyne.NewSize(fillWidth, sliderTrackHeight))

	knobX := sliderKnobRadius + float32(fraction)*(size.Width-2*sliderKnobRadius)
	knobY := float32(sliderTrackArea) / 2
	r.knob.Resize(fyne.NewSize(sliderKnobRadius*2, sliderKnobRadius*2))
	r.knob.Move(fyne.NewPos(knobX-sliderKnobRadius, knobY-sliderKnobRadius))
	haloRadius := float32(sliderKnobRadius + 7)
	r.halo.Resize(fyne.NewSize(haloRadius*2, haloRadius*2))
	r.halo.Move(fyne.NewPos(knobX-haloRadius, knobY-haloRadius))

	for index, tick := range r.ticks {
		f := float32(sliderTicks[index] / 100)
		centerIn(tick, sliderKnobRadius+f*(size.Width-2*sliderKnobRadius), sliderTrackArea+sliderTickArea/2)
	}
}

// fillPixel draws the filled part of the track: a purple gradient with rounded
// ends. It is a raster because Fyne gradients cannot be corner rounded and
// there is no clipping to round them with.
func (r *glowSliderRenderer) fillPixel(x, y, w, h int) color.Color {
	// Always an NRGBA, including for fully transparent pixels: Fyne allocates the
	// backing image from the type returned for pixel 0,0, and pixel 0,0 is always
	// outside the rounded end. Handing back colour.Transparent there gets an
	// alpha-only image and the gradient renders as a flat white bar.
	if w == 0 || h == 0 {
		return color.NRGBA{}
	}
	scale := float64(h) / sliderTrackHeight
	coverage := roundedCoverage(float64(x)+0.5, float64(y)+0.5, float64(w), float64(h), sliderTrackHeight/2*scale)
	if coverage <= 0 {
		return color.NRGBA{}
	}
	t := (float64(x) + 0.5) / float64(w) * r.fillFraction
	c := lerpColor(colorFillStart, colorFillEnd, t)
	if r.slider.Mixed {
		c = lerpColor(c, colorTrack, 0.5)
	}
	c.A = uint8(coverage * 255)
	return c
}

func (r *glowSliderRenderer) Refresh() {
	if r.slider.Mixed {
		r.halo.FillColor = color.NRGBA{R: 0x8a, G: 0x86, B: 0xa8, A: 0x40}
	} else {
		r.halo.FillColor = color.NRGBA{R: 0xa7, G: 0x9d, B: 0xea, A: 0x40}
	}
	r.Layout(r.size)
	r.fill.Refresh()
	r.halo.Refresh()
	canvas.Refresh(r.slider)
}

func (r *glowSliderRenderer) Objects() []fyne.CanvasObject { return r.objects }
func (r *glowSliderRenderer) Destroy()                     {}
