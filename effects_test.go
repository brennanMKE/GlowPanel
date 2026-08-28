package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// The device answers a malformed SET_EFFECT by changing nothing and publishing
// nothing at all - no error topic, no NACK, the reason goes only to its serial
// log. So these cover the checks that have to happen here, because a rejection
// on the wire is indistinguishable from a message that never arrived.

func decodeBody(t *testing.T, payload string) effectBody {
	t.Helper()
	const prefix = "SET_EFFECT:"
	if !strings.HasPrefix(payload, prefix) {
		t.Fatalf("payload does not start with %q: %s", prefix, payload)
	}
	var body effectBody
	if err := json.Unmarshal([]byte(payload[len(prefix):]), &body); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	return body
}

// Every shipped preset has to survive the same validation the panel applies to
// anything else, or it is a button that silently does nothing.
func TestPresetsAreValid(t *testing.T) {
	for _, e := range effects {
		payload, err := buildEffectPayload(e, 300)
		if err != nil {
			t.Errorf("%s: %v", e.ID, err)
			continue
		}
		if len(payload) > maxPayloadBytes {
			t.Errorf("%s: payload %d bytes", e.ID, len(payload))
		}
		body := decodeBody(t, payload)
		if body.Mode != e.Mode {
			t.Errorf("%s: mode %q, want %q", e.ID, body.Mode, e.Mode)
		}
	}
}

func TestPresetIDsAndModesAreUnique(t *testing.T) {
	// The banner maps the mode the device reports back to a button, so two
	// presets sharing a mode would highlight the wrong one.
	ids := map[string]bool{}
	modes := map[string]bool{}
	for _, e := range effects {
		if ids[e.ID] {
			t.Errorf("duplicate id %q", e.ID)
		}
		if modes[e.Mode] {
			t.Errorf("duplicate mode %q", e.Mode)
		}
		ids[e.ID] = true
		modes[e.Mode] = true
	}
}

// Speed 0 is Cylon's slowest sweep, not an absent field. Dropping it would
// hand the firmware its default of 128 instead, which is a different effect.
func TestZeroSpeedIsSent(t *testing.T) {
	cylon, ok := findEffect("cylon")
	if !ok {
		t.Fatal("cylon preset missing")
	}
	if cylon.Speed != 0 {
		t.Fatalf("cylon speed is %d; this test is about the zero case", cylon.Speed)
	}
	payload, err := buildEffectPayload(cylon, 60)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(payload, `"speed":0`) {
		t.Errorf("speed omitted from %s", payload)
	}
}

func TestTimeoutClamping(t *testing.T) {
	plain, _ := findEffect("chase")
	capped, _ := findEffect("strobe")
	cappedMode, _ := findMode(capped.Mode)

	cases := []struct {
		name    string
		effect  Effect
		seconds int
		want    int
	}{
		{"until stopped stays zero", plain, 0, 0},
		{"ordinary duration passes through", plain, 3600, 3600},
		{"negative is treated as until stopped", plain, -5, 0},
		{"over the firmware ceiling is clamped", plain, 99999, maxTimeoutSeconds},
		// A strobe left running unbounded in a kitchen is the one case where
		// "until stopped" should not be taken literally. The cap lives on the
		// mode, so the builder inherits it along with the preset.
		{"capped mode clamps until-stopped", capped, 0, cappedMode.MaxSeconds},
		{"capped mode clamps a long run", capped, 3600, cappedMode.MaxSeconds},
		{"capped mode allows a shorter run", capped, 15, 15},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			payload, err := buildEffectPayload(tc.effect, tc.seconds)
			if err != nil {
				t.Fatal(err)
			}
			if got := decodeBody(t, payload).Timeout; got != tc.want {
				t.Errorf("timeout %d, want %d", got, tc.want)
			}
		})
	}
}

func TestRejectsBadEffects(t *testing.T) {
	base := Effect{Label: "test", Mode: "CHASE", Colors: []string{"#FF0000"},
		Speed: 128, Intensity: 128}

	with := func(fn func(*Effect)) Effect {
		e := base
		e.Colors = append([]string(nil), base.Colors...)
		fn(&e)
		return e
	}

	cases := map[string]Effect{
		"unknown mode":  with(func(e *Effect) { e.Mode = "DISCO" }),
		"no colours":    with(func(e *Effect) { e.Colors = nil }),
		"nine colours":  with(func(e *Effect) { e.Colors = make([]string, 9) }),
		"short hex":     with(func(e *Effect) { e.Colors = []string{"#F00"} }),
		"not hex":       with(func(e *Effect) { e.Colors = []string{"red"} }),
		"speed high":    with(func(e *Effect) { e.Speed = 256 }),
		"speed low":     with(func(e *Effect) { e.Speed = -1 }),
		"intensity low": with(func(e *Effect) { e.Intensity = -1 }),
	}

	for name, e := range cases {
		t.Run(name, func(t *testing.T) {
			if payload, err := buildEffectPayload(e, 60); err == nil {
				t.Errorf("accepted, produced %s", payload)
			}
		})
	}
}

