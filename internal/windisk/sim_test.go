//go:build !windows

package windisk

import (
	"strings"
	"testing"

	"hddguard/internal/protect"
)

func TestNewControllerRequiresDryRunOffWindows(t *testing.T) {
	if _, err := NewController(false, nil); err == nil {
		t.Error("expected error without dry-run on non-Windows")
	}
}

func TestSimControllerFlow(t *testing.T) {
	var logs []string
	logf := func(format string, args ...any) {
		logs = append(logs, format)
		t.Logf(format, args...)
	}
	ctrl, err := NewController(true, logf)
	if err != nil {
		t.Fatal(err)
	}
	disks, err := ctrl.ListDisks()
	if err != nil {
		t.Fatal(err)
	}
	if len(disks) != 2 {
		t.Fatalf("got %d disks, want 2", len(disks))
	}
	hdds, err := protect.SelectDisks(disks, nil, true)
	if err != nil || len(hdds) != 1 || !hdds[0].Rotational {
		t.Fatalf("SelectDisks: %v, %v", hdds, err)
	}

	sess, err := ctrl.Protect(hdds, protect.ModeDismount)
	if err != nil {
		t.Fatal(err)
	}
	if err := sess.Release(); err != nil {
		t.Fatal(err)
	}

	joined := strings.Join(logs, "\n")
	for _, want := range []string{"dismount", "STANDBY IMMEDIATE", "released"} {
		if !strings.Contains(joined, want) {
			t.Errorf("logs missing %q:\n%s", want, joined)
		}
	}
}

func TestSimControllerStandbyMode(t *testing.T) {
	var logs []string
	ctrl, _ := NewController(true, func(format string, args ...any) { logs = append(logs, format) })
	disks, _ := ctrl.ListDisks()
	sess, err := ctrl.Protect(disks[1:], protect.ModeStandby)
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Release()
	joined := strings.Join(logs, "\n")
	if strings.Contains(joined, "dismount") {
		t.Errorf("standby mode must not dismount:\n%s", joined)
	}
	if !strings.Contains(joined, "flush") {
		t.Errorf("standby mode should flush:\n%s", joined)
	}
}

func TestNewControllerNilLogf(t *testing.T) {
	ctrl, err := NewController(true, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Must not panic with a nil logf.
	disks, _ := ctrl.ListDisks()
	sess, err := ctrl.Protect(disks, protect.ModeStandby)
	if err != nil {
		t.Fatal(err)
	}
	sess.Release()
}
