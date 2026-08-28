package main

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Effects are the firmware's SET_EFFECT command dressed up as one-tap buttons.
// A theme is a look the strips hold; an effect is something they *do*, and the
// firmware treats it that way - it runs, it can time out, and it reverts to
// whatever theme was showing when it stops.
//
// The nine presets below are the tuned defaults from GlowKitchen's
// scripts/demo_effect.sh. They are copied rather than derived because the panel
// has no way to read that script on a Pi that only has the binary.

// Effect is one preset: the payload fields the firmware wants, plus the label
// and colour the button wears. Both live here for the same reason Theme's do -
// adding a preset is a one-line change in one file.
type Effect struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Emoji string `json:"emoji"`
	Color string `json:"color"` // CSS background for the button

	Mode      string   `json:"mode"`      // one of the nine mode IDs
	Colors    []string `json:"colors"`    // 1-8 entries, #RRGGBB
	Speed     int      `json:"speed"`     // 0-255, 0 slowest
	Intensity int      `json:"intensity"` // 0-255, meaning is per mode
}

var effects = []Effect{
	{ID: "dolly", Label: "Dolly", Emoji: "✨",
		Color:  "linear-gradient(135deg,#ff1493,#ffb6c1)",
		Mode:   "SPARKLE",
		Colors: []string{"#FF69B4", "#FF1493", "#FFB6C1"}, Speed: 128, Intensity: 255},

	{ID: "cylon", Label: "Cylon", Emoji: "👁️",
		Color:  "linear-gradient(135deg,#7f0000,#ff2b2b)",
		Mode:   "SCAN",
		Colors: []string{"#FF0000"}, Speed: 0, Intensity: 255},

	{ID: "wipe", Label: "Wipe", Emoji: "🧹",
		Color:  "linear-gradient(90deg,#ff0000 55%,#2a0000 55%)",
		Mode:   "WIPE",
		Colors: []string{"#FF0000"}, Speed: 140, Intensity: 200},

	{ID: "chase", Label: "Chase", Emoji: "🏃",
		Color:  "linear-gradient(135deg,#ff6600,#00aaff)",
		Mode:   "CHASE",
		Colors: []string{"#FF6600", "#00AAFF"}, Speed: 150, Intensity: 200},

	{ID: "candle", Label: "Candle", Emoji: "🕯️",
		Color:  "linear-gradient(135deg,#ff3000,#ffa000)",
		Mode:   "FLICKER",
		Colors: []string{"#FF7000", "#FF3000", "#FFA000"}, Speed: 100, Intensity: 180},

	{ID: "breathe", Label: "Breathe", Emoji: "🌙",
		Color:  "linear-gradient(135deg,#001b6b,#3355ff)",
		Mode:   "PULSE",
		Colors: []string{"#0033FF"}, Speed: 90, Intensity: 220},

	{ID: "strobe", Label: "Strobe", Emoji: "⚡",
		Color:  "linear-gradient(135deg,#3a3558,#ffffff)",
		Mode:   "STROBE",
		Colors: []string{"#FFFFFF"}, Speed: 200, Intensity: 128},

	{ID: "loop", Label: "Loop", Emoji: "🔁",
		Color:  "linear-gradient(135deg,#ff0000,#00ff00,#0000ff)",
		Mode:   "COLORLOOP",
		Colors: []string{"#FF0000", "#00FF00", "#0000FF"}, Speed: 255, Intensity: 255},

	{ID: "blend", Label: "Blend", Emoji: "🎨",
		Color:  "linear-gradient(135deg,#ff0000,#ffaa00)",
		Mode:   "BLEND",
		Colors: []string{"#FF0000", "#FFAA00"}, Speed: 120, Intensity: 128},
}

// Duration is one choice on the "run for" row. Zero seconds means the effect
// runs until something else changes it, which is what the firmware does with a
// timeout of 0.
type Duration struct {
	Seconds int    `json:"seconds"`
	Label   string `json:"label"`
}

