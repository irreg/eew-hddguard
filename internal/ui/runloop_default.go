//go:build !windows

package ui

// RunLoop runs body with the platform UI. Outside Windows there is no tray,
// so body always gets a headless UI.
func RunLoop(opts Options, logf Logf, body func(UI) int) int {
	if opts.Tray && logf != nil {
		logf("ui: system tray is only available on Windows; running headless")
	}
	return body(NewHeadless(logf))
}
