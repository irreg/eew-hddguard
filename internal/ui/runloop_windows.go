//go:build windows

package ui

import (
	_ "embed"

	"fyne.io/systray"
	"github.com/go-toast/toast"
)

//go:embed assets/idle.ico
var iconIdle []byte

//go:embed assets/alert.ico
var iconAlert []byte

// RunLoop runs body with the platform UI. With opts.Tray it spins the
// system tray event loop on the calling goroutine and runs body alongside
// it; the tray disappears when body returns. Without it, body gets a
// headless UI.
func RunLoop(opts Options, logf Logf, body func(UI) int) int {
	if !opts.Tray {
		return body(NewHeadless(logf))
	}
	if logf == nil {
		logf = func(string, ...any) {}
	}
	u := &trayUI{actions: make(chan Action, 8), logf: logf}
	code := 0
	logf("ui: starting system tray")
	systray.Run(func() {
		logf("ui: tray ready")
		systray.SetIcon(iconIdle)
		systray.SetTooltip(opts.Tooltip)
		u.status = systray.AddMenuItem("待機中", "現在の保護状態")
		u.status.Disable()
		systray.AddSeparator()
		test := systray.AddMenuItem("テスト実行", "HDD保護を短時間発動して動作確認します")
		u.release = systray.AddMenuItem("今すぐ解除", "揺れが収まったら保護を早期解除します")
		u.release.Disable()
		systray.AddSeparator()
		quit := systray.AddMenuItem("終了", "eew-hddguardを終了します")
		go func() {
			for {
				select {
				case <-test.ClickedCh:
					u.emit(ActionTest)
				case <-u.release.ClickedCh:
					u.emit(ActionRelease)
				case <-quit.ClickedCh:
					u.emit(ActionQuit)
				}
			}
		}()
		go func() {
			code = body(u)
			systray.Quit()
		}()
	}, nil)
	return code
}

type trayUI struct {
	actions chan Action
	status  *systray.MenuItem
	release *systray.MenuItem
	logf    Logf
}

func (u *trayUI) emit(a Action) {
	select {
	case u.actions <- a:
	default:
		u.logf("ui: dropping action %s (queue full)", a)
	}
}

func (u *trayUI) Actions() <-chan Action { return u.actions }

func (u *trayUI) SetProtected(active bool, detail string) {
	if active {
		systray.SetIcon(iconAlert)
		u.status.SetTitle("保護中: " + detail)
		u.release.Enable()
	} else {
		systray.SetIcon(iconIdle)
		u.status.SetTitle("待機中")
		u.release.Disable()
	}
}

func (u *trayUI) Notify(title, message string) {
	n := toast.Notification{
		AppID:   "eew-hddguard",
		Title:   title,
		Message: message,
	}
	if err := n.Push(); err != nil {
		u.logf("ui: toast notification failed: %v", err)
	}
}
