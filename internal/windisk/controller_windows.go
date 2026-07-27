//go:build windows

// Package windisk implements protect.Controller for Windows physical disks.
package windisk

import (
	"fmt"
	"strings"
	"time"

	"golang.org/x/sys/windows"

	"eew-hddguard/internal/diskioctl"
	"eew-hddguard/internal/protect"
)

const maxPhysicalDrives = 64

// NewController returns the real Windows disk controller. With dryRun set,
// enumeration is real but protect/release actions are only logged.
func NewController(dryRun bool, logf protect.Logf) (protect.Controller, error) {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &controller{dryRun: dryRun, logf: logf}, nil
}

type controller struct {
	dryRun bool
	logf   protect.Logf
}

func openPath(path string, access uint32) (windows.Handle, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return windows.InvalidHandle, err
	}
	return windows.CreateFile(p, access,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil,
		windows.OPEN_EXISTING, 0, 0)
}

func ioctl(h windows.Handle, code uint32, in []byte, out []byte) error {
	var inPtr, outPtr *byte
	if len(in) > 0 {
		inPtr = &in[0]
	}
	if len(out) > 0 {
		outPtr = &out[0]
	}
	var ret uint32
	return windows.DeviceIoControl(h, code, inPtr, uint32(len(in)), outPtr, uint32(len(out)), &ret, nil)
}

// ListDisks enumerates physical disks, their rotational status, model
// strings, volumes, and whether they hold the system volume.
func (c *controller) ListDisks() ([]protect.DiskInfo, error) {
	sysDisks, err := systemDiskNumbers()
	if err != nil {
		c.logf("windisk: cannot determine system disk: %v", err)
		sysDisks = map[uint32]bool{}
	}
	volsByDisk, err := volumesByDisk()
	if err != nil {
		c.logf("windisk: volume enumeration failed: %v", err)
		volsByDisk = map[uint32][]string{}
	}

	var disks []protect.DiskInfo
	denied := 0
	for n := 0; n < maxPhysicalDrives; n++ {
		path := fmt.Sprintf(`\\.\PhysicalDrive%d`, n)
		h, err := openPath(path, windows.GENERIC_READ)
		if err != nil {
			if err == windows.ERROR_ACCESS_DENIED {
				denied++
			}
			continue
		}
		d := protect.DiskInfo{
			Number:     n,
			Rotational: queryRotational(h),
			Model:      queryModel(h),
			System:     sysDisks[uint32(n)],
			Volumes:    volsByDisk[uint32(n)],
		}
		windows.CloseHandle(h)
		disks = append(disks, d)
	}
	if len(disks) == 0 && denied > 0 {
		return nil, fmt.Errorf("access to all physical disks was denied (%d drives); run as administrator (管理者として実行してください)", denied)
	}
	return disks, nil
}

func queryRotational(h windows.Handle) bool {
	in := diskioctl.BuildStoragePropertyQuery(diskioctl.StorageDeviceSeekPenaltyProperty)
	out := make([]byte, 16)
	if err := ioctl(h, diskioctl.IOCTLStorageQueryProperty, in, out); err != nil {
		// If the query is unsupported, err on the safe side and treat the
		// disk as rotational so it can still be protected.
		return true
	}
	rot, err := diskioctl.ParseSeekPenalty(out)
	if err != nil {
		return true
	}
	return rot
}

func queryModel(h windows.Handle) string {
	in := diskioctl.BuildStoragePropertyQuery(diskioctl.StorageDeviceProperty)
	out := make([]byte, 1024)
	if err := ioctl(h, diskioctl.IOCTLStorageQueryProperty, in, out); err != nil {
		return ""
	}
	vendor, product, err := diskioctl.ParseDeviceDescriptor(out)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(strings.TrimSpace(vendor) + " " + product)
}

