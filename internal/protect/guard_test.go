package protect

import (
	"errors"
	"sync"
	"testing"
	"time"
)

type fakeSession struct {
	mu       sync.Mutex
	released int
	err      error
}

func (s *fakeSession) Release() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.released++
	return s.err
}

type fakeController struct {
	mu         sync.Mutex
	protects   int
	lastMode   Mode
	lastDisks  []DiskInfo
	protectErr error
	session    *fakeSession
}

func (c *fakeController) ListDisks() ([]DiskInfo, error) { return c.lastDisks, nil }

func (c *fakeController) Protect(disks []DiskInfo, mode Mode) (Session, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.protects++
	c.lastMode = mode
	c.lastDisks = disks
	if c.protectErr != nil {
		return nil, c.protectErr
	}
	if c.session == nil {
		c.session = &fakeSession{}
	}
	return c.session, nil
}

func (c *fakeController) protectCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.protects
}

func waitFor(t *testing.T, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal(msg)
}

func TestGuardProtectsAndReleasesAfterHold(t *testing.T) {
	ctrl := &fakeController{}
	g := NewGuard(ctrl, []DiskInfo{{Number: 1}}, ModeDismount, 50*time.Millisecond, t.Logf)
	if g.Active() {
		t.Fatal("guard active before trigger")
	}
	if err := g.Trigger("test"); err != nil {
		t.Fatal(err)
	}
	if !g.Active() {
		t.Fatal("guard not active after trigger")
	}
	if ctrl.lastMode != ModeDismount || len(ctrl.lastDisks) != 1 {
		t.Errorf("protect called with mode=%v disks=%v", ctrl.lastMode, ctrl.lastDisks)
	}
	waitFor(t, func() bool { return !g.Active() }, "hold never expired")
	ctrl.session.mu.Lock()
	released := ctrl.session.released
	ctrl.session.mu.Unlock()
	if released != 1 {
		t.Errorf("released %d times, want 1", released)
	}
}

func TestGuardSecondTriggerExtendsWithoutReprotecting(t *testing.T) {
	ctrl := &fakeController{}
	g := NewGuard(ctrl, []DiskInfo{{Number: 1}}, ModeStandby, 80*time.Millisecond, t.Logf)
	if err := g.Trigger("first"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(40 * time.Millisecond)
	if err := g.Trigger("second"); err != nil {
		t.Fatal(err)
	}
	// 60ms after the second trigger the original 80ms hold would have
	// expired, but the extension keeps it active.
	time.Sleep(60 * time.Millisecond)
	if !g.Active() {
		t.Error("hold was not extended by second trigger")
	}
	if ctrl.protectCount() != 1 {
		t.Errorf("protect called %d times, want 1", ctrl.protectCount())
	}
	waitFor(t, func() bool { return !g.Active() }, "hold never expired after extension")
}

func TestGuardCloseReleasesAndIsIdempotent(t *testing.T) {
	ctrl := &fakeController{}
	g := NewGuard(ctrl, nil, ModeStandby, time.Hour, nil)
	if err := g.Trigger("test"); err != nil {
		t.Fatal(err)
	}
	if err := g.Close(); err != nil {
		t.Fatal(err)
	}
	if g.Active() {
		t.Error("still active after Close")
	}
	if err := g.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
	ctrl.session.mu.Lock()
	defer ctrl.session.mu.Unlock()
	if ctrl.session.released != 1 {
		t.Errorf("released %d times, want 1", ctrl.session.released)
	}
}

func TestGuardProtectError(t *testing.T) {
	wantErr := errors.New("boom")
	ctrl := &fakeController{protectErr: wantErr}
	g := NewGuard(ctrl, nil, ModeStandby, time.Hour, t.Logf)
	if err := g.Trigger("test"); !errors.Is(err, wantErr) {
		t.Errorf("Trigger returned %v, want %v", err, wantErr)
	}
	if g.Active() {
		t.Error("guard active after failed protect")
	}
}

func TestGuardTriggerForUsesCustomHold(t *testing.T) {
	ctrl := &fakeController{}
	// Long default hold, short explicit hold.
	g := NewGuard(ctrl, nil, ModeStandby, time.Hour, t.Logf)
	if err := g.TriggerFor("manual test", 30*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return !g.Active() }, "custom hold never expired")
}

func TestGuardReleaseNow(t *testing.T) {
	ctrl := &fakeController{}
	g := NewGuard(ctrl, nil, ModeDismount, time.Hour, t.Logf)
	if err := g.ReleaseNow("noop"); err != nil {
		t.Errorf("ReleaseNow when idle: %v", err)
	}
	if err := g.Trigger("test"); err != nil {
		t.Fatal(err)
	}
	if err := g.ReleaseNow("shaking stopped"); err != nil {
		t.Fatal(err)
	}
	if g.Active() {
		t.Error("still active after ReleaseNow")
	}
	ctrl.session.mu.Lock()
	defer ctrl.session.mu.Unlock()
	if ctrl.session.released != 1 {
		t.Errorf("released %d times, want 1", ctrl.session.released)
	}
}

func TestGuardOnChangeCallbacks(t *testing.T) {
	ctrl := &fakeController{}
	g := NewGuard(ctrl, nil, ModeStandby, time.Hour, t.Logf)
	type change struct {
		active bool
		reason string
	}
	var mu sync.Mutex
	var changes []change
	g.SetOnChange(func(active bool, reason string) {
		mu.Lock()
		defer mu.Unlock()
		changes = append(changes, change{active, reason})
	})
	if err := g.Trigger("eew"); err != nil {
		t.Fatal(err)
	}
	// An extension must not fire the callback again.
	if err := g.Trigger("extend"); err != nil {
		t.Fatal(err)
	}
	if err := g.ReleaseNow("manual"); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	want := []change{{true, "eew"}, {false, "manual"}}
	if len(changes) != len(want) {
		t.Fatalf("changes = %v, want %v", changes, want)
	}
	for i := range want {
		if changes[i] != want[i] {
			t.Errorf("changes[%d] = %v, want %v", i, changes[i], want[i])
		}
	}
}

func TestGuardOnChangeCanCallBackIntoGuard(t *testing.T) {
	// The callback runs outside the guard's lock; calling guard methods
	// from it must not deadlock.
	ctrl := &fakeController{}
	g := NewGuard(ctrl, nil, ModeStandby, time.Hour, t.Logf)
	g.SetOnChange(func(active bool, reason string) {
		g.Active()
	})
	done := make(chan struct{})
	go func() {
		g.Trigger("eew")
		g.ReleaseNow("manual")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("deadlock: OnChange callback blocked the guard")
	}
}

func TestGuardReleaseErrorPropagates(t *testing.T) {
	wantErr := errors.New("unlock failed")
	ctrl := &fakeController{session: &fakeSession{err: wantErr}}
	g := NewGuard(ctrl, nil, ModeStandby, time.Hour, t.Logf)
	if err := g.Trigger("test"); err != nil {
		t.Fatal(err)
	}
	if err := g.Close(); !errors.Is(err, wantErr) {
		t.Errorf("Close returned %v, want %v", err, wantErr)
	}
}
