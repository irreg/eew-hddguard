//go:build !windows

package main

// attachParentConsole is Windows-only; elsewhere stdout/stderr already work.
func attachParentConsole() {}
