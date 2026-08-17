package main

import (
	"embed"
	"fmt"
	"strings"
	"sync"

	"fyne.io/fyne/v2"
)

// Emoji are shipped as bitmaps rather than drawn as text. Fyne renders glyphs
// through the bundled text font, and the emoji coverage there is neither
// complete nor identical across macOS and the Pi - 🌿 in particular came out as
// a handful of stray specks. Embedding the Noto Color Emoji bitmaps makes every
// tile render the same everywhere, which matters because these six icons are
// how a non-reading child tells the looks apart.
//
// The artwork is Microsoft's Fluent Emoji rather than Noto: the flat Noto
// drawings read as a downgrade next to the Apple emoji the WebKit build got on
// macOS, and these have the shading and depth that made those look right.
//
// Source: github.com/microsoft/fluentui-emoji, 3D style, 256px PNGs, MIT.
//
//go:embed assets/emoji/*.png
var emojiFS embed.FS

var (
	emojiOnce  sync.Once
	emojiCache map[string]fyne.Resource
)

// emojiResource maps an emoji string to its embedded bitmap, keyed by the Noto
// file naming convention (emoji_u1f308.png). Returns nil when the emoji has no
// bundled asset so callers can fall back to drawing it as text.
func emojiResource(emoji string) fyne.Resource {
	emojiOnce.Do(loadEmoji)
	return emojiCache[emojiKey(emoji)]
}

func loadEmoji() {
	emojiCache = make(map[string]fyne.Resource)
	entries, err := emojiFS.ReadDir("assets/emoji")
	if err != nil {
		return
	}
	for _, entry := range entries {
		data, err := emojiFS.ReadFile("assets/emoji/" + entry.Name())
		if err != nil {
			continue
		}
		key := strings.TrimSuffix(strings.TrimPrefix(entry.Name(), "emoji_"), ".png")
		emojiCache[key] = fyne.NewStaticResource(entry.Name(), data)
	}
}

// emojiKey renders the code points of an emoji the way Noto names its files:
// lowercase hex, "u" prefixed, joined with underscores for sequences.
func emojiKey(emoji string) string {
	var parts []string
	for _, r := range emoji {
		// Variation selectors are presentation hints, not part of the file name.
		if r == 0xfe0f || r == 0xfe0e {
			continue
		}
		parts = append(parts, fmt.Sprintf("u%04x", r))
	}
	return strings.Join(parts, "_")
}