var durations = []Duration{
	{Seconds: 300, Label: "5 min"},
	{Seconds: 3600, Label: "1 hour"},
	{Seconds: 0, Label: "Until stopped"},
}

// defaultDurationSeconds is what a freshly opened panel has selected.
const defaultDurationSeconds = 300

// Mode is one of the firmware's nine renderers, along with the three things a
// UI has to know about it. All three come from firmware behaviour rather than
// from taste, and a builder that ignores them produces effects that look
// nothing like what was picked.
type Mode struct {
	ID    string `json:"id"`    // the name the firmware expects
	Label string `json:"label"` // how it reads on a button

	// UsesIntensity is false for the two modes that hard-code their own value
	// and ignore the field entirely. Grey the slider for those, and only those.
	UsesIntensity bool `json:"usesIntensity"`

	// Means describes what intensity does here, as a sentence fragment ready to
	// display. It is different in every mode that uses it - width in some,
	// depth or saturation in others - which is the part that makes the slider
	// feel connected to something.
	Means string `json:"means"`

	// KeepsColors is false for the three modes that throw part of every colour
	// away: BLEND and FLICKER force saturation and value to full, and COLORLOOP
	// takes saturation from intensity instead. A pastel sent to any of them
	// comes back vivid, so the UI steers away rather than letting the firmware
	// quietly flatten the palette.
	KeepsColors bool `json:"keepsColors"`

	// ShowsWholePalette is true only for SPARKLE, which lays the colours out by
	// LED position rather than cycling them. It is the one mode where "what do
	// these eight colours look like together" has a spatial answer, so the
	// preview draws it differently.
	ShowsWholePalette bool `json:"showsWholePalette"`

	// MaxSeconds caps how long this mode may be asked to run, 0 for no cap.
	// Only STROBE sets it: a strobe is a novelty for a few seconds and an
	// unpleasant thing to leave running in a kitchen for an hour, so the
	// "until stopped" choice is clamped rather than offered honestly.
	MaxSeconds int `json:"maxSeconds"`
}

// modes is ordered for the picker rather than alphabetically: the ones worth
// reaching for first, and the two that discard colour last.
var modes = []Mode{
	{ID: "SPARKLE", Label: "Sparkle", UsesIntensity: true, KeepsColors: true,
		ShowsWholePalette: true, Means: "how many LEDs are lit at once"},
	{ID: "CHASE", Label: "Chase", UsesIntensity: true, KeepsColors: true,
		Means: "the width of the travelling run"},
	{ID: "SCAN", Label: "Scan", UsesIntensity: true, KeepsColors: true,
		Means: "the width of the sweeping band"},
	{ID: "WIPE", Label: "Wipe", UsesIntensity: true, KeepsColors: true,
		Means: "contrast with the part still to come"},
	{ID: "PULSE", Label: "Pulse", UsesIntensity: true, KeepsColors: true,
		Means: "the depth of the breath"},
	{ID: "STROBE", Label: "Strobe", UsesIntensity: true, KeepsColors: true,
		Means: "how much of each flash is on", MaxSeconds: 60},
	{ID: "COLORLOOP", Label: "Loop", UsesIntensity: true, KeepsColors: false,
		Means: "the saturation of the whole sweep"},
	{ID: "BLEND", Label: "Blend", UsesIntensity: false, KeepsColors: false},
	{ID: "FLICKER", Label: "Flicker", UsesIntensity: false, KeepsColors: false},
}

func findMode(id string) (Mode, bool) {
	for _, m := range modes {
		if m.ID == id {
			return m, true
		}
	}
	return Mode{}, false
}

// maxTimeoutSeconds is the firmware's 8 hour ceiling. It clamps rather than
// rejects, but the panel should not be sending something it knows will be
// changed underneath it.
const maxTimeoutSeconds = 28800

// maxPayloadBytes guards the device's 512 byte MQTT buffer. The largest legal
// payload is well under this; the check is cheap insurance against a future
// preset with eight long colours.
const maxPayloadBytes = 480