// The firmware is case-insensitive about the mode but rejects a numeric one,
// and accepts a colour with or without the leading hash.
func TestAcceptedVariants(t *testing.T) {
	e := Effect{Label: "test", Mode: "chase", Colors: []string{"FF0000", "#00aaff"},
		Speed: 10, Intensity: 20}
	payload, err := buildEffectPayload(e, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got := decodeBody(t, payload).Mode; got != "CHASE" {
		t.Errorf("mode %q, want CHASE", got)
	}
}

// The builder greys out the three modes that force saturation and value to
// full, so the rule that decides which colours are safe has to be right.
func TestIsVivid(t *testing.T) {
	vivid := []string{"#FF0000", "#00FF00", "#0000FF", "#FFFF00", "#FF6600", "#00FFFF", "ff00ff"}
	flat := []string{"#FFFFFF", "#FFB6C1", "#FFD9A0", "#800000", "#C9B6FF", "#101010"}

	for _, c := range vivid {
		if !isVivid(c) {
			t.Errorf("%s should survive Blend/Flicker/Loop unchanged", c)
		}
	}
	for _, c := range flat {
		if isVivid(c) {
			t.Errorf("%s would be flattened, should not be reported as vivid", c)
		}
	}
	if isVivid("#F00") || isVivid("nonsense") || isVivid("") {
		t.Error("a malformed colour must not be reported as vivid")
	}

	if !AllVivid([]string{"#FF0000", "#0000FF"}) {
		t.Error("AllVivid rejected an all-vivid palette")
	}
	if AllVivid([]string{"#FF0000", "#FFB6C1"}) {
		t.Error("AllVivid accepted a palette with a pastel in it")
	}
}

// The palette's own vividness flags are what the UI gates on, so they have to
// agree with the rule rather than with whoever typed the list.
func TestPaletteFlags(t *testing.T) {
	vivid := 0
	for _, c := range palette {
		if c.Vivid != isVivid(c.Hex) {
			t.Errorf("%s (%s): Vivid is %v", c.Name, c.Hex, c.Vivid)
		}
		if c.Vivid {
			vivid++
		}
		if !hexColorRe.MatchString(c.Hex) {
			t.Errorf("%s: %q is not #RRGGBB", c.Name, c.Hex)
		}
	}
	// A palette of nothing but vivid colours would make the Blend/Flicker/Loop
	// gating dead code; one of nothing but pastels would make those three modes
	// unreachable. Both halves have to be there.
	if vivid == 0 || vivid == len(palette) {
		t.Errorf("%d of %d palette colours are vivid", vivid, len(palette))
	}
}

// Every mode the picker offers has to be one the payload builder accepts, or a
// button in the UI is a button that silently does nothing.
func TestModesAreUsable(t *testing.T) {
	seen := map[string]bool{}
	for _, m := range modes {
		if seen[m.ID] {
			t.Errorf("duplicate mode %q", m.ID)
		}
		seen[m.ID] = true

		if _, err := buildEffectPayload(Effect{
			Label: m.Label, Mode: m.ID, Colors: []string{"#FF0000"},
			Speed: 128, Intensity: 128,
		}, 60); err != nil {
			t.Errorf("%s: %v", m.ID, err)
		}
		if m.UsesIntensity && m.Means == "" {
			t.Errorf("%s uses intensity but does not say what it does", m.ID)
		}
	}
	if len(modes) != 9 {
		t.Errorf("got %d modes, want the firmware's 9", len(modes))
	}

	// Every preset's mode must be one the builder knows about, since the mode
	// now carries the timeout cap and the intensity rules.
	for _, e := range effects {
		if _, ok := findMode(e.Mode); !ok {
			t.Errorf("preset %s uses unknown mode %q", e.ID, e.Mode)
		}
	}
}

// The slider's top scales with the shortest strip, because a speed that reads
// as movement on 128 LEDs is a flash on 10.
func TestSpeedCeiling(t *testing.T) {
	cases := []struct{ leds, want int }{
		{0, 255},   // nothing has reported; do not clamp on a guess
		{10, 140},  // the dev board
		{5, 140},   // shorter than the dev board, same floor
		{240, 255}, // what the renderers were tuned for
		{400, 255},
	}
	for _, tc := range cases {
		if got := speedCeiling(tc.leds); got != tc.want {
			t.Errorf("speedCeiling(%d) = %d, want %d", tc.leds, got, tc.want)
		}
	}
	// In between, longer strips get a higher ceiling than shorter ones.
	if speedCeiling(60) <= speedCeiling(20) || speedCeiling(60) >= speedCeiling(200) {
		t.Error("the ceiling should rise with strip length")
	}
}

// SetCustomEffect repeats the 1-8 check rather than trusting the swatch row:
// nine colours are rejected outright by the firmware, silently.
func TestSetCustomEffectValidates(t *testing.T) {
	a := NewApp()
	a.cfg = &Config{Devices: []string{"tv"}}
	a.broker = NewBroker(a.cfg)

	nine := make([]string, 9)
	for i := range nine {
		nine[i] = "#FF0000"
	}

	cases := map[string]struct {
		mode   string
		colors []string
	}{
		"nine colours": {"SPARKLE", nine},
		"no colours":   {"SPARKLE", nil},
		"unknown mode": {"DISCO", []string{"#FF0000"}},
		"bad colour":   {"SPARKLE", []string{"#F00"}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			// Not connected, so "not connected to broker" would mean it got
			// past validation to the publish - which is itself a failure.
			msg := a.SetCustomEffect(tc.mode, tc.colors, 128, 128, 60)
			if msg == "" || msg == "not connected to broker" {
				t.Errorf("accepted, returned %q", msg)
			}
		})
	}
}
