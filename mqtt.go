package main

import (
	"encoding/json"
	"fmt"
	"log"
	"sort"
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
	NumLeds     int    `json:"numLeds"`

	// A running custom effect. EffectMode is empty when none is running - the
	// firmware omits the whole "custom" object unless the theme is "Custom",
	// and absence means no effect rather than an error. EffectRemaining is
	// whole seconds, or -1 for an effect with no timeout at all.
	EffectMode      string `json:"effectMode"`
	EffectRemaining int    `json:"effectRemaining"`
}

// statusPayload matches what the firmware publishes on lights/<device>/state.
type statusPayload struct {
	Brightness      int    `json:"brightness"`
	Theme           string `json:"theme"`
	LedsEnabled     bool   `json:"ledsEnabled"`
	FirmwareVersion string `json:"firmwareVersion"`
	NumLeds         int    `json:"numLeds"`

	// Present only while a custom effect is running, which is why it is a
	// pointer: a nil Custom is the normal state, not a missing field to warn
	// about. There is deliberately no colours field here - the panel that sent
	// the effect keeps its own palette; it cannot be recovered from the device.
	Custom *customPayload `json:"custom"`
}

type customPayload struct {
	Mode string `json:"mode"`
	// TimeoutRemaining is whole seconds rounded up, and -1 - never null, never
	// absent while custom is present - for an effect running until changed.
	TimeoutRemaining int `json:"timeoutRemaining"`
}

// statusQuery is the only payload GlowPanel ever publishes on its own, without
// somebody pressing a button. The firmware answers it by publishing its state
// and changes nothing about the strips: no theme, no brightness, no power. Keep
// it that way. Anything that alters the lights goes through Publish, which is
// only ever reached from a button press.
const statusQuery = "STATUS"

// broadcastTopic is where everything the panel sends goes: commands and the
// STATUS query alike. Every strip subscribes to it, so nothing depends on the
// panel having been told a device's name.
const broadcastTopic = "lights/all/cmd"

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

	b.client = mqtt.NewClient(opts)
	tok := b.client.Connect()
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
		NumLeds:    p.NumLeds,
	}
	if p.Custom != nil {
		next.EffectMode = p.Custom.Mode
		next.EffectRemaining = p.Custom.TimeoutRemaining
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

// Publish sends one command to lights/all/cmd, which every strip on the broker
// subscribes to. One message, not one per device: the panel used to loop over
// the names in glow.conf and send five identical copies, which meant a strip
// the config did not happen to list - one being staged onto new firmware, say -
// never heard anything at all.
//
// This is the only path that can change what the strips are doing, and it must
// stay that way: it is called from button presses alone, never from anything on
// a timer. Background refreshes use RequestStatus.
func (b *Broker) Publish(payload string, retain bool) error {
	if b.client == nil || !b.client.IsConnected() {
		return fmt.Errorf("not connected to broker")
	}
	tok := b.client.Publish(broadcastTopic, 1, retain, payload)
	if !tok.WaitTimeout(5 * time.Second) {
		return fmt.Errorf("timed out publishing to %s", broadcastTopic)
	}
	return tok.Error()
}

// RequestStatus asks every strip to report in. It is read-only - the payload is
// fixed to statusQuery and can never carry a theme, a brightness or a power
// command - and it is published without the retain flag, so nothing is left on
// the broker for a device to replay and act on when it reconnects.
//
// Returns false when the request was throttled or the broker is unreachable.
func (b *Broker) RequestStatus() bool { return b.query(b.client, false) }

// RequestStatusNow is RequestStatus without the throttle, for the two moments
// where waiting up to minQueryGap would show the user something stale: just
// after an effect command, to confirm it landed, and every few seconds while
// one is counting down. Still read-only, still unretained.
func (b *Broker) RequestStatusNow() bool { return b.query(b.client, true) }

// EffectActive reports whether any strip last said it was running an effect.
func (b *Broker) EffectActive() bool {
	mode, _ := b.RunningEffect()
	return mode != ""
}

// RunningEffect reports the effect any strip on the broker is running, not just
// the ones named in glow.conf. Effects are broadcast to lights/all/cmd, so the
// strip that answers may well be one the panel does not otherwise track - it
// still needs a banner and a working Stop button.
//
// An empty mode means nothing is running. Remaining is -1 for an effect with no
// timeout. Devices are scanned in name order so two strips running different
// effects produce a stable answer rather than a flickering one.
func (b *Broker) RunningEffect() (string, int) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	names := make([]string, 0, len(b.state))
	for name, s := range b.state {
		if s.EffectMode != "" {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return "", 0
	}
	sort.Strings(names)
	s := b.state[names[0]]
	return s.EffectMode, s.EffectRemaining
}

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

	c.Publish(broadcastTopic, 1, false, statusQuery)
	return true
}

// MinNumLeds returns the shortest strip that has reported a length, or 0 when
// none has. Effects are broadcast to every strip at once, so a speed that suits
// a 128-LED run may be a blur on a 10-LED one; the shortest is the constraint.
func (b *Broker) MinNumLeds() int {
	b.mu.RLock()
	defer b.mu.RUnlock()

	min := 0
	for _, s := range b.state {
		if s.NumLeds <= 0 {
			continue
		}
		if min == 0 || s.NumLeds < min {
			min = s.NumLeds
		}
	}
	return min
}

// StripLengths returns every device that has reported a length, keyed by the
// name it reports under. That includes devices absent from glow.conf - a bench
// board, say - because a strip on the broker is a strip an effect reaches.
func (b *Broker) StripLengths() map[string]int {
	b.mu.RLock()
	defer b.mu.RUnlock()

	out := make(map[string]int, len(b.state))
	for name, s := range b.state {
		if s.NumLeds > 0 {
			out[name] = s.NumLeds
		}
	}
	return out
}

// PublishTo sends one command to a single device rather than to every strip.
// Used only for effects whose speed has to be solved per strip; everything else
// still goes out on the broadcast topic, which needs no device names at all.
func (b *Broker) PublishTo(device, payload string, retain bool) error {
	if b.client == nil || !b.client.IsConnected() {
		return fmt.Errorf("not connected to broker")
	}
	topic := "lights/" + device + "/cmd"
	tok := b.client.Publish(topic, 1, retain, payload)
	if !tok.WaitTimeout(5 * time.Second) {
		return fmt.Errorf("timed out publishing to %s", topic)
	}
	return tok.Error()
}

// MaxNumLeds returns the longest strip that has reported a length, or 0 when
// none has. It is the counterpart to MinNumLeds: the shortest strip decides how
// fast an effect may safely move, and the longest decides how fast it has to
// move to cross in a given time. Both are needed to turn a preset's requested
// crossing rate into one broadcast speed byte.
func (b *Broker) MaxNumLeds() int {
	b.mu.RLock()
	defer b.mu.RUnlock()

	max := 0
	for _, s := range b.state {
		if s.NumLeds > max {
			max = s.NumLeds
		}
	}
	return max
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
	return b.client != nil && b.client.IsConnected()
}

func (b *Broker) Disconnect() {
	if b.client != nil && b.client.IsConnected() {
		b.client.Disconnect(250)
	}
}
