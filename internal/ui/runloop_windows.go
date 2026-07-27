//go:build windows

package ui

import (
	_ "embed"
	"time"

	"fyne.io/systray"
	"github.com/go-toast/toast"
)

//go:embed assets/idle.ico
var iconIdle []byte

//go:embed assets/alert.ico
var iconAlert []byte

// trayReadyTimeout bounds how long RunLoop waits for the tray to come up.
// In a non-interactive session (e.g. a scheduled task not marked "run only
// when user is logged on") there is no notification area and the tray loop
// never becomes ready; protection must not be held hostage by the UI.
const trayReadyTimeout = 10 * time.Second

// RunLoop runs body with the platform UI. With opts.Tray it starts the
// system tray event loop and runs body once the tray is ready; if the tray
// cannot initialize in time, body runs with a headless UI instead. The
// tray disappears when body returns.
func RunLoop(opts Options, logf Logf, body func(UI) int) int {
	if !opts.Tray {
		return body(NewHeadless(logf))
	}
	if logf == nil {
		logf = func(string, ...any) {}
	}
	u := &trayUI{actions: make(chan Action, 8), logf: logf}
	ready := make(chan struct{})
	logf("ui: starting system tray")
	go systray.Run(func() {
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
		close(ready)
	}, nil)
	select {
	case <-ready:
		defer systray.Quit()
		return body(u)
	case <-time.After(trayReadyTimeout):
		logf("ui: system tray not ready after %s — likely a non-interactive session; continuing without tray (register the task with LogonType Interactive to get the tray)", trayReadyTimeout)
		return body(NewHeadless(logf))
	}
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
