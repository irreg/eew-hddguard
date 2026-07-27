// Package diskioctl builds and parses the raw buffers exchanged with
// Windows disk IOCTLs. Everything here is pure byte manipulation so it can
// be unit-tested on any OS; the actual DeviceIoControl calls live in
// internal/windisk.
package diskioctl

import (
	"encoding/binary"
	"fmt"
)

// Windows IOCTL / FSCTL codes used by eew-hddguard.
const (
	IOCTLATAPassThrough             = 0x0004D02C
	IOCTLStorageQueryProperty       = 0x002D1400
	IOCTLVolumeGetVolumeDiskExtents = 0x00560000
	FSCTLLockVolume                 = 0x00090018
	FSCTLUnlockVolume               = 0x0009001C
	FSCTLDismountVolume             = 0x00090020
)

// STORAGE_PROPERTY_QUERY property IDs.
const (
	StorageDeviceProperty            = 0
	StorageDeviceSeekPenaltyProperty = 7
)

const (
	// ataPassThroughExSize is sizeof(ATA_PASS_THROUGH_EX) on 64-bit Windows.
	ataPassThroughExSize   = 48
	ataFlagsDrdyRequired   = 0x01
	ataCmdStandbyImmediate = 0xE0
	// currentTaskFileOffset is the offset of CurrentTaskFile within
	// ATA_PASS_THROUGH_EX; the command register is byte 6 of the task file.
	currentTaskFileOffset = 40
	commandRegisterOffset = currentTaskFileOffset + 6
)

// BuildStandbyImmediate returns an ATA_PASS_THROUGH_EX buffer carrying the
// STANDBY IMMEDIATE (E0h) command, for IOCTL_ATA_PASS_THROUGH on a 64-bit
// system. The same buffer is used for input and output.
func BuildStandbyImmediate(timeoutSec uint32) []byte {
	buf := make([]byte, ataPassThroughExSize)
	binary.LittleEndian.PutUint16(buf[0:], ataPassThroughExSize) // Length
	binary.LittleEndian.PutUint16(buf[2:], ataFlagsDrdyRequired) // AtaFlags
	binary.LittleEndian.PutUint32(buf[12:], timeoutSec)          // TimeOutValue
	buf[commandRegisterOffset] = ataCmdStandbyImmediate
	return buf
}

// BuildStoragePropertyQuery returns a STORAGE_PROPERTY_QUERY buffer for
// IOCTL_STORAGE_QUERY_PROPERTY (QueryType = PropertyStandardQuery).
func BuildStoragePropertyQuery(propertyID uint32) []byte {
	buf := make([]byte, 12)
	binary.LittleEndian.PutUint32(buf[0:], propertyID)
	// QueryType (offset 4) = 0, AdditionalParameters zeroed.
	return buf
}

// ParseSeekPenalty parses a DEVICE_SEEK_PENALTY_DESCRIPTOR. A device that
// incurs a seek penalty is a rotational (spinning) disk.
func ParseSeekPenalty(buf []byte) (rotational bool, err error) {
	if len(buf) < 9 {
		return false, fmt.Errorf("seek penalty descriptor too short: %d bytes", len(buf))
	}
	return buf[8] != 0, nil
}

// ParseDiskExtents parses a VOLUME_DISK_EXTENTS buffer and returns the
// physical disk numbers the volume spans.
func ParseDiskExtents(buf []byte) ([]uint32, error) {
	if len(buf) < 8 {
		return nil, fmt.Errorf("disk extents buffer too short: %d bytes", len(buf))
	}
	n := binary.LittleEndian.Uint32(buf[0:])
	const headerSize, extentSize = 8, 24
	if uint64(len(buf)) < headerSize+uint64(n)*extentSize {
		return nil, fmt.Errorf("disk extents buffer truncated: %d extents in %d bytes", n, len(buf))
	}
	disks := make([]uint32, 0, n)
	for i := uint32(0); i < n; i++ {
		disks = append(disks, binary.LittleEndian.Uint32(buf[headerSize+i*extentSize:]))
	}
	return disks, nil
}

// BusTypeSpaces is the STORAGE_BUS_TYPE of a Storage Spaces virtual disk.
const BusTypeSpaces = 17

// ParseDeviceDescriptor extracts the vendor and product strings and the bus
// type from a STORAGE_DEVICE_DESCRIPTOR buffer.
func ParseDeviceDescriptor(buf []byte) (vendor, product string, busType uint32, err error) {
	if len(buf) < 36 {
		return "", "", 0, fmt.Errorf("device descriptor too short: %d bytes", len(buf))
	}
	vendor = cstringAt(buf, binary.LittleEndian.Uint32(buf[12:]))  // VendorIdOffset
	product = cstringAt(buf, binary.LittleEndian.Uint32(buf[16:])) // ProductIdOffset
	busType = binary.LittleEndian.Uint32(buf[28:])
	return vendor, product, busType, nil
}

// cstringAt reads a NUL-terminated ASCII string at off, trimming spaces.
// Returns "" for a zero or out-of-range offset.
func cstringAt(buf []byte, off uint32) string {
	if off == 0 || uint64(off) >= uint64(len(buf)) {
		return ""
	}
	end := off
	for end < uint32(len(buf)) && buf[end] != 0 {
		end++
	}
	s := string(buf[off:end])
	// Device strings are space-padded.
	start, stop := 0, len(s)
	for start < stop && s[start] == ' ' {
		start++
	}
	for stop > start && s[stop-1] == ' ' {
		stop--
	}
	return s[start:stop]
}
