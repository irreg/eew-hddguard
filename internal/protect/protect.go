// Package protect orchestrates putting disks into a shake-safe state when an
// EEW warning arrives and restoring them afterwards.
package protect

import (
	"fmt"
	"sort"
)

// Mode selects how aggressively disks are protected.
type Mode int

const (
	// ModeStandby flushes volumes and issues ATA STANDBY IMMEDIATE.
	// Heads park, but any I/O from the OS wakes the disk again.
	ModeStandby Mode = iota
	// ModeDismount additionally locks and dismounts the volumes so no I/O
	// can reach the disk while protection is held.
	ModeDismount
)

// ParseMode parses a config mode string.
func ParseMode(s string) (Mode, error) {
	switch s {
	case "standby":
		return ModeStandby, nil
	case "dismount":
		return ModeDismount, nil
	}
	return 0, fmt.Errorf("unknown mode %q (want \"standby\" or \"dismount\")", s)
}

func (m Mode) String() string {
	switch m {
	case ModeStandby:
		return "standby"
	case ModeDismount:
		return "dismount"
	}
	return fmt.Sprintf("Mode(%d)", int(m))
}

// DiskInfo describes one physical disk.
type DiskInfo struct {
	Number     int    // \\.\PhysicalDriveN
	Model      string // vendor/product string, best effort
	Rotational bool   // true for spinning HDDs
	System     bool   // holds the Windows system volume
	Volumes    []string
}

// Logf is the logging callback used across the package.
type Logf func(format string, args ...any)

// Session represents disks currently held in a protected state.
type Session interface {
	Release() error
}

// Controller performs the OS-specific disk operations.
type Controller interface {
	ListDisks() ([]DiskInfo, error)
	Protect(disks []DiskInfo, mode Mode) (Session, error)
}

// SelectDisks picks protection targets. If explicit disk numbers are given
// they are used (still refusing the system disk unless excludeSystem is
// false); otherwise all non-system rotational disks are selected.
func SelectDisks(all []DiskInfo, explicit []int, excludeSystem bool) ([]DiskInfo, error) {
	byNumber := make(map[int]DiskInfo, len(all))
	for _, d := range all {
		byNumber[d.Number] = d
	}
	if len(explicit) > 0 {
		nums := append([]int(nil), explicit...)
		sort.Ints(nums)
		var out []DiskInfo
		for _, n := range nums {
			d, ok := byNumber[n]
			if !ok {
				return nil, fmt.Errorf("disk %d not found", n)
			}
			if d.System && excludeSystem {
				return nil, fmt.Errorf("disk %d is the system disk; set excludeSystemDisk to false to include it", n)
			}
			out = append(out, d)
		}
		return out, nil
	}
	var out []DiskInfo
	for _, d := range all {
		if !d.Rotational {
			continue
		}
		if d.System && excludeSystem {
			continue
		}
		out = append(out, d)
	}
	return out, nil
}
