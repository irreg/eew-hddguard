package ui

import (
	"strings"
	"testing"
)

func TestActionString(t *testing.T) {
	if ActionTest.String() != "test" || ActionRelease.String() != "release" || ActionQuit.String() != "quit" {
		t.Error("action strings wrong")
	}
	if s := Action(9).String(); !strings.Contains(s, "9") {
		t.Errorf("unknown action string: %s", s)
	}
}

func TestHeadless(t *testing.T) {
	var logs []string
	h := NewHeadless(func(format string, args ...any) { logs = append(logs, format) })
	if h.Actions() != nil {
		t.Error("headless Actions() must be nil")
	}
	h.SetProtected(true, "test")
	h.Notify("title", "message")
	if len(logs) != 2 {
		t.Errorf("expected 2 log lines, got %d", len(logs))
	}
}

func TestHeadlessNilLogf(t *testing.T) {
	h := NewHeadless(nil)
	// Must not panic.
	h.SetProtected(false, "x")
	h.Notify("a", "b")
}
