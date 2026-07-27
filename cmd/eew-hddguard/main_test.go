package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"eew-hddguard/internal/config"
	"eew-hddguard/internal/eew"
	"eew-hddguard/internal/protect"
	"eew-hddguard/internal/ui"
)

type testSession struct {
	mu       sync.Mutex
	released int
}

func (s *testSession) Release() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.released++
	return nil
}

type testController struct {
	mu       sync.Mutex
	disks    []protect.DiskInfo
	listErr  error
	protects int
	session  testSession
}

func (c *testController) ListDisks() ([]protect.DiskInfo, error) { return c.disks, c.listErr }

func (c *testController) Protect(disks []protect.DiskInfo, mode protect.Mode) (protect.Session, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.protects++
	return &c.session, nil
}

type testSource struct {
	events []eew.Event
	err    error
}

func (s *testSource) Run(ctx context.Context, out chan<- eew.Event) error {
	for _, ev := range s.events {
		select {
		case out <- ev:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return s.err
}

func defaultDisks() []protect.DiskInfo {
	return []protect.DiskInfo{
		{Number: 0, Model: "SysSSD", Rotational: false, System: true},
		{Number: 1, Model: "DataHDD", Rotational: true, Volumes: []string{`\\?\Volume{x}`}},
	}
}

// withFakes swaps the controller/source factories for the duration of a test.
func withFakes(t *testing.T, ctrl *testController, src eew.Source) {
	t.Helper()
	origCtrl, origSrc, origHold := newController, newSource, simulateHold
	newController = func(dryRun bool, logf protect.Logf) (protect.Controller, error) {
		return ctrl, nil
	}
	if src != nil {
		newSource = func(cfg *config.Config, logf protect.Logf) eew.Source { return src }
	}
	simulateHold = 50 * time.Millisecond
	t.Cleanup(func() { newController, newSource, simulateHold = origCtrl, origSrc, origHold })
}

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRunBadFlag(t *testing.T) {
	var out, errBuf bytes.Buffer
	if code := run([]string{"-no-such-flag"}, &out, &errBuf); code != 2 {
		t.Errorf("code = %d, want 2", code)
	}
}

func TestRunBadConfig(t *testing.T) {
	var out, errBuf bytes.Buffer
	path := writeConfig(t, `{"mode": "bogus"}`)
	if code := run([]string{"-config", path}, &out, &errBuf); code != 1 {
		t.Errorf("code = %d, want 1; stderr:\n%s", code, errBuf.String())
	}
}

func TestRunControllerError(t *testing.T) {
	orig := newController
	newController = func(dryRun bool, logf protect.Logf) (protect.Controller, error) {
		return nil, errors.New("no disks for you")
	}
	t.Cleanup(func() { newController = orig })
	var out, errBuf bytes.Buffer
	path := writeConfig(t, `{}`)
	if code := run([]string{"-config", path}, &out, &errBuf); code != 1 {
		t.Errorf("code = %d, want 1", code)
	}
}

func TestRunListDiskError(t *testing.T) {
	ctrl := &testController{listErr: errors.New("enumeration failed")}
	withFakes(t, ctrl, nil)
	var out, errBuf bytes.Buffer
	path := writeConfig(t, `{}`)
	if code := run([]string{"-config", path}, &out, &errBuf); code != 1 {
		t.Errorf("code = %d, want 1", code)
	}
}

func TestRunListDisks(t *testing.T) {
	ctrl := &testController{disks: defaultDisks()}
	withFakes(t, ctrl, nil)
	var out, errBuf bytes.Buffer
	path := writeConfig(t, `{}`)
	if code := run([]string{"-config", path, "-list-disks"}, &out, &errBuf); code != 0 {
		t.Fatalf("code = %d, want 0; stderr:\n%s", code, errBuf.String())
	}
	s := out.String()
	for _, want := range []string{"DataHDD", "SysSSD", "HDD", "SSD", "yes"} {
		if !strings.Contains(s, want) {
			t.Errorf("output missing %q:\n%s", want, s)
		}
	}
}

func TestRunListDisksEmpty(t *testing.T) {
	ctrl := &testController{}
	withFakes(t, ctrl, nil)
	var out, errBuf bytes.Buffer
	path := writeConfig(t, `{}`)
	if code := run([]string{"-config", path, "-list-disks"}, &out, &errBuf); code != 0 {
		t.Fatalf("code = %d, want 0; stderr:\n%s", code, errBuf.String())
	}
	if !strings.Contains(out.String(), "管理者権限") {
		t.Errorf("empty listing should hint at admin rights:\n%s", out.String())
	}
}

func TestRunSelectionError(t *testing.T) {
	ctrl := &testController{disks: defaultDisks()}
	withFakes(t, ctrl, nil)
	var out, errBuf bytes.Buffer
	path := writeConfig(t, `{"disks": [9]}`)
	if code := run([]string{"-config", path}, &out, &errBuf); code != 1 {
		t.Errorf("code = %d, want 1", code)
	}
}

func TestRunNoTargets(t *testing.T) {
	ctrl := &testController{disks: []protect.DiskInfo{
		{Number: 0, Rotational: false, System: true},
	}}
	withFakes(t, ctrl, nil)
	var out, errBuf bytes.Buffer
	path := writeConfig(t, `{}`)
	if code := run([]string{"-config", path}, &out, &errBuf); code != 1 {
		t.Errorf("code = %d, want 1", code)
	}
	if !strings.Contains(errBuf.String(), "no target disks") {
		t.Errorf("stderr:\n%s", errBuf.String())
	}
}

func TestRunSimulate(t *testing.T) {
	ctrl := &testController{disks: defaultDisks()}
	withFakes(t, ctrl, nil)
	var out, errBuf bytes.Buffer
	path := writeConfig(t, `{}`)
	if code := run([]string{"-config", path, "-simulate"}, &out, &errBuf); code != 0 {
		t.Fatalf("code = %d, want 0; stderr:\n%s", code, errBuf.String())
	}
	if ctrl.protects != 1 {
		t.Errorf("protect called %d times, want 1", ctrl.protects)
	}
	ctrl.session.mu.Lock()
	defer ctrl.session.mu.Unlock()
	if ctrl.session.released != 1 {
		t.Errorf("released %d times, want 1", ctrl.session.released)
	}
}

func TestRunEventLoopTriggersAndShutsDown(t *testing.T) {
	ctrl := &testController{disks: defaultDisks()}
	src := &testSource{events: []eew.Event{
		{EventID: "e1", Serial: "1", Prefs: []string{"石川県"}},                 // filtered out
		{EventID: "e2", Serial: "1", Prefs: []string{"東京都"}, Magnitude: 7.0}, // triggers
		{EventID: "e2", Serial: "2", Prefs: []string{"東京都"}},                 // extends
	}}
	withFakes(t, ctrl, src)
	var out, errBuf bytes.Buffer
	path := writeConfig(t, `{"watchPrefs": ["東京都"]}`)
	if code := run([]string{"-config", path}, &out, &errBuf); code != 0 {
		t.Fatalf("code = %d, want 0; stderr:\n%s", code, errBuf.String())
	}
	if ctrl.protects != 1 {
		t.Errorf("protect called %d times, want 1", ctrl.protects)
	}
	ctrl.session.mu.Lock()
	released := ctrl.session.released
	ctrl.session.mu.Unlock()
	if released != 1 {
		t.Errorf("released %d times, want 1 (on shutdown)", released)
	}
	logs := errBuf.String()
	if !strings.Contains(logs, "ignoring event e1") {
		t.Errorf("logs missing filter message:\n%s", logs)
	}
}

func TestRunSourceFailure(t *testing.T) {
	ctrl := &testController{disks: defaultDisks()}
	src := &testSource{err: errors.New("feed exploded")}
	withFakes(t, ctrl, src)
	var out, errBuf bytes.Buffer
	path := writeConfig(t, `{}`)
	if code := run([]string{"-config", path}, &out, &errBuf); code != 1 {
		t.Errorf("code = %d, want 1", code)
	}
}

// fakeUI records notifications and state changes and feeds scripted actions
// to the monitor loop.
type fakeUI struct {
	actions chan ui.Action
	mu      sync.Mutex
	notices []string
	states  []bool
}

func (u *fakeUI) Actions() <-chan ui.Action { return u.actions }

func (u *fakeUI) SetProtected(active bool, detail string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.states = append(u.states, active)
}

func (u *fakeUI) Notify(title, message string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.notices = append(u.notices, title+" "+message)
}

// blockingSource emits nothing and runs until cancelled, keeping the
// monitor loop alive for UI-action tests.
type blockingSource struct{}

func (blockingSource) Run(ctx context.Context, out chan<- eew.Event) error {
	<-ctx.Done()
	return ctx.Err()
}

func TestRunUIManualTestAndEarlyRelease(t *testing.T) {
	ctrl := &testController{disks: defaultDisks()}
	withFakes(t, ctrl, blockingSource{})

	fake := &fakeUI{actions: make(chan ui.Action, 8)}
	fake.actions <- ui.ActionTest
	fake.actions <- ui.ActionRelease
	fake.actions <- ui.ActionQuit

	origUI, origHold := runUI, manualTestHold
	runUI = func(opts ui.Options, logf ui.Logf, body func(ui.UI) int) int { return body(fake) }
	manualTestHold = time.Hour // must not expire before the manual release
	t.Cleanup(func() { runUI, manualTestHold = origUI, origHold })

	var out, errBuf bytes.Buffer
	path := writeConfig(t, `{}`)
	if code := run([]string{"-config", path}, &out, &errBuf); code != 0 {
		t.Fatalf("code = %d, want 0; stderr:\n%s", code, errBuf.String())
	}
	if ctrl.protects != 1 {
		t.Errorf("protect called %d times, want 1", ctrl.protects)
	}
	ctrl.session.mu.Lock()
	released := ctrl.session.released
	ctrl.session.mu.Unlock()
	if released != 1 {
		t.Errorf("released %d times, want 1", released)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.states) != 2 || !fake.states[0] || fake.states[1] {
		t.Errorf("states = %v, want [true false]", fake.states)
	}
	notices := strings.Join(fake.notices, "\n")
	for _, want := range []string{"発動", "今すぐ解除", "解除しました", "手動解除"} {
		if !strings.Contains(notices, want) {
			t.Errorf("notifications missing %q:\n%s", want, notices)
		}
	}
}

func TestRunLogFile(t *testing.T) {
	ctrl := &testController{disks: defaultDisks()}
	withFakes(t, ctrl, &testSource{})
	logPath := filepath.Join(t.TempDir(), "eew-hddguard.log")
	var out, errBuf bytes.Buffer
	path := writeConfig(t, `{"logFile": `+strconv.Quote(logPath)+`}`)
	if code := run([]string{"-config", path}, &out, &errBuf); code != 0 {
		t.Fatalf("code = %d, want 0; stderr:\n%s", code, errBuf.String())
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "shutting down") {
		t.Errorf("log file missing content:\n%s", data)
	}
}

func TestRunLogFileOpenError(t *testing.T) {
	ctrl := &testController{disks: defaultDisks()}
	withFakes(t, ctrl, nil)
	var out, errBuf bytes.Buffer
	// A directory path cannot be opened as a file.
	path := writeConfig(t, `{"logFile": `+strconv.Quote(t.TempDir())+`}`)
	if code := run([]string{"-config", path}, &out, &errBuf); code != 1 {
		t.Errorf("code = %d, want 1", code)
	}
}

func TestRunMissingConfigUsesDefaults(t *testing.T) {
	ctrl := &testController{disks: defaultDisks()}
	src := &testSource{}
	withFakes(t, ctrl, src)
	var out, errBuf bytes.Buffer
	missing := filepath.Join(t.TempDir(), "absent.json")
	if code := run([]string{"-config", missing, "-dry-run"}, &out, &errBuf); code != 0 {
		t.Fatalf("code = %d, want 0; stderr:\n%s", code, errBuf.String())
	}
	if !strings.Contains(errBuf.String(), "using defaults") {
		t.Errorf("stderr:\n%s", errBuf.String())
	}
}
