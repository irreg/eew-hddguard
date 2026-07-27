//go:build windows

package main

import (
	"os"

	"golang.org/x/sys/windows"
)

// ATTACH_PARENT_PROCESS for kernel32!AttachConsole.
const attachParentProcess = ^uintptr(0)

var procAttachConsole = windows.NewLazySystemDLL("kernel32.dll").NewProc("AttachConsole")

// attachParentConsole connects this process to the console it was launched
// from, if any. A binary built with -H=windowsgui gets no console of its
// own, so without this, -list-disks output and flag errors would be
// silently discarded when run from PowerShell/cmd. Started from Explorer
// or the task scheduler there is no parent console and this is a no-op.
func attachParentConsole() {
	// Remember which std handles were already provided (e.g. output
	// redirected to a file); those must not be clobbered.
	outMissing := stdHandleMissing(windows.STD_OUTPUT_HANDLE)
	errMissing := stdHandleMissing(windows.STD_ERROR_HANDLE)
	if r, _, _ := procAttachConsole.Call(attachParentProcess); r == 0 {
		return
	}
	if f, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0); err == nil {
		if outMissing {
			os.Stdout = f
		}
		if errMissing {
			os.Stderr = f
		}
	}
}

func stdHandleMissing(which uint32) bool {
	h, err := windows.GetStdHandle(which)
	return err != nil || h == 0 || h == windows.InvalidHandle
}
