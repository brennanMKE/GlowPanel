package main

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/widget"
)

const (
	// The tile is drawn inset inside its widget so the selection glow has
	// somewhere to go. Without the margin the glow would be clipped at the
	// widget bounds and the ring would look like it was sitting on the edge of
	// a hole rather than around a card.
	tileInset     = 4
	tileRingWidth = 3
	tileGlowWidth = 4
	tileEmojiSize = 44
)

type themeButton struct {
	widget.BaseWidget
	theme   Theme
	colors  []color.NRGBA
	active  bool
	focused bool
	onTap   func()
}

func newThemeButton(item Theme, onTap func()) *themeButton {
	button := &themeButton{theme: item, onTap: onTap}
	// The palette is painted raw. These are the values the CSS build used, and
	// muting them for the screen cost the tiles the depth that made a child able
	// to tell six looks apart at a glance.
	button.colors = append(make([]color.NRGBA, 0, len(item.Colors)), item.Colors...)
	button.ExtendBaseWidget(button)
	return button
}

func (b *themeButton) AccessibilityLabel() string { return b.theme.Label }
func (b *themeButton) AccessibilityRole() fyne.AccessibleRole {
	return fyne.AccessibleRoleButton
}
func (b *themeButton) FocusGained()   { b.focused = true; b.Refresh() }
func (b *themeButton) FocusLost()     { b.focused = false; b.Refresh() }
func (b *themeButton) TypedRune(rune) {}
func (b *themeButton) TypedKey(event *fyne.KeyEvent) {
	if event.Name == fyne.KeyReturn || event.Name == fyne.KeySpace {
		b.Tapped(nil)
	}
}
func (b *themeButton) Tapped(*fyne.PointEvent) {
	if b.onTap != nil {
		b.onTap()
	}
}
func (b *themeButton) SetActive(active bool) {
	if b.active == active {
		return
	}
	b.active = active
	b.Refresh()
}

func (b *themeButton) CreateRenderer() fyne.WidgetRenderer {
	r := &themeButtonRenderer{button: b}
	r.surface = canvas.NewRasterWithPixels(r.pixel)

	// The label sits on gradients that run from dark to light, so white alone
	// loses contrast at the light end. A dropped shadow keeps it legible on
	// every tile without having to pick a different colour per theme.
	r.shadow = newText(b.theme.Label, 19, true, color.NRGBA{A: 0x99})
	r.label = newText(b.theme.Label, 19, true, color.White)

	if res := emojiResource(b.theme.Emoji); res != nil {
		image := canvas.NewImageFromResource(res)
		image.FillMode = canvas.ImageFillContain
		r.emojiImage = image
	} else {
		// Nothing bundled for this emoji - draw it as text and accept whatever
		// the platform font manages.
		r.emojiText = newText(b.theme.Emoji, tileEmojiSize, false, color.White)
		r.emojiText.Alignment = fyne.TextAlignCenter
	}

	r.objects = []fyne.CanvasObject{r.surface}
	if r.emojiImage != nil {
		r.objects = append(r.objects, r.emojiImage)
	} else {
		r.objects = append(r.objects, r.emojiText)
	}
	r.objects = append(r.objects, r.shadow, r.label)
	return r
}

type themeButtonRenderer struct {
	button     *themeButton
	surface    *canvas.Raster
	emojiImage *canvas.Image
	emojiText  *canvas.Text
	shadow     *canvas.Text
	label      *canvas.Text
	objects    []fyne.CanvasObject
	size       fyne.Size
}

func (r *themeButtonRenderer) MinSize() fyne.Size { return fyne.NewSize(150, 86) }

func (r *themeButtonRenderer) Layout(size fyne.Size) {
	r.size = size
	r.surface.Resize(size)
	r.surface.Move(fyne.NewPos(0, 0))

	emojiY := size.Height * 0.37
	if r.emojiImage != nil {
		r.emojiImage.Resize(fyne.NewSize(tileEmojiSize, tileEmojiSize))
		r.emojiImage.Move(fyne.NewPos((size.Width-tileEmojiSize)/2, emojiY-tileEmojiSize/2))
	} else {
		centerIn(r.emojiText, size.Width/2, emojiY)
	}

	labelY := size.Height - tileInset - 20
	centerIn(r.label, size.Width/2, labelY)
	centerIn(r.shadow, size.Width/2+1.5, labelY+1.5)
}

// pixel paints one tile: the theme gradient on a 135 degree diagonal,
// a hairline highlight along the top edge, and - when the tile is the selected
// look - a white ring that follows the rounded edge exactly, with a short glow
// outside it.
func (r *themeButtonRenderer) pixel(x, y, w, h int) color.Color {
	// Every return is an NRGBA on purpose - see fillPixel in slider.go: Fyne
	// picks the backing image format from whatever pixel 0,0 hands back.
	if w == 0 || h == 0 || r.size.Width == 0 {
		return color.NRGBA{}
	}
	scale := float64(w) / float64(r.size.Width)
	margin := tileInset * scale
	tileW := float64(w) - 2*margin
	tileH := float64(h) - 2*margin
	if tileW <= 0 || tileH <= 0 {
		return color.NRGBA{}
	}
	px := float64(x) + 0.5 - margin
	py := float64(y) + 0.5 - margin

	distance := roundedDistance(px, py, tileW, tileH, tileRadius*scale)
	out := color.NRGBA{}

	if coverage := clamp01(0.5 - distance); coverage > 0 {
		base := gradientAt(r.button.colors, (px+py)/(tileW+tileH))
		// A slight top-down wash and a bright top edge give the tile the same
		// glassy card look the web build got from a CSS gradient plus border.
		base = lerpColor(base, color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}, 0.05*(1-clamp01(py/(tileH*0.6))))
		if edge := -distance; edge >= 0 && edge < 1.6*scale {
			base = lerpColor(base, color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}, 0.16*(1-edge/(1.6*scale)))
		}
		base.A = uint8(coverage * 255)
		out = base
	}

	if r.button.active || r.button.focused {
		if distance > 0 && distance < tileGlowWidth*scale {
			falloff := 1 - distance/(tileGlowWidth*scale)
			glow := color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: uint8(0.30 * falloff * falloff * 255)}
			out = overNRGBA(glow, out)
		}
		inner := clamp01(0.5 - (distance + tileRingWidth*scale))
		if band := clamp01(0.5-distance) - inner; band > 0 {
			out = overNRGBA(color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: uint8(band * 255)}, out)
		}
	}
	return out
}

func (r *themeButtonRenderer) Refresh() {
	r.surface.Refresh()
	canvas.Refresh(r.button)
}

func (r *themeButtonRenderer) Objects() []fyne.CanvasObject { return r.objects }
func (r *themeButtonRenderer) Destroy()                     {}

// gradientAt samples a multi-stop gradient. Rainbow has six stops, the rest have
// two, and they all have to come off the same code path.
func gradientAt(colors []color.NRGBA, t float64) color.NRGBA {
	switch len(colors) {
	case 0:
		return color.NRGBA{A: 0xff}
	case 1:
		return colors[0]
	}
	t = clamp01(t)
	position := t * float64(len(colors)-1)
	index := int(position)
	if index >= len(colors)-1 {
		return colors[len(colors)-1]
	}
	return lerpColor(colors[index], colors[index+1], position-float64(index))
}
