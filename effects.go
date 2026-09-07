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
// Dolly and Cylon are the last two of the tuned defaults from GlowKitchen's
// scripts/demo_effect.sh, copied rather than derived because the panel has no
// way to read that script on a Pi that only has the binary. The other seven are
// ours. The originals were a tour of the nine renderers, one preset per mode,
// which is a good way to show what firmware can do and a poor way to fill a
// grid someone actually presses: Wipe, Chase, Blend and Loop were each a colour
// or two doing the plainest possible version of their mode. What replaced them
// picks the mode that serves the picture instead, which is why three presets
// are SPARKLE and two are CHASE and nothing is left on WIPE.

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

	// TraverseMs asks for a constant wall-clock crossing rather than a fixed
	// speed byte: how long one pass along the strip should take, in
	// milliseconds. Zero means "use Speed as written".
	//
	// The firmware advances these renderers one LED per tick, so a fixed speed
	// gives a crossing whose duration scales with the strip - Cylon at speed 0
	// is a 1.0s sweep on the 10-LED dev board and a 24s crawl on a 240-LED run.
	// A scanner is recognisable by its rate, not by its milliseconds per LED,
	// so the preset states the rate and the panel solves for the speed byte
	// against whatever length the strips have reported. See traverseSpeed.
	TraverseMs int `json:"traverseMs"`
}

