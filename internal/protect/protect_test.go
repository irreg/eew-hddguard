package protect

import (
	"strings"
	"testing"
)

func TestParseMode(t *testing.T) {
	if m, err := ParseMode("standby"); err != nil || m != ModeStandby {
		t.Errorf("standby: %v %v", m, err)
	}
	if m, err := ParseMode("dismount"); err != nil || m != ModeDismount {
		t.Errorf("dismount: %v %v", m, err)
	}
	if _, err := ParseMode("nuke"); err == nil {
		t.Error("expected error for unknown mode")
	}
}

func TestModeString(t *testing.T) {
	if ModeStandby.String() != "standby" || ModeDismount.String() != "dismount" {
		t.Error("mode strings wrong")
	}
	if s := Mode(9).String(); !strings.Contains(s, "9") {
		t.Errorf("unknown mode string: %s", s)
	}
}

func TestSelectDisks(t *testing.T) {
	all := []DiskInfo{
		{Number: 0, Rotational: false, System: true},
		{Number: 1, Rotational: true},
		{Number: 2, Rotational: true, System: true},
		{Number: 3, Rotational: false},
	}

	t.Run("auto picks non-system HDDs", func(t *testing.T) {
		got, err := SelectDisks(all, nil, true)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || got[0].Number != 1 {
			t.Errorf("got %v", got)
		}
	})

	t.Run("auto with system allowed", func(t *testing.T) {
		got, err := SelectDisks(all, nil, false)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 2 || got[0].Number != 1 || got[1].Number != 2 {
			t.Errorf("got %v", got)
		}
	})

	t.Run("explicit selection", func(t *testing.T) {
		got, err := SelectDisks(all, []int{3, 1}, true)
		if err != nil {
			t.Fatal(err)
		}
		// Sorted, and explicit selection may include SSDs.
		if len(got) != 2 || got[0].Number != 1 || got[1].Number != 3 {
			t.Errorf("got %v", got)
		}
	})

	t.Run("explicit missing disk", func(t *testing.T) {
		if _, err := SelectDisks(all, []int{7}, true); err == nil {
			t.Error("expected error")
		}
	})

	t.Run("explicit system disk refused", func(t *testing.T) {
		if _, err := SelectDisks(all, []int{2}, true); err == nil {
			t.Error("expected error")
		}
	})

	t.Run("explicit system disk allowed when opted in", func(t *testing.T) {
		got, err := SelectDisks(all, []int{2}, false)
		if err != nil || len(got) != 1 || got[0].Number != 2 {
			t.Errorf("got %v, %v", got, err)
		}
	})
}
