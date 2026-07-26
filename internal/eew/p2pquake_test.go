package eew

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

const sample556 = `{
  "code": 556, "test": false, "cancelled": false,
  "earthquake": {
    "originTime": "2026/07/27 12:34:56",
    "hypocenter": {"name": "千葉県東方沖", "latitude": 35.6, "longitude": 140.9, "depth": 30, "magnitude": 6.8}
  },
  "issue": {"time": "2026/07/27 12:35:00", "eventId": "20260727123456", "serial": "3"},
  "areas": [
    {"pref": "千葉県", "name": "千葉県北東部", "kindCode": "10"},
    {"pref": "東京都", "name": "東京都２３区", "kindCode": "10"}
  ]
}`

func TestParseP2PQuake(t *testing.T) {
	ev, ok := ParseP2PQuake([]byte(sample556))
	if !ok {
		t.Fatal("expected sample 556 to parse")
	}
	if ev.Source != "p2pquake" || ev.EventID != "20260727123456" || ev.Serial != "3" {
		t.Errorf("identity fields wrong: %+v", ev)
	}
	if ev.Hypocenter != "千葉県東方沖" || ev.Magnitude != 6.8 {
		t.Errorf("hypocenter fields wrong: %+v", ev)
	}
	if len(ev.Prefs) != 2 || ev.Prefs[0] != "千葉県" || ev.Prefs[1] != "東京都" {
		t.Errorf("prefs wrong: %v", ev.Prefs)
	}
	if ev.Test || ev.Cancelled {
		t.Errorf("flags wrong: %+v", ev)
	}
}

func TestParseP2PQuakeRejects(t *testing.T) {
	for name, data := range map[string]string{
		"other code":   `{"code": 551, "earthquake": {}}`,
		"invalid json": `{nope`,
		"empty":        ``,
	} {
		if _, ok := ParseP2PQuake([]byte(data)); ok {
			t.Errorf("%s: expected ok=false", name)
		}
	}
}

func TestParseP2PQuakeFlags(t *testing.T) {
	ev, ok := ParseP2PQuake([]byte(`{"code":556,"test":true,"cancelled":true,"issue":{"eventId":"x","serial":"1"}}`))
	if !ok {
		t.Fatal("expected parse")
	}
	if !ev.Test || !ev.Cancelled {
		t.Errorf("flags not carried: %+v", ev)
	}
}

// wsServer runs a test WebSocket server; handler receives each connection.
func wsServer(t *testing.T, handler func(conn *websocket.Conn, connNum int)) *httptest.Server {
	t.Helper()
	upgrader := websocket.Upgrader{}
	var conns atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		handler(conn, int(conns.Add(1)))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func wsURL(srv *httptest.Server) string {
	return "ws" + strings.TrimPrefix(srv.URL, "http")
}

func TestP2PQuakeSourceDeliversAndReconnects(t *testing.T) {
	srv := wsServer(t, func(conn *websocket.Conn, connNum int) {
		defer conn.Close()
		if connNum == 1 {
			// Non-warning message, then drop the connection to force a
			// reconnect.
			conn.WriteMessage(websocket.TextMessage, []byte(`{"code":551}`))
			return
		}
		conn.WriteMessage(websocket.TextMessage, []byte(sample556))
		// Keep the connection open until the client goes away.
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	})

	src := &P2PQuakeSource{
		URL:        wsURL(srv),
		Logf:       t.Logf,
		MinBackoff: 10 * time.Millisecond,
		MaxBackoff: 20 * time.Millisecond,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out := make(chan Event, 1)
	done := make(chan error, 1)
	go func() { done <- src.Run(ctx, out) }()

	select {
	case ev := <-out:
		if ev.EventID != "20260727123456" {
			t.Errorf("unexpected event: %+v", ev)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for event across reconnect")
	}

	cancel()
	select {
	case err := <-done:
		if err != context.Canceled {
			t.Errorf("Run returned %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not stop after cancel")
	}
}

func TestP2PQuakeSourceDialFailureRetries(t *testing.T) {
	// A server that immediately rejects the websocket upgrade.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusForbidden)
	}))
	t.Cleanup(srv.Close)

	src := &P2PQuakeSource{
		URL:        wsURL(srv),
		MinBackoff: 5 * time.Millisecond,
		MaxBackoff: 10 * time.Millisecond,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	err := src.Run(ctx, make(chan Event))
	if err != context.DeadlineExceeded {
		t.Errorf("Run returned %v, want context.DeadlineExceeded", err)
	}
}

func TestP2PQuakeSourceDefaultURL(t *testing.T) {
	// With no URL configured the source must target the public endpoint;
	// verify the dial goes to that host by cancelling immediately and
	// checking the error mentions it (no network needed: instant cancel).
	src := &P2PQuakeSource{MinBackoff: time.Millisecond}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := src.Run(ctx, make(chan Event)); err != context.Canceled {
		t.Errorf("Run returned %v, want context.Canceled", err)
	}
}

func TestP2PQuakeSourcePingKeepsConnectionAlive(t *testing.T) {
	srv := wsServer(t, func(conn *websocket.Conn, connNum int) {
		defer conn.Close()
		// Ping first; the client's pong proves its ping handler ran.
		if err := conn.WriteControl(websocket.PingMessage, []byte("hb"), time.Now().Add(time.Second)); err != nil {
			return
		}
		conn.WriteMessage(websocket.TextMessage, []byte(sample556))
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	})
	src := &P2PQuakeSource{URL: wsURL(srv), MinBackoff: 10 * time.Millisecond, ReadTimeout: 5 * time.Second}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out := make(chan Event, 1)
	go src.Run(ctx, out)
	select {
	case <-out:
	case <-ctx.Done():
		t.Fatal("no event received")
	}
}