var effects = []Effect{
	{ID: "dolly", Label: "Dolly", Emoji: "✨",
		Color:  "linear-gradient(135deg,#ff1493,#ffb6c1)",
		Mode:   "SPARKLE",
		Colors: []string{"#FF69B4", "#FF1493", "#FFB6C1"}, Speed: 128, Intensity: 255},

	{ID: "cylon", Label: "Cylon", Emoji: "👁️",
		Color:  "linear-gradient(135deg,#7f0000,#ff2b2b)",
		Mode:   "SCAN",
		Colors: []string{"#FF0000"}, Speed: 0, Intensity: 255,
		// One sweep in about a second, the rate the Knight Rider and Cylon
		// scanners actually move at, on a strip of any length.
		TraverseMs: 1100},

	// The dragon hoarding sign over the noodle bar in Blade Runner: cold blue
	// neon with the red of its tongue running through it. FLICKER because the
	// sign is a bad tube - it is never quite steady - and because FLICKER
	// forces saturation to full, which these three vivid colours already are.
	//
	// Speed and intensity were dead here until the firmware gave FLICKER its
	// knobs; both were carried over from Candle and did nothing. A neon tube is
	// not a candle: it buzzes rather than gutters, and it drops out hard, so
	// this now sits near the fast, deep end of both.
	{ID: "cyberpunk", Label: "Cyberpunk", Emoji: "🐉",
		Color:  "linear-gradient(135deg,#0088ff,#00ffff 45%,#ff0000)",
		Mode:   "FLICKER",
		Colors: []string{"#0088FF", "#00FFFF", "#FF0000"}, Speed: 205, Intensity: 215},

	// The four ghosts, in the order they leave the pen. The colours are
	// GlowGhosts' GHOST_COLORS, not the arcade's: Pinky and Clyde are
	// deliberately deeper than #FFB8FF and #FFB851, which carry so much white
	// that a WS2812B renders them as pale lavender and near-white. That project
	// found this on the same LEDs these strips use, so its values are taken
	// over the canonical ones rather than rediscovered here.
	//
	// Intensity is low so each ghost is a compact blob: with the firmware's
	// present one-run CHASE that reads as a single ghost patrolling, and with a
	// multi-run CHASE it becomes the four of them nose to tail.
	{ID: "pacman", Label: "Pac-Man", Emoji: "👻",
		Color:  "linear-gradient(135deg,#ff0000,#ff1e96 35%,#00ffff 65%,#ff4600)",
		Mode:   "CHASE",
		Colors: []string{"#FF0000", "#FF1E96", "#00FFFF", "#FF4600"},
		Speed:  150, Intensity: 70,
		// Ghosts are menacing, not quick: a lap in three seconds.
		TraverseMs: 3000},

	// Digital rain, third attempt and finally the right renderer. SPARKLE had
	// the pop-and-fade and no direction; CHASE had direction but moved its
	// runs in lockstep at one length, which is the one thing rain never does.
	// RAIN gives each drop its own rate and length, which is what the eye
	// actually reads as rain.
	{ID: "matrix", Label: "Matrix", Emoji: "🟩",
		Color:  "linear-gradient(180deg,#ccffcc,#00ff41 30%,#00340c)",
		Mode:   "RAIN",
		Colors: []string{"#00FF00", "#00FF88", "#CCFFCC"},
		Speed:  175, Intensity: 210},

	// Ruby Rhod, not the film. The Fifth Element's own palette is high-chroma
	// warms against one cold anchor, which on a strip is close to what Sunset
	// already does; Ruby is the exception the palette notes call out - leopard
	// and gold and hot pink, meant to clash with everything around it - and he
	// is the part of that film a light strip can actually be.
	//
	// The two dark swatches are deliberately left out. NEON takes hue and
	// saturation from a colour and its value from the flicker, so leopard spot
	// #241A10 and patent black #0B0B0B do not come out dark - they come out as
	// muddy bright orange and grey. Print spots are a texture, and a strip has
	// no way to render one; the four colours that survive the trip are the ones
	// whose hue carries the identity.
	//
	// NEON rather than FLICKER for the same reason Cyberpunk needs it: gold and
	// magenta have to be lit at once, in different places, the way they are on
	// the costume. FLICKER would show the whole strip gold, then the whole strip
	// pink.
	{ID: "fifthelement", Label: "Fifth Element", Emoji: "🎤",
		Color:  "linear-gradient(135deg,#d2a24c,#e0218a 40%,#c8a02c 70%,#f07aa8)",
		Mode:   "NEON",
		Colors: []string{"#D2A24C", "#E0218A", "#C8A02C", "#F07AA8"},
		Speed:  185, Intensity: 190},

	// Two light cycles. TRAIL rather than CHASE because a cycle's whole point
	// is that its ribbon stays: the arena fills as they ride, then clears and
	// starts over. CHASE's tail fades within a few LEDs, which is a comet.
	//
	// TraverseMs counts a full numLeds pass, but TRAIL completes its fill in
	// numLeds/colorCount ticks - each run only covers its own segment - so two
	// colours asking for 3600 fill in about 1800ms.
	{ID: "tron", Label: "Tron", Emoji: "🏍️",
		Color:  "linear-gradient(135deg,#00ffff,#0a1a2a 50%,#ff6600)",
		Mode:   "TRAIL",
		Colors: []string{"#00FFFF", "#FF6600"},
		Speed:  150, Intensity: 120,
		TraverseMs: 3600},

	// The seven tetrominoes, in the standard I-J-L-O-S-T-Z colours. This was
	// SPARKLE, which was colourful twinkling that happened to use recognisable
	// colours: nothing fell and nothing stacked, so the reference landed on the
	// palette alone. STACK drops a piece, settles it on what is already there,
	// and clears when the strip fills.
	//
	// Each piece is ledsPerColor LEDs long, taken from the device rather than
	// set here: one-LED pieces of different colours do not read as two pieces
	// on a diffused strip, they read as white. That is the same unit the strips
	// already group themes by, so a piece is as wide as a colour is on that
	// strip.
	//
	// Intensity 0 means no gap between settled pieces - they sit flush, the way
	// tetrominoes do. The gap is there in the mode for anyone who wants the
	// pieces separated, and is worth raising on a strip with a heavy diffuser.
	{ID: "tetris", Label: "Tetris", Emoji: "🧱",
		Color:  "linear-gradient(0deg,#00ffff,#0000ff 18%,#ff6600 34%,#ffff00 50%,#00ff00 66%,#8800ff 82%,#ff0000)",
		Mode:   "STACK",
		Colors: []string{"#00FFFF", "#0000FF", "#FF6600", "#FFFF00", "#00FF00", "#8800FF", "#FF0000"},
		Speed:  210, Intensity: 0,
		// A piece falling the full length of the strip takes about four
		// seconds; later pieces land sooner because the stack is in the way.
		TraverseMs: 4000},

	// A P1 phosphor terminal idling. Speed and intensity are both deliberately
	// low: PULSE takes about nine seconds for a breath here and only dips to
	// roughly two thirds, so it glows rather than pulses. It is the quietest
	// thing in the grid and the one worth leaving on.
	{ID: "phosphor", Label: "Phosphor", Emoji: "💾",
		Color:  "linear-gradient(135deg,#0a2a0a,#33ff33 70%,#0a2a0a)",
		Mode:   "PULSE",
		Colors: []string{"#00FF00"}, Speed: 40, Intensity: 80},
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

	// UsesIntensity is false only for BLEND, which hard-codes its own value and
	// ignores the field entirely. Grey the slider for that one, and only it.
	// FLICKER was the other until the firmware gave it both knobs.
	UsesIntensity bool `json:"usesIntensity"`

	// Means describes what intensity does here, as a sentence fragment ready to
	// display. It is different in every mode that uses it - width in some,
	// depth or saturation in others - which is the part that makes the slider
	// feel connected to something.
	Means string `json:"means"`

	// KeepsColors is false for the three modes that throw part of every colour
	// away: BLEND and FLICKER force saturation to full - FLICKER takes value
	// from its own flicker range - and COLORLOOP takes saturation from
	// intensity instead. A pastel sent to any of them comes back vivid, so the
	// UI steers away rather than letting the firmware quietly flatten the
	// palette.
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

	// SlowMs and FastMs are the firmware's tick interval at speed 0 and at
	// speed 255, from the renderers in GlowKitchen's src/effects.h. They are
	// set only for the three modes that advance one LED per tick - CHASE, WIPE
	// and SCAN - because those are the only ones where a tick interval and a
	// strip length together say how long a pass takes. Both zero means this
	// mode has no traversal to solve for, and TraverseMs is ignored.
	SlowMs int `json:"-"`
	FastMs int `json:"-"`
}

