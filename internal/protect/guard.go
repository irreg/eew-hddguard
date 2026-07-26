package protect

import (
	"sync"
	"time"
)

// Guard debounces EEW triggers into protect/release cycles: the first
// trigger protects the disks, further triggers while protected extend the
// hold, and the hold timer releases them.
type Guard struct {
	ctrl  Controller
	disks []DiskInfo
	mode  Mode
	hold  time.Duration
	logf  Logf

	mu      sync.Mutex
	session Session
	timer   *time.Timer
}

// NewGuard builds a Guard. hold is how long protection lasts after the most
// recent trigger.
func NewGuard(ctrl Controller, disks []DiskInfo, mode Mode, hold time.Duration, logf Logf) *Guard {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &Guard{ctrl: ctrl, disks: disks, mode: mode, hold: hold, logf: logf}
}

// Trigger protects the disks, or extends the hold if already protected.
func (g *Guard) Trigger(reason string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.session != nil {
		g.timer.Reset(g.hold)
		g.logf("guard: already protected; hold extended by %s (%s)", g.hold, reason)
		return nil
	}
	g.logf("guard: PROTECTING %d disk(s), mode=%s (%s)", len(g.disks), g.mode, reason)
	sess, err := g.ctrl.Protect(g.disks, g.mode)
	if err != nil {
		g.logf("guard: protect failed: %v", err)
		return err
	}
	g.session = sess
	g.timer = time.AfterFunc(g.hold, g.expire)
	g.logf("guard: protected; will release in %s", g.hold)
	return nil
}

func (g *Guard) expire() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.releaseLocked("hold expired")
}

// Active reports whether disks are currently protected.
func (g *Guard) Active() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.session != nil
}

// Close releases protection if it is active. Safe to call multiple times.
func (g *Guard) Close() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.timer != nil {
		g.timer.Stop()
		g.timer = nil
	}
	return g.releaseLocked("shutdown")
}

func (g *Guard) releaseLocked(reason string) error {
	if g.session == nil {
		return nil
	}
	err := g.session.Release()
	g.session = nil
	if err != nil {
		g.logf("guard: release failed (%s): %v", reason, err)
		return err
	}
	g.logf("guard: released (%s)", reason)
	return nil
}