var hexColorRe = regexp.MustCompile(`^#?[0-9a-fA-F]{6}$`)

// effectBody is the JSON object that follows the SET_EFFECT: prefix. Note that
// the payload as a whole is not JSON - the prefix is a literal - so this is
// marshalled on its own and concatenated rather than wrapped in an envelope.
//
// None of these fields are omitempty on purpose: speed 0 is the slowest scan,
// not an absent field, and leaving it out would silently give Cylon the
// firmware's default of 128 instead.
type effectBody struct {
	Mode      string   `json:"mode"`
	Colors    []string `json:"colors"`
	Speed     int      `json:"speed"`
	Intensity int      `json:"intensity"`
	Timeout   int      `json:"timeout"`
}

// buildEffectPayload validates and renders one SET_EFFECT command.
//
// The validation is not defensive habit: the firmware answers a malformed
// payload by changing nothing and publishing nothing - no error topic, no NACK,
// the reason goes to its serial log alone. A rejection is indistinguishable
// from a dropped message, so anything wrong has to be caught here or it is
// never caught at all.
func buildEffectPayload(e Effect, seconds int) (string, error) {
	m, ok := findMode(strings.ToUpper(strings.TrimSpace(e.Mode)))
	if !ok {
		return "", fmt.Errorf("unknown effect mode: %s", e.Mode)
	}
	mode := m.ID

	if len(e.Colors) < 1 || len(e.Colors) > 8 {
		return "", fmt.Errorf("%s: needs 1-8 colours, has %d", e.Label, len(e.Colors))
	}
	for _, c := range e.Colors {
		// Three-digit #RGB is not supported by the firmware's parser, so the
		// pattern deliberately requires all six digits.
		if !hexColorRe.MatchString(c) {
			return "", fmt.Errorf("%s: %q is not #RRGGBB", e.Label, c)
		}
	}

	if e.Speed < 0 || e.Speed > 255 {
		return "", fmt.Errorf("%s: speed %d outside 0-255", e.Label, e.Speed)
	}
	if e.Intensity < 0 || e.Intensity > 255 {
		return "", fmt.Errorf("%s: intensity %d outside 0-255", e.Label, e.Intensity)
	}

	if seconds < 0 {
		seconds = 0
	}
	// "Until stopped" is 0, which no cap should turn into "for ever" - so a
	// capped mode asked to run unbounded runs for its cap instead. The cap is
	// on the mode, not the preset, so the builder inherits it too.
	if m.MaxSeconds > 0 && (seconds == 0 || seconds > m.MaxSeconds) {
		seconds = m.MaxSeconds
	}
	if seconds > maxTimeoutSeconds {
		seconds = maxTimeoutSeconds
	}

	body, err := json.Marshal(effectBody{
		Mode:      mode,
		Colors:    e.Colors,
		Speed:     e.Speed,
		Intensity: e.Intensity,
		Timeout:   seconds,
	})
	if err != nil {
		return "", err
	}

	payload := "SET_EFFECT:" + string(body)
	if len(payload) > maxPayloadBytes {
		return "", fmt.Errorf("%s: payload is %d bytes, over the %d byte limit",
			e.Label, len(payload), maxPayloadBytes)
	}
	return payload, nil
}

func findEffect(id string) (Effect, bool) {
	for _, e := range effects {
		if e.ID == id {
			return e, true
		}
	}
	return Effect{}, false
}

// --- the swatch palette ------------------------------------------------------

// PaletteColor is one tappable colour in the builder's palette.
//
// The palette is drawn in our own DOM rather than through <input type="color">.
// That input hands back #rrggbb with no conversion, which is tempting, but it
// delegates the interaction to the OS: a desktop colour chooser dialog, on a
// Pi's small touchscreen, under labwc, with no keyboard. A fixed grid of large
// targets is the thing that actually works on the hardware this runs on.
type PaletteColor struct {
	Hex   string `json:"hex"`
	Name  string `json:"name"`
	Vivid bool   `json:"vivid"`
}