// modes is ordered for the picker rather than alphabetically: the ones worth
// reaching for first, and the two that discard colour last.
var modes = []Mode{
	{ID: "SPARKLE", Label: "Sparkle", UsesIntensity: true, KeepsColors: true,
		ShowsWholePalette: true, Means: "how many LEDs are lit at once"},
	{ID: "CHASE", Label: "Chase", UsesIntensity: true, KeepsColors: true,
		Means: "the width of the travelling run", SlowMs: 200, FastMs: 2},
	{ID: "SCAN", Label: "Scan", UsesIntensity: true, KeepsColors: true,
		Means: "the width of the sweeping band", SlowMs: 250, FastMs: 4},
	{ID: "WIPE", Label: "Wipe", UsesIntensity: true, KeepsColors: true,
		Means: "contrast with the part still to come", SlowMs: 200, FastMs: 2},
	{ID: "PULSE", Label: "Pulse", UsesIntensity: true, KeepsColors: true,
		Means: "the depth of the breath"},
	{ID: "STROBE", Label: "Strobe", UsesIntensity: true, KeepsColors: true,
		Means: "how much of each flash is on", MaxSeconds: 60},
	{ID: "COLORLOOP", Label: "Loop", UsesIntensity: true, KeepsColors: false,
		Means: "the saturation of the whole sweep"},
	{ID: "BLEND", Label: "Blend", UsesIntensity: false, KeepsColors: false},
	{ID: "FLICKER", Label: "Flicker", UsesIntensity: true, KeepsColors: false,
		Means: "how deep each flicker dips"},

	// The four renderers added in GlowKitchen issue #0021. All of them keep
	// the colours they are given - none forces saturation - so unlike FLICKER
	// they are safe to hand a pastel.
	{ID: "NEON", Label: "Neon", UsesIntensity: true, KeepsColors: true,
		Means: "how deep each flicker dips"},
	{ID: "RAIN", Label: "Rain", UsesIntensity: true, KeepsColors: true,
		Means: "how many drops are falling"},
	{ID: "TRAIL", Label: "Trail", UsesIntensity: true, KeepsColors: true,
		Means: "the width of the bright head", SlowMs: 200, FastMs: 2},
	{ID: "STACK", Label: "Stack", UsesIntensity: true, KeepsColors: true,
		Means: "the dark gap between settled pieces", SlowMs: 250, FastMs: 4},
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

// --- a constant crossing rate, whatever the strip's length -------------------

// The firmware's speed byte is not a rate, it is a tick interval: CHASE, WIPE
// and SCAN all advance exactly one LED per tick, so speed sets milliseconds per
// LED and the strip's length sets how many of them a pass takes. A preset with
// a fixed speed therefore crosses a long strip slowly and a short one quickly -
// Cylon at speed 0 sweeps a 10-LED dev board in 1.0s and a 240-LED run in 24s,
// which is why it read as broken on the long strips and fine on the bench.
//
// speedInterval mirrors the firmware helper of the same name from GlowKitchen's
// src/effects.h:
//
//	interval = slowMs - (slowMs-fastMs)*speed/255
//
// and traverseSpeed inverts it: given how long one pass should take, solve for
// the speed byte that produces it on a strip of this length.

// traverseSpeed returns the speed byte that makes one pass along a numLeds
// strip take about traverseMs in mode m. The interval is clamped to what the
// firmware can actually express, so a strip too long to cross in the requested
// time gets the fastest tick available rather than a wrapped-around speed.
func traverseSpeed(m Mode, numLeds, traverseMs int) int {
	if m.SlowMs <= m.FastMs || numLeds <= 0 || traverseMs <= 0 {
		return -1 // not a mode, or not a request, this can answer
	}
	interval := traverseMs / numLeds
	if interval < m.FastMs {
		interval = m.FastMs
	}
	if interval > m.SlowMs {
		interval = m.SlowMs
	}
	speed := (m.SlowMs - interval) * 255 / (m.SlowMs - m.FastMs)
	if speed < 0 {
		speed = 0
	}
	if speed > 255 {
		speed = 255
	}
	return speed
}

// minTraverseMs is the fastest crossing worth showing anyone: below about this,
// a run stops reading as something moving along the strip and reads as the
// whole strip blinking. It is the floor the shortest strip is protected by.
const minTraverseMs = 400

// speedForStrip is the speed byte that gives this preset its stated crossing
// time on one strip of a known length. It is what the per-device path uses, and
// it needs no compromise at all: each strip is solved for on its own terms.
//
// A preset without a crossing time, or a length we do not have, keeps the speed
// it was written with.
func speedForStrip(e Effect, numLeds int) int {
	if e.TraverseMs <= 0 || numLeds <= 0 {
		return e.Speed
	}
	m, ok := findMode(strings.ToUpper(strings.TrimSpace(e.Mode)))
	if !ok {
		return e.Speed
	}
	if speed := traverseSpeed(m, numLeds, e.TraverseMs); speed >= 0 {
		return speed
	}
	return e.Speed
}

// resolveSpeed is what a preset's speed becomes on the way out. A preset
// without a TraverseMs keeps the speed it was written with.
//
// The rate is solved for the longest strip, since the long ones are what a
// fixed speed leaves crawling. One SET_EFFECT reaches every strip at once
// though, so the same byte has to be survivable on the shortest, and the second
// term is that guarantee: never faster than a minTraverseMs crossing there.
//
// Note this deliberately does not use speedCeiling, which caps the builder's
// slider. That ceiling rises linearly with strip length, and applying it here
// held a 60-LED strip to a 2.3s sweep when 1.1s was both asked for and
// perfectly readable. The ceiling is the right guard for a slider, where the
// user can pick any speed and a bad one has to be unreachable; it is the wrong
// guard for a preset that has already said, in milliseconds, how fast it wants
// to cross.
func resolveSpeed(e Effect, longest, shortest int) int {
	if e.TraverseMs <= 0 || longest <= 0 {
		return e.Speed
	}
	m, ok := findMode(strings.ToUpper(strings.TrimSpace(e.Mode)))
	if !ok {
		return e.Speed
	}
	speed := traverseSpeed(m, longest, e.TraverseMs)
	if speed < 0 {
		return e.Speed
	}
	if shortest > 0 && shortest < longest {
		if floor := traverseSpeed(m, shortest, minTraverseMs); floor >= 0 && speed > floor {
			speed = floor
		}
	}
	return speed
}
