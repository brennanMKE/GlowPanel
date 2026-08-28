package main

import (
	"strings"
	"sync"
	"testing"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// These cover the one rule the panel must not break: nothing it sends on its
// own - on a timer, on reconnect, on regaining focus - may change the strips.

type pub struct {
	topic   string
	retain  bool
	payload string
}

type fakeClient struct {
	mqtt.Client
	mu   sync.Mutex
	pubs []pub
}

func (f *fakeClient) IsConnected() bool { return true }
func (f *fakeClient) Publish(topic string, _ byte, retain bool, payload any) mqtt.Token {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pubs = append(f.pubs, pub{topic, retain, payload.(string)})
	return &mqtt.DummyToken{}
}

type fakeMsg struct {
	mqtt.Message
	topic   string
	payload []byte
}

func (m fakeMsg) Topic() string   { return m.topic }
func (m fakeMsg) Payload() []byte { return m.payload }

// commands returns everything published that was not a STATUS query, so the
// confirmation poll an effect command schedules cannot race into an assertion
// about what the button itself sent.
func (f *fakeClient) commands() []pub {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []pub
	for _, p := range f.pubs {
		if p.payload != statusQuery {
			out = append(out, p)
		}
	}
	return out
}

func TestQueryIsReadOnlyAndThrottled(t *testing.T) {
	c := &fakeClient{}
	b := NewBroker(&Config{Devices: []string{"tv"}})
	b.client = c

	if !b.RequestStatus() {
		t.Fatal("first request should go out")
	}
	if b.RequestStatus() {
		t.Fatal("second request inside the throttle window should be suppressed")
	}
	if !b.query(c, true) {
		t.Fatal("forced request should bypass the throttle")
	}

	if len(c.pubs) != 2 {
		t.Fatalf("got %d publishes, want 2: %+v", len(c.pubs), c.pubs)
	}
	for _, p := range c.pubs {
		if p.payload != "STATUS" {
			t.Errorf("background publish carried %q, want STATUS", p.payload)
		}
		if p.retain {
			t.Errorf("background publish to %s was retained", p.topic)
		}
	}
}

func TestOnStateNotifiesOnlyOnChange(t *testing.T) {
	b := NewBroker(&Config{Devices: []string{"tv"}})
	n := 0
	b.SetOnChange(func() { n++ })

	msg := fakeMsg{topic: "lights/tv/state",
		payload: []byte(`{"brightness":225,"theme":"Green","ledsEnabled":true,"firmwareVersion":"1.0"}`)}

	b.onState(nil, msg)
	b.onState(nil, msg)
	b.onState(nil, msg)
	if n != 1 {
		t.Fatalf("repeated identical reports fired %d notifications, want 1", n)
	}

	b.onState(nil, fakeMsg{topic: "lights/tv/state",
		payload: []byte(`{"brightness":40,"theme":"Green","ledsEnabled":true,"firmwareVersion":"1.0"}`)})
	if n != 2 {
		t.Fatalf("changed report fired %d notifications total, want 2", n)
	}

	if got := b.Snapshot()[0].Percent; got != RawToPercent(40) {
		t.Fatalf("snapshot percent %d, want %d", got, RawToPercent(40))
	}
}

// A retained command is redelivered to every device on every reconnect,
// forever, by anything subscribed to the topic. That is merely wrong for a
// theme; for an effect carrying a timeout it also restarts the countdown on
// each reconnect. Effects go out unretained regardless of what the config says
// about the rest.
func TestEffectsAreNeverRetained(t *testing.T) {
	c := &fakeClient{}
	a := NewApp()
	a.cfg = &Config{Devices: []string{"tv"}, Retain: true}
	a.broker = NewBroker(a.cfg)
	a.broker.client = c

	if msg := a.SetEffect("chase", 300); msg != "" {
		t.Fatalf("SetEffect: %s", msg)
	}
	if msg := a.ClearEffect(); msg != "" {
		t.Fatalf("ClearEffect: %s", msg)
	}

	pubs := c.commands()
	if len(pubs) != 2 {
		t.Fatalf("got %d publishes, want 2: %+v", len(pubs), pubs)
	}
	for _, p := range pubs {
		if p.retain {
			t.Errorf("%q was published retained", p.payload)
		}
		// Broadcast, not per-device: one message reaches every strip on the
		// broker, including one glow.conf does not name - a strip being staged
		// onto new firmware, say.
		if p.topic != broadcastTopic {
			t.Errorf("published to %s, want %s", p.topic, broadcastTopic)
		}
	}
	if !strings.HasPrefix(pubs[0].payload, "SET_EFFECT:{") {
		t.Errorf("first publish was %q", pubs[0].payload)
	}
	if pubs[1].payload != "CLEAR_EFFECT" {
		t.Errorf("second publish was %q", pubs[1].payload)
	}
}

// SetTheme used to hard-code retain: true, which is how a FOREST ended up
// sitting on the broker snapping a strip back to Forest seconds after every
// effect was applied. It follows the configured flag now, like the others.
func TestThemeHonoursConfiguredRetain(t *testing.T) {
	for _, retain := range []bool{true, false} {
		c := &fakeClient{}
		a := NewApp()
		a.cfg = &Config{Devices: []string{"tv"}, Retain: retain}
		a.broker = NewBroker(a.cfg)
		a.broker.client = c

		if msg := a.SetTheme("FOREST"); msg != "" {
			t.Fatalf("SetTheme: %s", msg)
		}
		pubs := c.commands()
		if len(pubs) != 1 || pubs[0].retain != retain {
			t.Errorf("Retain=%v produced %+v", retain, pubs)
		}
		if len(pubs) == 1 && pubs[0].topic != broadcastTopic {
			t.Errorf("published to %s, want %s", pubs[0].topic, broadcastTopic)
		}
	}
}

// The custom object is absent whenever the theme is not "Custom". That is the
// normal state, not an error, and it must not leave a stale effect on screen.
func TestCustomEffectReporting(t *testing.T) {
	b := NewBroker(&Config{Devices: []string{"tv"}})

	b.onState(nil, fakeMsg{topic: "lights/tv/state",
		payload: []byte(`{"brightness":113,"theme":"Custom","ledsEnabled":true,"numLeds":10,
			"custom":{"mode":"SPARKLE","timeoutRemaining":56}}`)})

	d := b.Snapshot()[0]
	if d.EffectMode != "SPARKLE" || d.EffectRemaining != 56 || d.NumLeds != 10 {
		t.Fatalf("effect not read back: %+v", d)
	}
	if !b.EffectActive() {
		t.Error("EffectActive false while an effect is running")
	}

	// -1 is an effect with no timeout, never null and never a missing key.
	b.onState(nil, fakeMsg{topic: "lights/tv/state",
		payload: []byte(`{"theme":"Custom","ledsEnabled":true,"custom":{"mode":"PULSE","timeoutRemaining":-1}}`)})
	if got := b.Snapshot()[0].EffectRemaining; got != -1 {
		t.Errorf("remaining %d, want -1", got)
	}

	b.onState(nil, fakeMsg{topic: "lights/tv/state",
		payload: []byte(`{"brightness":113,"theme":"Forest","ledsEnabled":true}`)})
	if d := b.Snapshot()[0]; d.EffectMode != "" || d.EffectRemaining != 0 {
		t.Errorf("effect survived a plain theme report: %+v", d)
	}
	if b.EffectActive() {
		t.Error("EffectActive true after the effect ended")
	}
}

// Effects are broadcast, so the strip that picks one up may be one glow.conf
// never named - during a staged firmware rollout that is the normal case. The
// banner has to follow the effect, not the configured device list.
func TestRunningEffectSeesUnconfiguredDevices(t *testing.T) {
	b := NewBroker(&Config{Devices: []string{"tv"}})

	b.onState(nil, fakeMsg{topic: "lights/84fce68774a4/state",
		payload: []byte(`{"theme":"Custom","ledsEnabled":true,"numLeds":10,
			"custom":{"mode":"SPARKLE","timeoutRemaining":56}}`)})

	if !b.EffectActive() {
		t.Error("an effect on an unconfigured strip went unnoticed")
	}
	mode, remaining := b.RunningEffect()
	if mode != "SPARKLE" || remaining != 56 {
		t.Errorf("RunningEffect() = %q, %d; want SPARKLE, 56", mode, remaining)
	}

	// Snapshot still covers only the configured strips: the chips in the footer
	// are the room, and should not sprout a row for a bare MAC address.
	if snap := b.Snapshot(); len(snap) != 1 || snap[0].Name != "tv" {
		t.Errorf("Snapshot grew beyond the configured devices: %+v", snap)
	}
}

// One command, one message. The panel used to loop over the names in glow.conf
// and publish an identical copy to each, which is both five times the traffic
// and silent towards any strip the config does not happen to list.
func TestOneMessagePerCommand(t *testing.T) {
	c := &fakeClient{}
	b := NewBroker(&Config{Devices: []string{"tv", "desk", "kitchen"}})
	b.client = c

	if err := b.Publish("FOREST", false); err != nil {
		t.Fatal(err)
	}
	if len(c.pubs) != 1 {
		t.Fatalf("three configured devices produced %d publishes, want 1: %+v",
			len(c.pubs), c.pubs)
	}
	if c.pubs[0].topic != broadcastTopic {
		t.Errorf("published to %s, want %s", c.pubs[0].topic, broadcastTopic)
	}
}

// A theme arriving on the broadcast topic is ignored by a strip running an
// effect, so pressing a theme button has to take the effect down first or the
// button silently does nothing - the one way out of an effect for anyone who
// has not found the Stop button.
func TestThemePressClearsARunningEffect(t *testing.T) {
	c := &fakeClient{}
	a := NewApp()
	a.cfg = &Config{Devices: []string{"tv"}, Retain: false}
	a.broker = NewBroker(a.cfg)
	a.broker.client = c

	a.broker.onState(nil, fakeMsg{topic: "lights/84fce68774a4/state",
		payload: []byte(`{"theme":"Custom","ledsEnabled":true,
			"custom":{"mode":"SPARKLE","timeoutRemaining":56}}`)})

	if msg := a.SetTheme("FOREST"); msg != "" {
		t.Fatalf("SetTheme: %s", msg)
	}

	pubs := c.commands()
	if len(pubs) != 2 {
		t.Fatalf("got %d publishes, want CLEAR_EFFECT then FOREST: %+v", len(pubs), pubs)
	}
	if pubs[0].payload != "CLEAR_EFFECT" {
		t.Errorf("first publish was %q, want CLEAR_EFFECT", pubs[0].payload)
	}
	if pubs[0].retain {
		t.Error("the clear was published retained")
	}
	if pubs[1].payload != "FOREST" {
		t.Errorf("second publish was %q, want FOREST", pubs[1].payload)
	}

	// With nothing running the theme goes out on its own, no spurious clear.
	c2 := &fakeClient{}
	a2 := NewApp()
	a2.cfg = a.cfg
	a2.broker = NewBroker(a2.cfg)
	a2.broker.client = c2
	if msg := a2.SetTheme("FOREST"); msg != "" {
		t.Fatalf("SetTheme: %s", msg)
	}
	if pubs := c2.commands(); len(pubs) != 1 || pubs[0].payload != "FOREST" {
		t.Errorf("idle theme press produced %+v", pubs)
	}
}