// The first sixteen are fully saturated at full value, so every mode renders
// them as picked. The last eight are pastels and whites, which BLEND, FLICKER
// and COLORLOOP flatten - the builder greys those three modes out while any of
// these is in the palette.
var palette = []PaletteColor{
	{Hex: "#FF0000", Name: "Red"}, {Hex: "#FF4400", Name: "Vermilion"},
	{Hex: "#FF6600", Name: "Orange"}, {Hex: "#FFAA00", Name: "Amber"},
	{Hex: "#FFFF00", Name: "Yellow"}, {Hex: "#AAFF00", Name: "Lime"},
	{Hex: "#00FF00", Name: "Green"}, {Hex: "#00FF88", Name: "Spring"},
	{Hex: "#00FFFF", Name: "Cyan"}, {Hex: "#0088FF", Name: "Azure"},
	{Hex: "#0000FF", Name: "Blue"}, {Hex: "#8800FF", Name: "Violet"},
	{Hex: "#FF00FF", Name: "Magenta"}, {Hex: "#FF0088", Name: "Fuchsia"},
	{Hex: "#FF0044", Name: "Rose"}, {Hex: "#00FFAA", Name: "Teal"},

	{Hex: "#FFFFFF", Name: "White"}, {Hex: "#FFD9A0", Name: "Warm white"},
	{Hex: "#FFB6C1", Name: "Soft pink"}, {Hex: "#FFCBA4", Name: "Peach"},
	{Hex: "#FFF3C4", Name: "Cream"}, {Hex: "#B6FFD1", Name: "Mint"},
	{Hex: "#A0D8FF", Name: "Sky"}, {Hex: "#C9B6FF", Name: "Lavender"},
}

func init() {
	// Derived rather than typed out, so a colour added to the list above cannot
	// be labelled wrongly.
	for i := range palette {
		palette[i].Vivid = isVivid(palette[i].Hex)
	}
}

// isVivid reports whether a colour survives BLEND, FLICKER and COLORLOOP
// unchanged. Those three force both saturation and value to full, so only a
// colour that is already there - one channel at 0 and one at 255 - comes back
// looking like what was picked. #FFB6C1 renders as vivid pink, not soft pink.
func isVivid(hex string) bool {
	h := strings.TrimPrefix(strings.TrimSpace(hex), "#")
	if len(h) != 6 {
		return false
	}
	var lo, hi int64 = 255, 0
	for i := 0; i < 3; i++ {
		v, err := strconv.ParseInt(h[i*2:i*2+2], 16, 32)
		if err != nil {
			return false
		}
		if v < lo {
			lo = v
		}
		if v > hi {
			hi = v
		}
	}
	return lo == 0 && hi == 255
}

// AllVivid reports whether every colour in a palette survives the three modes
// that discard saturation and value.
func AllVivid(colors []string) bool {
	for _, c := range colors {
		if !isVivid(c) {
			return false
		}
	}
	return true
}

// --- speed on short strips ---------------------------------------------------

// speedCeiling scales the usable top of the speed slider by strip length. The
// renderers were tuned for roughly 240 LEDs; on the 10-LED dev board anything
// much above 140 crosses the whole strip in a fraction of a second and reads as
// a flash rather than as movement. Exposing a raw 0-255 gives the user a slider
// whose top third does nothing useful.
//
// Effects are broadcast, so this takes the shortest strip that has reported:
// the ceiling that is safe for all of them.
func speedCeiling(numLeds int) int {
	const (
		shortStrip = 10
		longStrip  = 240
		shortMax   = 140
		longMax    = 255
	)
	if numLeds <= 0 {
		return longMax // nothing has reported yet; do not clamp on a guess
	}
	if numLeds <= shortStrip {
		return shortMax
	}
	if numLeds >= longStrip {
		return longMax
	}
	span := longStrip - shortStrip
	return shortMax + (numLeds-shortStrip)*(longMax-shortMax)/span
}
