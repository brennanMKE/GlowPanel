package main

import (
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
