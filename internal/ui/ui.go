// Package ui provides the user-facing surface of hddguard: a system tray
// icon with manual test / early-release actions and toast notifications on
// Windows, and a headless logger elsewhere (or with the tray disabled).
package ui

import "fmt"

// Logf is the logging callback used across the package.
type Logf func(format string, args ...any)

// Action is a user request coming from the UI.
type Action int

const (
	// ActionTest runs a short protect/release cycle to verify head parking.
	ActionTest Action = iota
	// ActionRelease releases protection early (shaking has stopped).
	ActionRelease
	// ActionQuit exits the program.
	ActionQuit
)

func (a Action) String() string {
	switch a {
	case ActionTest:
		return "test"
	case ActionRelease:
		return "release"
	case ActionQuit:
		return "quit"
	}
	return fmt.Sprintf("Action(%d)", int(a))
}

// Options configures RunLoop.
type Options struct {
	// Tray enables the system tray icon (Windows only; ignored elsewhere).
	Tray bool
	// Tooltip is shown when hovering the tray icon.
	Tooltip string
}

// UI is what the monitoring loop talks to.
type UI interface {
	// Actions returns the user-action channel. May be nil (never fires)
	// for UIs without controls.
	Actions() <-chan Action
	// SetProtected reflects the protection state in the UI.
	SetProtected(active bool, detail string)
	// Notify shows a user-visible notification.
	Notify(title, message string)
}

// Headless is a UI that only logs. Used on non-Windows platforms and when
// the tray is disabled.
type Headless struct {
	logf Logf
}

// NewHeadless builds a Headless UI.
func NewHeadless(logf Logf) *Headless {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &Headless{logf: logf}
}

// Actions returns nil: a headless UI never emits user actions.
func (h *Headless) Actions() <-chan Action { return nil }

// SetProtected logs the state change.
func (h *Headless) SetProtected(active bool, detail string) {
	h.logf("ui: protected=%v (%s)", active, detail)
}

// Notify logs the notification.
func (h *Headless) Notify(title, message string) {
	h.logf("ui: notify: %s — %s", title, message)
}
