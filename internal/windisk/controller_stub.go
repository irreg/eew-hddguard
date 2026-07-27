//go:build !windows

// Package windisk implements protect.Controller for Windows physical disks.
// On other platforms only the dry-run simulation controller is available.
package windisk

import (
	"errors"

	"eew-hddguard/internal/protect"
)

// NewController returns a simulation controller when dryRun is set; real
// disk control is only available on Windows.
func NewController(dryRun bool, logf protect.Logf) (protect.Controller, error) {
	if !dryRun {
		return nil, errors.New("disk control is only supported on Windows; use dry-run mode on this OS")
	}
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &simController{logf: logf}, nil
}
