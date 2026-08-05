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

type Broker struct {
	cfg    *Config
	client mqtt.Client

	mu    sync.RWMutex
	state map[string]*DeviceState
	seen  map[string]time.Time
}

func NewBroker(cfg *Config) *Broker {
	return &Broker{
		cfg:   cfg,
		state: make(map[string]*DeviceState),
		seen:  make(map[string]time.Time),
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
		// waiting for the next spontaneous publish.
		c.Publish("lights/all/cmd", 1, false, "STATUS")
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

	b.mu.Lock()
	defer b.mu.Unlock()
	b.state[device] = &DeviceState{
		Name:       device,
		Brightness: p.Brightness,
		Percent:    RawToPercent(p.Brightness),
		Theme:      p.Theme,
		Enabled:    p.LedsEnabled,
		Firmware:   p.FirmwareVersion,
	}
	b.seen[device] = time.Now()
}

// Publish sends one command to every configured device. It reports the first
// error but still attempts the rest, so one unreachable strip does not stop the
// others from responding.
func (b *Broker) Publish(payload string, retain bool) error {
	if b.client == nil || !b.client.IsConnected() {
		return fmt.Errorf("not connected to broker")
	}
	var firstErr error
	for _, d := range b.cfg.Devices {
		tok := b.client.Publish("lights/"+d+"/cmd", 1, retain, payload)
		if !tok.WaitTimeout(5*time.Second) && firstErr == nil {
			firstErr = fmt.Errorf("timed out publishing to %s", d)
		} else if err := tok.Error(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (b *Broker) RequestStatus() {
	if b.client != nil && b.client.IsConnected() {
		b.client.Publish("lights/all/cmd", 1, false, "STATUS")
	}
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
