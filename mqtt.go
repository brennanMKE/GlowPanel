package main

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// DeviceState is the subset of a strip's JSON status payload the UI cares about.
type DeviceState struct {
	Name        string `json:"name"`
	Brightness  int    `json:"brightness"`
	Percent     int    `json:"percent"`
	Theme       string `json:"theme"`
	Enabled     bool   `json:"enabled"`
	Firmware    string `json:"firmware"`
	LastSeenAgo int    `json:"lastSeenAgo"` // seconds
}

// statusPayload matches what the firmware publishes on lights/<device>/state.
type statusPayload struct {
	Brightness      int    `json:"brightness"`
	Theme           string `json:"theme"`
	LedsEnabled     bool   `json:"ledsEnabled"`
	FirmwareVersion string `json:"firmwareVersion"`
}

// statusQuery is the only payload GlowPanel ever publishes on its own, without
// somebody pressing a button. The firmware answers it by publishing its state
// and changes nothing about the strips: no theme, no brightness, no power. Keep
// it that way. Anything that alters the lights goes through Publish, which is
// only ever reached from a button press.
const statusQuery = "STATUS"

// minQueryGap throttles status requests so several triggers arriving together -
// the window regaining focus, a reconnect, the slow background refresh - result
// in one message on the wire instead of a burst.
const minQueryGap = 15 * time.Second

type Broker struct {
	cfg    *Config
	client mqtt.Client

	mu        sync.RWMutex
	state     map[string]*DeviceState
	seen      map[string]time.Time
	lastQuery time.Time

	// onChange fires when something the UI renders actually changed, so the app
	// can push an update instead of the frontend asking on a timer.
	onChange func()
}

func NewBroker(cfg *Config) *Broker {
	return &Broker{
		cfg:   cfg,
		state: make(map[string]*DeviceState),
		seen:  make(map[string]time.Time),
	}
}

// SetOnChange registers the callback fired from MQTT goroutines when the cached
// view of the strips changes. Set it before Connect.
func (b *Broker) SetOnChange(fn func()) { b.onChange = fn }

func (b *Broker) notify() {
	if b.onChange != nil {
		b.onChange()
	}
}

func (b *Broker) Connect() error {
	opts := mqtt.NewClientOptions().
		AddBroker(fmt.Sprintf("tcp://%s:%s", b.cfg.Broker, b.cfg.Port)).
		SetClientID(fmt.Sprintf("glowpanel-%d", time.Now().UnixNano())).
		SetUsername(b.cfg.User).
		SetPassword(b.cfg.Password).
		SetAutoReconnect(true).
		SetConnectRetry(true).
		SetConnectRetryInterval(5 * time.Second).
		SetConnectTimeout(10 * time.Second)

	// Re-subscribe on every (re)connect, not just the first, so a broker restart
	// does not silently leave the panel showing stale state forever.
	opts.OnConnect = func(c mqtt.Client) {
		if tok := c.Subscribe("lights/+/state", 0, b.onState); tok.Wait() && tok.Error() != nil {
			log.Printf("subscribe failed: %v", tok.Error())
			return
		}
		// Ask everyone to report in so the UI populates immediately rather than
		// waiting for the next spontaneous publish. Forced past the throttle:
		// a reconnect means our cached state may be stale.
		b.query(c, true)
		b.notify()
	}

	// The connection dot is part of what the UI renders, so a drop is a change
	// worth pushing rather than something the frontend has to notice on a timer.
	opts.OnConnectionLost = func(_ mqtt.Client, err error) {
		log.Printf("mqtt connection lost: %v", err)
		b.notify()
	}

	client := mqtt.NewClient(opts)
	b.mu.Lock()
	b.client = client
	b.mu.Unlock()
	tok := client.Connect()
	if !tok.WaitTimeout(12 * time.Second) {
		return fmt.Errorf("timed out connecting to %s:%s", b.cfg.Broker, b.cfg.Port)
	}
	return tok.Error()
}

