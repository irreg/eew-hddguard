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
