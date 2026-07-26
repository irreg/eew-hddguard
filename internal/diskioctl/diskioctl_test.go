package diskioctl

import (
	"encoding/binary"
	"testing"
)

func TestBuildStandbyImmediate(t *testing.T) {
	buf := BuildStandbyImmediate(10)
	if len(buf) != 48 {
		t.Fatalf("len = %d, want 48 (sizeof ATA_PASS_THROUGH_EX on x64)", len(buf))
	}
	if got := binary.LittleEndian.Uint16(buf[0:]); got != 48 {
		t.Errorf("Length = %d, want 48", got)
	}
	if got := binary.LittleEndian.Uint16(buf[2:]); got != 0x01 {
		t.Errorf("AtaFlags = %#x, want ATA_FLAGS_DRDY_REQUIRED", got)
	}
	if got := binary.LittleEndian.Uint32(buf[8:]); got != 0 {
		t.Errorf("DataTransferLength = %d, want 0", got)
	}
	if got := binary.LittleEndian.Uint32(buf[12:]); got != 10 {
		t.Errorf("TimeOutValue = %d, want 10", got)
	}
	if buf[46] != 0xE0 {
		t.Errorf("command register = %#x, want 0xE0 (STANDBY IMMEDIATE)", buf[46])
	}
	// Everything else must be zero.
	for i, b := range buf {
		switch i {
		case 0, 2, 12, 46:
		default:
			if b != 0 {
				t.Errorf("buf[%d] = %#x, want 0", i, b)
			}
		}
	}
}

func TestBuildStoragePropertyQuery(t *testing.T) {
	buf := BuildStoragePropertyQuery(StorageDeviceSeekPenaltyProperty)
	if len(buf) != 12 {
		t.Fatalf("len = %d, want 12", len(buf))
	}
	if got := binary.LittleEndian.Uint32(buf[0:]); got != 7 {
		t.Errorf("PropertyId = %d, want 7", got)
	}
	if got := binary.LittleEndian.Uint32(buf[4:]); got != 0 {
		t.Errorf("QueryType = %d, want 0 (PropertyStandardQuery)", got)
	}
}

func TestParseSeekPenalty(t *testing.T) {
	buf := make([]byte, 12)
	rot, err := ParseSeekPenalty(buf)
	if err != nil || rot {
		t.Errorf("zeroed descriptor: rot=%v err=%v, want false,nil", rot, err)
	}
	buf[8] = 1
	rot, err = ParseSeekPenalty(buf)
	if err != nil || !rot {
		t.Errorf("penalty set: rot=%v err=%v, want true,nil", rot, err)
	}
	if _, err := ParseSeekPenalty(buf[:4]); err == nil {
		t.Error("short buffer: expected error")
	}
}

func TestParseDiskExtents(t *testing.T) {
	// Two extents: disks 1 and 3.
	buf := make([]byte, 8+2*24)
	binary.LittleEndian.PutUint32(buf[0:], 2)
	binary.LittleEndian.PutUint32(buf[8:], 1)
	binary.LittleEndian.PutUint32(buf[8+24:], 3)
	disks, err := ParseDiskExtents(buf)
	if err != nil {
		t.Fatal(err)
	}
	if len(disks) != 2 || disks[0] != 1 || disks[1] != 3 {
		t.Errorf("disks = %v, want [1 3]", disks)
	}

	if _, err := ParseDiskExtents(buf[:4]); err == nil {
		t.Error("short header: expected error")
	}
	binary.LittleEndian.PutUint32(buf[0:], 100)
	if _, err := ParseDiskExtents(buf); err == nil {
		t.Error("truncated extents: expected error")
	}
}

func TestParseDeviceDescriptor(t *testing.T) {
	buf := make([]byte, 64)
	copy(buf[40:], "WDC ")
	buf[44] = 0
	copy(buf[48:], "WD40EZRZ\x00")
	binary.LittleEndian.PutUint32(buf[12:], 40) // VendorIdOffset
	binary.LittleEndian.PutUint32(buf[16:], 48) // ProductIdOffset
	vendor, product, err := ParseDeviceDescriptor(buf)
	if err != nil {
		t.Fatal(err)
	}
	if vendor != "WDC" || product != "WD40EZRZ" {
		t.Errorf("got %q %q", vendor, product)
	}

	if _, _, err := ParseDeviceDescriptor(buf[:10]); err == nil {
		t.Error("short buffer: expected error")
	}

	// Zero offsets mean "no string".
	zero := make([]byte, 64)
	vendor, product, err = ParseDeviceDescriptor(zero)
	if err != nil || vendor != "" || product != "" {
		t.Errorf("zero offsets: %q %q %v", vendor, product, err)
	}

	// Out-of-range offset is ignored, and an unterminated string is
	// clamped to the buffer end.
	bad := make([]byte, 40)
	binary.LittleEndian.PutUint32(bad[12:], 999)
	binary.LittleEndian.PutUint32(bad[16:], 36)
	copy(bad[36:], "ABCD") // runs to end of buffer, no NUL
	vendor, product, err = ParseDeviceDescriptor(bad)
	if err != nil || vendor != "" || product != "ABCD" {
		t.Errorf("got %q %q %v", vendor, product, err)
	}
}