func (b *Broker) onState(_ mqtt.Client, msg mqtt.Message) {
	parts := strings.Split(msg.Topic(), "/")
	if len(parts) < 3 {
		return
	}
	device := parts[1]

	var p statusPayload
	if err := json.Unmarshal(msg.Payload(), &p); err != nil {
		return
	}

	next := DeviceState{
		Name:       device,
		Brightness: p.Brightness,
		Percent:    RawToPercent(p.Brightness),
		Theme:      p.Theme,
		Enabled:    p.LedsEnabled,
		Firmware:   p.FirmwareVersion,
	}

	b.mu.Lock()
	prev, known := b.state[device]
	// LastSeenAgo is derived in Snapshot and left zero here, so the structs
	// compare on the fields that actually come off the wire.
	changed := !known || *prev != next
	b.state[device] = &next
	b.seen[device] = time.Now()
	b.mu.Unlock()

	// A strip that re-reports the same values is not news; staying quiet here is
	// what keeps the UI idle when nothing is happening.
	if changed {
		b.notify()
	}
}

// Publish sends one command to every configured device. It reports the first
// error but still attempts the rest, so one unreachable strip does not stop the
// others from responding.
//
// This is the only path that can change what the strips are doing, and it must
// stay that way: it is called from button presses alone, never from anything on
// a timer. Background refreshes use RequestStatus.
func (b *Broker) Publish(payload string, retain bool) error {
	client := b.currentClient()
	if client == nil || !client.IsConnected() {
		return fmt.Errorf("not connected to broker")
	}
	var firstErr error
	for _, d := range b.cfg.Devices {
		tok := client.Publish("lights/"+d+"/cmd", 1, retain, payload)
		if !tok.WaitTimeout(5*time.Second) && firstErr == nil {
			firstErr = fmt.Errorf("timed out publishing to %s", d)
		} else if err := tok.Error(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// RequestStatus asks every strip to report in. It is read-only - the payload is
// fixed to statusQuery and can never carry a theme, a brightness or a power
// command - and it is published without the retain flag, so nothing is left on
// the broker for a device to replay and act on when it reconnects.
//
// Returns false when the request was throttled or the broker is unreachable.
func (b *Broker) RequestStatus() bool { return b.query(b.currentClient(), false) }

// query takes the client explicitly so the OnConnect handler, which runs on a
// paho goroutine, can use the client it was handed rather than racing the field.
func (b *Broker) query(c mqtt.Client, force bool) bool {
	if c == nil || !c.IsConnected() {
		return false
	}

	b.mu.Lock()
	if !force && time.Since(b.lastQuery) < minQueryGap {
		b.mu.Unlock()
		return false
	}
	b.lastQuery = time.Now()
	b.mu.Unlock()

	c.Publish("lights/all/cmd", 1, false, statusQuery)
	return true
}

// Snapshot returns the configured devices in config order, so the UI list does
// not reshuffle as messages arrive.
func (b *Broker) Snapshot() []DeviceState {
	b.mu.RLock()
	defer b.mu.RUnlock()

	out := make([]DeviceState, 0, len(b.cfg.Devices))
	for _, d := range b.cfg.Devices {
		s, ok := b.state[d]
		if !ok {
			out = append(out, DeviceState{Name: d, LastSeenAgo: -1})
			continue
		}
		copy := *s
		copy.LastSeenAgo = int(time.Since(b.seen[d]).Seconds())
		out = append(out, copy)
	}
	return out
}

func (b *Broker) Connected() bool {
	client := b.currentClient()
	return client != nil && client.IsConnected()
}

func (b *Broker) Disconnect() {
	client := b.currentClient()
	if client != nil && client.IsConnected() {
		client.Disconnect(250)
	}
}

func (b *Broker) currentClient() mqtt.Client {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.client
}
