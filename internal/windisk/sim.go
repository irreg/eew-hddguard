package windisk

import "quakeguard/internal/protect"

// simController is a dry-run stand-in used on non-Windows platforms so the
// full trigger/hold/release flow can be exercised end to end.
type simController struct {
	logf protect.Logf
}

func (c *simController) ListDisks() ([]protect.DiskInfo, error) {
	return []protect.DiskInfo{
		{Number: 0, Model: "Simulated NVMe SSD", Rotational: false, System: true,
			Volumes: []string{`\\?\Volume{sim-system}`}},
		{Number: 1, Model: "Simulated HDD", Rotational: true, System: false,
			Volumes: []string{`\\?\Volume{sim-data}`}},
	}, nil
}

func (c *simController) Protect(disks []protect.DiskInfo, mode protect.Mode) (protect.Session, error) {
	for _, d := range disks {
		for _, vol := range d.Volumes {
			if mode == protect.ModeDismount {
				c.logf("windisk: [sim] flush, lock and dismount %s", vol)
			} else {
				c.logf("windisk: [sim] flush %s", vol)
			}
		}
		c.logf("windisk: [sim] STANDBY IMMEDIATE -> PhysicalDrive%d", d.Number)
	}
	return &simSession{c: c, disks: disks}, nil
}

type simSession struct {
	c     *simController
	disks []protect.DiskInfo
}

func (s *simSession) Release() error {
	for _, d := range s.disks {
		s.c.logf("windisk: [sim] released PhysicalDrive%d", d.Number)
	}
	return nil
}
