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
		// "until stopped" should not be taken literally.
		{"capped preset clamps until-stopped", capped, 0, capped.MaxSeconds},
		{"capped preset clamps a long run", capped, 3600, capped.MaxSeconds},
		{"capped preset allows a shorter run", capped, 15, 15},
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
