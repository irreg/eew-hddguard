package eew

import (
	"context"
	"encoding/json"
	"time"

	"github.com/gorilla/websocket"
)

// DefaultP2PQuakeURL is the public P2P地震情報 v2 WebSocket endpoint.
const DefaultP2PQuakeURL = "wss://api.p2pquake.net/v2/ws"

// codeEEWWarning is the P2P地震情報 message code for 緊急地震速報(警報).
const codeEEWWarning = 556

// P2PQuakeSource receives EEW warnings from the P2P地震情報 WebSocket API.
// Note: the feed only carries 警報-class EEW (predicted intensity 5弱+).
type P2PQuakeSource struct {
	URL  string
	Logf func(format string, args ...any)

	// ReadTimeout is the longest silence tolerated on the socket before
	// reconnecting. The server sends periodic pings, so a healthy
	// connection never goes silent this long. Defaults to 5 minutes.
	ReadTimeout time.Duration
	// MinBackoff/MaxBackoff bound the reconnect delay. Defaults 1s/60s.
	MinBackoff time.Duration
	MaxBackoff time.Duration
}

func (s *P2PQuakeSource) logf(format string, args ...any) {
	if s.Logf != nil {
		s.Logf(format, args...)
	}
}

// Run connects to the feed and delivers warning events to out, reconnecting
// with exponential backoff, until ctx is cancelled.
func (s *P2PQuakeSource) Run(ctx context.Context, out chan<- Event) error {
	minB := s.MinBackoff
	if minB <= 0 {
		minB = time.Second
	}
	maxB := s.MaxBackoff
	if maxB <= 0 {
		maxB = time.Minute
	}
	backoff := minB
	for {
		connected, err := s.runOnce(ctx, out)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if connected {
			backoff = minB
		}
		s.logf("eew: connection lost (%v); reconnecting in %s", err, backoff)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
		backoff *= 2
		if backoff > maxB {
			backoff = maxB
		}
	}
}

// runOnce runs a single connection until it fails. connected reports whether
// the dial succeeded (used to reset backoff).
func (s *P2PQuakeSource) runOnce(ctx context.Context, out chan<- Event) (connected bool, err error) {
	url := s.URL
	if url == "" {
		url = DefaultP2PQuakeURL
	}
	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second}
	conn, resp, err := dialer.DialContext(ctx, url, nil)
	if err != nil {
		return false, err
	}
	if resp != nil && resp.Body != nil {
		resp.Body.Close()
	}
	defer conn.Close()
	s.logf("eew: connected to %s", url)

	// Unblock ReadMessage when ctx is cancelled.
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			conn.Close()
		case <-done:
		}
	}()

	readTimeout := s.ReadTimeout
	if readTimeout <= 0 {
		readTimeout = 5 * time.Minute
	}
	extend := func() { conn.SetReadDeadline(time.Now().Add(readTimeout)) }
	extend()
	conn.SetPingHandler(func(appData string) error {
		extend()
		return conn.WriteControl(websocket.PongMessage, []byte(appData), time.Now().Add(10*time.Second))
	})

	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			return true, err
		}
		extend()
		ev, ok := ParseP2PQuake(data)
		if !ok {
			continue
		}
		select {
		case out <- ev:
		case <-ctx.Done():
			return true, ctx.Err()
		}
	}
}

type p2pEEWMessage struct {
	Code       int  `json:"code"`
	Test       bool `json:"test"`
	Cancelled  bool `json:"cancelled"`
	Earthquake struct {
		Hypocenter struct {
			Name      string  `json:"name"`
			Magnitude float64 `json:"magnitude"`
		} `json:"hypocenter"`
	} `json:"earthquake"`
	Issue struct {
		EventID string `json:"eventId"`
		Serial  string `json:"serial"`
	} `json:"issue"`
	Areas []struct {
		Pref string `json:"pref"`
		Name string `json:"name"`
	} `json:"areas"`
}

// ParseP2PQuake decodes one P2P地震情報 WebSocket message. It returns
// ok=false for messages that are not EEW warnings (code 556) or that fail
// to decode.
func ParseP2PQuake(data []byte) (Event, bool) {
	var m p2pEEWMessage
	if err := json.Unmarshal(data, &m); err != nil {
		return Event{}, false
	}
	if m.Code != codeEEWWarning {
		return Event{}, false
	}
	ev := Event{
		Source:     "p2pquake",
		EventID:    m.Issue.EventID,
		Serial:     m.Issue.Serial,
		Test:       m.Test,
		Cancelled:  m.Cancelled,
		Hypocenter: m.Earthquake.Hypocenter.Name,
		Magnitude:  m.Earthquake.Hypocenter.Magnitude,
	}
	for _, a := range m.Areas {
		ev.Prefs = append(ev.Prefs, a.Pref)
	}
	return ev, true
}
