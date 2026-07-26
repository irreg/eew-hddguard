//go:build !windows

package ui

import (
	"strings"
	"testing"
)

func TestRunLoopHeadless(t *testing.T) {
	var got UI
	code := RunLoop(Options{}, t.Logf, func(u UI) int {
		got = u
		return 7
	})
	if code != 7 {
		t.Errorf("code = %d, want 7", code)
	}
	if _, ok := got.(*Headless); !ok {
		t.Errorf("got %T, want *Headless", got)
	}
}

func TestRunLoopTrayFallsBackOffWindows(t *testing.T) {
	var logs []string
	code := RunLoop(Options{Tray: true}, func(format string, args ...any) {
		logs = append(logs, format)
	}, func(u UI) int { return 0 })
	if code != 0 {
		t.Errorf("code = %d, want 0", code)
	}
	joined := strings.Join(logs, "\n")
	if !strings.Contains(joined, "only available on Windows") {
		t.Errorf("missing fallback log:\n%s", joined)
	}
}

func TestRunLoopTrayNilLogf(t *testing.T) {
	// Must not panic even with Tray requested and no logger.
	if code := RunLoop(Options{Tray: true}, nil, func(u UI) int { return 3 }); code != 3 {
		t.Errorf("code = %d, want 3", code)
	}
}
