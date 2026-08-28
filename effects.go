package main

import (
	"encoding/json"
	"fmt"
	"regexp"
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

	Mode      string   `json:"mode"`      // one of effectModes
	Colors    []string `json:"colors"`    // 1-8 entries, #RRGGBB
	Speed     int      `json:"speed"`     // 0-255, 0 slowest
	Intensity int      `json:"intensity"` // 0-255, meaning is per mode

	// MaxSeconds caps how long this preset may be asked to run, 0 for no cap.
	// Only STROBE sets it: a strobe is a novelty for a few seconds and an
	// unpleasant thing to leave running in a kitchen for an hour, so the
	// "until stopped" choice is clamped rather than offered honestly.
	MaxSeconds int `json:"maxSeconds"`
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
		Colors: []string{"#FFFFFF"}, Speed: 200, Intensity: 128,
		MaxSeconds: 60},

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

// effectModes is the firmware's list. A mode outside it is rejected by the
// device silently, so the panel checks first.
var effectModes = map[string]bool{
	"BLEND": true, "FLICKER": true, "CHASE": true, "WIPE": true, "SCAN": true,
	"SPARKLE": true, "PULSE": true, "STROBE": true, "COLORLOOP": true,
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
	mode := strings.ToUpper(strings.TrimSpace(e.Mode))
	if !effectModes[mode] {
		return "", fmt.Errorf("unknown effect mode: %s", e.Mode)
	}

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
	// capped preset asked to run unbounded runs for its cap instead.
	if e.MaxSeconds > 0 && (seconds == 0 || seconds > e.MaxSeconds) {
		seconds = e.MaxSeconds
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
