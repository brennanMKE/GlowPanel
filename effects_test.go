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

func TestPresetIDsAreUnique(t *testing.T) {
	ids := map[string]bool{}
	for _, e := range effects {
		if ids[e.ID] {
			t.Errorf("duplicate id %q", e.ID)
		}
		ids[e.ID] = true
	}
}

// Modes used to be unique too, because the banner maps the mode the device
// reports back to a button and two presets sharing one would highlight the
// wrong button. Candle and Cyberpunk are both FLICKER, so that is now handled
// in applyEffect() instead: a report that agrees with the lit button leaves it
// alone. This test only pins the assumption that makes that work - every mode
// named by a preset is one the firmware actually has.
func TestPresetModesExist(t *testing.T) {
	for _, e := range effects {
		if _, ok := findMode(e.Mode); !ok {
			t.Errorf("%s: unknown mode %q", e.ID, e.Mode)
		}
	}
}

// Speed 0 is the slowest sweep, not an absent field. Dropping it would hand the
// firmware its default of 128 instead, which is a different effect. Cylon is
// still the case that matters - resolveSpeed hands it a literal 0 on a short
// strip - but the property is about the encoder, so this drives it directly
// rather than through whatever speed the preset currently carries.
func TestZeroSpeedIsSent(t *testing.T) {
	zero := Effect{Label: "zero", Mode: "SCAN", Colors: []string{"#FF0000"}, Speed: 0, Intensity: 255}
	payload, err := buildEffectPayload(zero, 60)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(payload, `"speed":0`) {
		t.Errorf("speed omitted from %s", payload)
	}
}

