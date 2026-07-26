package protect

import (
	"sync"
	"time"
)

// Guard debounces EEW triggers into protect/release cycles: the first
// trigger protects the disks, further triggers while protected extend the
// hold, and the hold timer — or a manual release — restores them.
type Guard struct {
	ctrl  Controller
	disks []DiskInfo
	mode  Mode
	hold  time.Duration
	logf  Logf

	mu       sync.Mutex
	session  Session
	timer    *time.Timer
	onChange func(active bool, reason string)
}

// NewGuard builds a Guard. hold is how long protection lasts after the most
// recent trigger.
func NewGuard(ctrl Controller, disks []DiskInfo, mode Mode, hold time.Duration, logf Logf) *Guard {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &Guard{ctrl: ctrl, disks: disks, mode: mode, hold: hold, logf: logf}
}

// SetOnChange registers a callback invoked (outside the guard's lock) after
// every protect/release transition. The callback may call back into the
// Guard from another goroutine, but must not block.
func (g *Guard) SetOnChange(fn func(active bool, reason string)) {
	g.mu.Lock()
	g.onChange = fn
	g.mu.Unlock()
}

// Trigger protects the disks with the configured hold, or extends the hold
// if already protected.
func (g *Guard) Trigger(reason string) error {
	return g.TriggerFor(reason, g.hold)
}

// TriggerFor is Trigger with an explicit hold duration (used for manual
// test runs with a short hold).
func (g *Guard) TriggerFor(reason string, hold time.Duration) error {
	g.mu.Lock()
	if g.session != nil {
		g.timer.Reset(hold)
		g.mu.Unlock()
		g.logf("guard: already protected; hold extended by %s (%s)", hold, reason)
		return nil
	}
	g.logf("guard: PROTECTING %d disk(s), mode=%s (%s)", len(g.disks), g.mode, reason)
	sess, err := g.ctrl.Protect(g.disks, g.mode)
	if err != nil {
		g.mu.Unlock()
		g.logf("guard: protect failed: %v", err)
		return err
	}
	g.session = sess
	g.timer = time.AfterFunc(hold, g.expire)
	fn := g.onChange
	g.mu.Unlock()
	g.logf("guard: protected; will release in %s", hold)
	if fn != nil {
		fn(true, reason)
	}
	return nil
}

func (g *Guard) expire() {
	g.releaseWith("保持時間が経過")
}

// ReleaseNow releases protection early (user-initiated recovery once the
// shaking has stopped). No-op when not protected.
func (g *Guard) ReleaseNow(reason string) error {
	return g.releaseWith(reason)
}

// Active reports whether disks are currently protected.
func (g *Guard) Active() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.session != nil
}

// Close releases protection if it is active. Safe to call multiple times.
func (g *Guard) Close() error {
	return g.releaseWith("シャットダウン")
}

func (g *Guard) releaseWith(reason string) error {
	g.mu.Lock()
	if g.timer != nil {
		g.timer.Stop()
		g.timer = nil
	}
	if g.session == nil {
		g.mu.Unlock()
		return nil
	}
	err := g.session.Release()
	g.session = nil
	fn := g.onChange
	g.mu.Unlock()
	if err != nil {
		g.logf("guard: release failed (%s): %v", reason, err)
	} else {
		g.logf("guard: released (%s)", reason)
	}
	if fn != nil {
		fn(false, reason)
	}
	return err
}
