package main

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

// The panel is read from across a kitchen, often by a child, so the palette is
// built around three stacked surfaces rather than flat black: the window, the
// cards that sit on it, and the controls inside those cards. Each step up is a
// little lighter, which is what makes the cards read as panels instead of as
// arbitrary regions of background.
//
// The values are sampled from the original Wails build so the two versions look
// like the same product.
var (
	colorWindow   = color.NRGBA{R: 0x12, G: 0x10, B: 0x1b, A: 0xff} // deepest layer
	colorCard     = color.NRGBA{R: 0x1c, G: 0x1a, B: 0x2a, A: 0xff} // panels
	colorControl  = color.NRGBA{R: 0x29, G: 0x26, B: 0x3e, A: 0xff} // buttons, pills
	colorTrack    = color.NRGBA{R: 0x2e, G: 0x2b, B: 0x47, A: 0xff} // slider groove
	colorHairline = color.NRGBA{R: 0x2d, G: 0x2a, B: 0x40, A: 0xff}

	colorText    = color.NRGBA{R: 0xf4, G: 0xf2, B: 0xfe, A: 0xff}
	colorMuted   = color.NRGBA{R: 0xa3, G: 0x9d, B: 0xc1, A: 0xff}
	colorOnline  = color.NRGBA{R: 0x5e, G: 0xc2, B: 0x69, A: 0xff}
	colorOffline = color.NRGBA{R: 0xe0, G: 0x5a, B: 0x5a, A: 0xff}
	colorDanger  = color.NRGBA{R: 0xff, G: 0x8a, B: 0x8a, A: 0xff}

	// Slider fill, dark to light left to right.
	colorFillStart = color.NRGBA{R: 0x38, G: 0x34, B: 0x54, A: 0xff}
	colorFillEnd   = color.NRGBA{R: 0xa7, G: 0x9d, B: 0xea, A: 0xff}
)

const (
	outerInset  = 22 // window edge to content, identical on all four sides
	cardRadius  = 18
	cardPadding = 16
	tileRadius  = 14
	tileGutter  = 16
)

// glowTheme keeps Fyne's stock widgets - the ones we did not replace - in step
// with the hand-drawn ones. Everything not listed falls through to the bundled
// dark theme.
type glowTheme struct{}

func (glowTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	switch name {
	case theme.ColorNameBackground:
		return colorWindow
	case theme.ColorNameForeground:
		return colorText
	case theme.ColorNameDisabled, theme.ColorNamePlaceHolder:
		return colorMuted
	case theme.ColorNameButton, theme.ColorNameInputBackground:
		return colorControl
	case theme.ColorNameOverlayBackground, theme.ColorNameMenuBackground:
		return colorCard
	case theme.ColorNameSeparator:
		return colorHairline
	case theme.ColorNameError:
		return colorDanger
	case theme.ColorNameSuccess:
		return colorOnline
	// The theme tiles draw their own selection ring, inside their own bounds. The
	// stock focus tint follows whatever primary colour Fyne is configured with -
	// violet, on the machine this was ported on - and painted a second halo that
	// spilled past the tile. Neutralising it leaves exactly one ring whatever the
	// user's primary colour happens to be.
	case theme.ColorNameFocus:
		return color.Transparent
	case theme.ColorNameSelection:
		return colorControl
	case theme.ColorNameHover:
		return color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0x14}
	}
	return theme.DarkTheme().Color(name, variant)
}

func (glowTheme) Font(style fyne.TextStyle) fyne.Resource {
	return theme.DarkTheme().Font(style)
}

func (glowTheme) Icon(name fyne.ThemeIconName) fyne.Resource {
	return theme.DarkTheme().Icon(name)
}

func (glowTheme) Size(name fyne.ThemeSizeName) float32 {
	switch name {
	case theme.SizeNamePadding:
		return 8
	case theme.SizeNameInnerPadding:
		return 8
	case theme.SizeNameText:
		return 14
	}
	return theme.DarkTheme().Size(name)
}