func TestTimeoutClamping(t *testing.T) {
	// Any uncapped preset does for the ordinary cases.
	plain, ok := findEffect("dolly")
	if !ok {
		t.Fatal("dolly preset missing")
	}
	// The cap lives on the mode, not on a preset, and no preset ships on STROBE
	// any more - the builder is now the only way to reach it. Building the
	// capped case by hand is the point rather than a workaround: it is exactly
	// what SetCustomEffect passes through.
	cappedMode, ok := findMode("STROBE")
	if !ok {
		t.Fatal("STROBE mode missing")
	}
	capped := Effect{Label: "capped", Mode: cappedMode.ID,
		Colors: []string{"#FFFFFF"}, Speed: 200, Intensity: 128}

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
	// Nine renderers shipped in GlowKitchen issue #0015; NEON, RAIN, TRAIL and
	// STACK were added in #0021. The count is pinned rather than left open so
	// that a mode added to the firmware and forgotten here - which would make
	// it unreachable from the builder - fails a test instead of going unnoticed.
	if len(modes) != 13 {
		t.Errorf("got %d modes, want the firmware's 13", len(modes))
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

// --- crossing rate -----------------------------------------------------------

// The numbers here are the firmware's, not ours: SCAN ticks every
// speedInterval(speed, 100, 4) ms and moves one LED per tick, so a sweep is
// numLeds of those. If GlowKitchen retunes a renderer, this is what fails.
func TestTraverseSpeedMatchesTheFirmwareInterval(t *testing.T) {
	scan, ok := findMode("SCAN")
	if !ok {
		t.Fatal("SCAN mode missing")
	}
	// speedInterval() from GlowKitchen src/effects.h.
	interval := func(speed, slow, fast int) int {
		return slow - ((slow-fast)*speed)/255
	}

	for _, leds := range []int{10, 30, 60, 144, 240, 300} {
		speed := traverseSpeed(scan, leds, 1100)
		if speed < 0 || speed > 255 {
			t.Fatalf("%d LEDs: speed %d outside 0-255", leds, speed)
		}
		sweep := interval(speed, scan.SlowMs, scan.FastMs) * leds
		// A 300-LED strip cannot be swept in 1100ms at the firmware's 4ms
		// floor, and a 10-LED one cannot be slowed past its 100ms ceiling;
		// both land close enough that the eye reads the same scanner.
		if sweep < 800 || sweep > 1400 {
			t.Errorf("%d LEDs: speed %d gives a %dms sweep, want ~1100ms", leds, speed, sweep)
		}
	}
}

// The bug this whole mechanism exists for: one speed byte, wildly different
// sweeps. Cylon at a fixed speed 0 took 24 seconds to cross 240 LEDs.
func TestCylonCrossesLongStripsAtTheSameRate(t *testing.T) {
	cylon, ok := findEffect("cylon")
	if !ok {
		t.Fatal("cylon preset missing")
	}
	if cylon.TraverseMs <= 0 {
		t.Fatal("cylon no longer asks for a crossing rate")
	}
	short := resolveSpeed(cylon, 10, 10)
	long := resolveSpeed(cylon, 240, 240)
	if long <= short {
		t.Errorf("240 LEDs got speed %d, 10 LEDs got %d; the long strip must move faster per LED", long, short)
	}

	// A 10-LED strip used to be pinned at speed 0 because the firmware's slow
	// end was 100ms and the target needed 110. The slow ends were widened after
	// bench testing on an 11-LED board, so short strips now reach the rate
	// rather than bottoming out short of it.
	scan, _ := findMode("SCAN")
	sweep := (scan.SlowMs - ((scan.SlowMs-scan.FastMs)*short)/255) * 10
	if sweep < 900 || sweep > 1300 {
		t.Errorf("10 LEDs: speed %d gives a %dms sweep, want ~%dms", short, sweep, cylon.TraverseMs)
	}
}

// The per-device path is the one presets actually take. Each strip is solved
// for on its own, so no length is a compromise against any other.
func TestSpeedForStripSolvesEachLengthOnItsOwn(t *testing.T) {
	for _, id := range []string{"cylon", "pacman", "tron", "tetris"} {
		e, ok := findEffect(id)
		if !ok {
			t.Fatalf("%s preset missing", id)
		}
		m, _ := findMode(e.Mode)
		for _, leds := range []int{11, 50, 60, 120, 128, 240} {
			speed := speedForStrip(e, leds)
			pass := (m.SlowMs - ((m.SlowMs-m.FastMs)*speed)/255) * leds
			lo, hi := e.TraverseMs*3/4, e.TraverseMs*5/4
			if pass >= lo && pass <= hi {
				continue
			}
			// Speed 0 is the firmware's slowest tick. A strip short enough that
			// even that cannot stretch the pass to the asked-for time is at the
			// renderer's floor, not at a bug: 11 LEDs times CHASE's 200ms slow
			// end is 2.2s, and Pac-Man wants 3s. Landing under the target is
			// the honest outcome; landing over it never is.
			if speed == 0 && pass < lo {
				continue
			}
			t.Errorf("%s at %d LEDs: speed %d gives %dms, want %dms",
				id, leds, speed, pass, e.TraverseMs)
		}
	}
}

// A preset with a fixed speed, or a length we do not have, must be left alone.
func TestSpeedForStripLeavesFixedPresetsAlone(t *testing.T) {
	for _, e := range effects {
		if e.TraverseMs > 0 {
			continue
		}
		if got := speedForStrip(e, 128); got != e.Speed {
			t.Errorf("%s: speed %d, want the written %d", e.ID, got, e.Speed)
		}
	}
	cylon, _ := findEffect("cylon")
	if got := speedForStrip(cylon, 0); got != cylon.Speed {
		t.Errorf("unknown length: speed %d, want the written %d", got, cylon.Speed)
	}
}

// A uniform fleet gets the rate the preset asked for, at every length. This is
// the assertion the traverseSpeed unit test does not make: resolveSpeed is
// where a clamp meant for the builder's slider can quietly undo the whole
// point, which is exactly what it did to a 60-LED strip.
func TestResolveSpeedHitsTheRateOnAUniformFleet(t *testing.T) {
	cylon, _ := findEffect("cylon")
	scan, _ := findMode("SCAN")
	interval := func(speed, slow, fast int) int { return slow - ((slow-fast)*speed)/255 }

	for _, leds := range []int{30, 60, 144, 240} {
		speed := resolveSpeed(cylon, leds, leds)
		sweep := interval(speed, scan.SlowMs, scan.FastMs) * leds
		if sweep < 800 || sweep > 1400 {
			t.Errorf("%d LEDs: speed %d gives a %dms sweep, want ~%dms",
				leds, speed, sweep, cylon.TraverseMs)
		}
	}
}

// One SET_EFFECT reaches every strip, so a speed solved for a 240-LED run must
// still not blink a 10-LED dev board on and off.
func TestResolveSpeedProtectsTheShortestStrip(t *testing.T) {
	cylon, _ := findEffect("cylon")
	scan, _ := findMode("SCAN")
	interval := func(speed, slow, fast int) int { return slow - ((slow-fast)*speed)/255 }

	speed := resolveSpeed(cylon, 240, 10)
	if sweep := interval(speed, scan.SlowMs, scan.FastMs) * 10; sweep < minTraverseMs {
		t.Errorf("speed %d sweeps the 10-LED strip in %dms, under the %dms floor",
			speed, sweep, minTraverseMs)
	}
	// ...and is still a large improvement on the 24s the fixed speed gave.
	if sweep := interval(speed, scan.SlowMs, scan.FastMs) * 240; sweep > 12000 {
		t.Errorf("240-LED sweep is %dms; the mixed-fleet compromise gave up too much", sweep)
	}
}

// A preset without a crossing rate, or a fleet that has not reported yet, must
// send exactly the speed the preset was written with.
func TestResolveSpeedLeavesFixedPresetsAlone(t *testing.T) {
	for _, e := range effects {
		if e.TraverseMs > 0 {
			continue
		}
		if got := resolveSpeed(e, 240, 240); got != e.Speed {
			t.Errorf("%s: speed %d, want the written %d", e.ID, got, e.Speed)
		}
	}
	cylon, _ := findEffect("cylon")
	if got := resolveSpeed(cylon, 0, 0); got != cylon.Speed {
		t.Errorf("nothing reported: speed %d, want the written %d", got, cylon.Speed)
	}
}

// FLICKER and BLEND force saturation and value to full, so a preset aimed at
// either has to be built from colours that already survive that.
func TestPresetsSurviveTheModesThatDiscardColour(t *testing.T) {
	for _, e := range effects {
		m, ok := findMode(e.Mode)
		if !ok || m.KeepsColors {
			continue
		}
		if !AllVivid(e.Colors) {
			t.Errorf("%s: %s flattens these colours; use vivid ones", e.ID, e.Mode)
		}
	}
}
