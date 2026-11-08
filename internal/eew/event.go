// Package eew provides Earthquake Early Warning (緊急地震速報) reception.
package eew

import (
	"context"
	"strings"
)

// Event is a normalized EEW warning event, independent of the feed that
// produced it.
type Event struct {
	Source     string  // feed identifier, e.g. "p2pquake"
	EventID    string  // feed-specific event ID
	Serial     string  // report serial within the event
	Test       bool    // training/test message
	Cancelled  bool    // the warning was cancelled
	Hypocenter string  // hypocenter name (may be empty)
	Magnitude  float64 // magnitude (0 if unknown)
	Prefs      []string
}

// Source delivers EEW events until the context is cancelled.
type Source interface {
	Run(ctx context.Context, out chan<- Event) error
}

// ShouldTrigger reports whether ev warrants disk protection, given the
// prefectures the user wants to watch. An empty watchPrefs matches any
// warning area.
func ShouldTrigger(ev Event, watchPrefs []string, allowTest bool) (bool, string) {
	if ev.Cancelled {
		return false, "cancelled"
	}
	if ev.Test && !allowTest {
		return false, "test message"
	}
	if len(watchPrefs) == 0 {
		return true, "EEW warning (watching all regions)"
	}
	for _, w := range watchPrefs {
		for _, p := range ev.Prefs {
			if normalizePref(p) == normalizePref(w) {
				return true, "warning area includes " + w
			}
		}
	}
	return false, "no watched prefecture in warning areas"
}

// normalizePref strips a trailing 都/道/府/県 so that user-written names
// like 東京都 match the P2P地震情報 feed, which sends 東京. The suffix is
// only stripped when at least two runes remain, so 京都 stays 京都 while
// 京都府 and 東京都 normalize to 京都 and 東京.
func normalizePref(s string) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) >= 3 {
		switch r[len(r)-1] {
		case '都', '道', '府', '県':
			r = r[:len(r)-1]
		}
	}
	return string(r)
}