// systemDiskNumbers returns the physical disk numbers backing the Windows
// system volume.
func systemDiskNumbers() (map[uint32]bool, error) {
	winDir, err := windows.GetWindowsDirectory()
	if err != nil || len(winDir) < 2 {
		return nil, fmt.Errorf("GetWindowsDirectory: %w", err)
	}
	drive := winDir[:2] // e.g. "C:"
	h, err := openPath(`\\.\`+drive, 0)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(h)
	nums, err := diskNumbersOf(h)
	if err != nil {
		return nil, err
	}
	set := make(map[uint32]bool, len(nums))
	for _, d := range nums {
		set[d] = true
	}
	return set, nil
}

func diskNumbersOf(h windows.Handle) ([]uint32, error) {
	out := make([]byte, 4096)
	if err := ioctl(h, diskioctl.IOCTLVolumeGetVolumeDiskExtents, nil, out); err != nil {
		return nil, err
	}
	return diskioctl.ParseDiskExtents(out)
}

// volumesByDisk maps physical disk number to the volume paths (without a
// trailing backslash) located on it.
func volumesByDisk() (map[uint32][]string, error) {
	res := make(map[uint32][]string)
	var name [windows.MAX_PATH + 1]uint16
	fh, err := windows.FindFirstVolume(&name[0], uint32(len(name)))
	if err != nil {
		return nil, err
	}
	defer windows.FindVolumeClose(fh)
	for {
		vol := strings.TrimSuffix(windows.UTF16ToString(name[:]), `\`)
		if h, err := openPath(vol, 0); err == nil {
			if nums, err := diskNumbersOf(h); err == nil {
				for _, d := range nums {
					res[d] = append(res[d], vol)
				}
			}
			windows.CloseHandle(h)
		}
		if err := windows.FindNextVolume(fh, &name[0], uint32(len(name))); err != nil {
			return res, nil
		}
	}
}

type heldVolume struct {
	path   string
	handle windows.Handle
	locked bool
}

type session struct {
	c    *controller
	vols []heldVolume
}

// Protect flushes each target disk's volumes, optionally locks and
// dismounts them, then issues ATA STANDBY IMMEDIATE to park the heads.
// It protects as many disks as it can, logging per-disk failures.
func (c *controller) Protect(disks []protect.DiskInfo, mode protect.Mode) (protect.Session, error) {
	sess := &session{c: c}
	for _, d := range disks {
		for _, vol := range d.Volumes {
			if mode == protect.ModeDismount {
				c.lockAndDismount(sess, vol)
			} else {
				c.flushVolume(vol)
			}
		}
		if c.dryRun {
			c.logf("windisk: [dry-run] would send STANDBY IMMEDIATE to PhysicalDrive%d", d.Number)
			continue
		}
		if err := c.standbyDisk(d.Number); err != nil {
			c.logf("windisk: standby of PhysicalDrive%d failed: %v", d.Number, err)
		} else {
			c.logf("windisk: PhysicalDrive%d heads parked (STANDBY IMMEDIATE)", d.Number)
		}
	}
	return sess, nil
}

func (c *controller) flushVolume(vol string) {
	if c.dryRun {
		c.logf("windisk: [dry-run] would flush %s", vol)
		return
	}
	h, err := openPath(vol, windows.GENERIC_READ|windows.GENERIC_WRITE)
	if err != nil {
		c.logf("windisk: open %s for flush failed: %v", vol, err)
		return
	}
	defer windows.CloseHandle(h)
	if err := windows.FlushFileBuffers(h); err != nil {
		c.logf("windisk: flush %s failed: %v", vol, err)
	} else {
		c.logf("windisk: flushed %s", vol)
	}
}

// lockAndDismount flushes, locks (with retries) and dismounts a volume,
// keeping the handle open in the session so no I/O reaches the disk until
// release.
func (c *controller) lockAndDismount(sess *session, vol string) {
	if c.dryRun {
		c.logf("windisk: [dry-run] would flush, lock and dismount %s", vol)
		return
	}
	h, err := openPath(vol, windows.GENERIC_READ|windows.GENERIC_WRITE)
	if err != nil {
		c.logf("windisk: open %s failed: %v", vol, err)
		return
	}
	if err := windows.FlushFileBuffers(h); err != nil {
		c.logf("windisk: flush %s failed: %v", vol, err)
	}
	locked := false
	for attempt := 0; attempt < 3; attempt++ {
		if err := ioctl(h, diskioctl.FSCTLLockVolume, nil, nil); err == nil {
			locked = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err := ioctl(h, diskioctl.FSCTLDismountVolume, nil, nil); err != nil {
		c.logf("windisk: dismount %s failed: %v", vol, err)
	}
	if !locked {
		// Handles that blocked the lock were invalidated by the dismount;
		// locking now usually succeeds and keeps the volume offline.
		locked = ioctl(h, diskioctl.FSCTLLockVolume, nil, nil) == nil
	}
	c.logf("windisk: %s dismounted (locked=%v)", vol, locked)
	sess.vols = append(sess.vols, heldVolume{path: vol, handle: h, locked: locked})
}

func (c *controller) standbyDisk(number int) error {
	h, err := openPath(fmt.Sprintf(`\\.\PhysicalDrive%d`, number), windows.GENERIC_READ|windows.GENERIC_WRITE)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(h)
	buf := diskioctl.BuildStandbyImmediate(10)
	return ioctl(h, diskioctl.IOCTLATAPassThrough, buf, buf)
}

// Release unlocks and closes the held volumes; Windows remounts them on the
// next access and the disks spin up on the next I/O.
func (s *session) Release() error {
	var firstErr error
	for _, v := range s.vols {
		if v.locked {
			if err := ioctl(v.handle, diskioctl.FSCTLUnlockVolume, nil, nil); err != nil && firstErr == nil {
				firstErr = fmt.Errorf("unlock %s: %w", v.path, err)
			}
		}
		windows.CloseHandle(v.handle)
		s.c.logf("windisk: %s released", v.path)
	}
	s.vols = nil
	return firstErr
}
