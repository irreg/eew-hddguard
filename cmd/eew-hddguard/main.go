// Command eew-hddguard receives 緊急地震速報 (EEW) warnings and parks the
// heads of local hard disks — optionally dismounting their volumes so I/O
// cannot wake them — until the shaking has passed. On Windows it lives in
// the system tray with manual test / early-release actions and toast
// notifications.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"eew-hddguard/internal/config"
	"eew-hddguard/internal/eew"
	"eew-hddguard/internal/protect"
	"eew-hddguard/internal/ui"
	"eew-hddguard/internal/windisk"
)

// Indirection points so tests can substitute fakes.
var (
	newController = windisk.NewController
	newSource     = func(cfg *config.Config, logf protect.Logf) eew.Source {
		return &eew.P2PQuakeSource{URL: cfg.SourceURL, Logf: logf}
	}
	runUI = ui.RunLoop
	// simulateHold caps the protection hold used by -simulate.
	simulateHold = 2 * time.Second
	// manualTestHold is the protection hold for the tray "テスト実行" action.
	manualTestHold = 10 * time.Second
)

func main() {
	attachParentConsole()
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("eew-hddguard", flag.ContinueOnError)
	flags.SetOutput(stderr)
	cfgPath := flags.String("config", "config.json", "path to config file")
	listDisks := flags.Bool("list-disks", false, "list detected disks and exit")
	simulate := flags.Bool("simulate", false, "run one protect/release cycle and exit")
	dryRun := flags.Bool("dry-run", false, "log disk actions instead of performing them")
	noTray := flags.Bool("no-tray", false, "run headless without the system tray")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	logger := log.New(stderr, "", log.LstdFlags)
	logf := logger.Printf

	cfg, err := config.Load(*cfgPath)
	if errors.Is(err, fs.ErrNotExist) {
		logf("config %s not found; using defaults", *cfgPath)
		cfg = config.Default()
	} else if err != nil {
		logf("config error: %v", err)
		return 1
	}
	if *dryRun {
		cfg.DryRun = true
	}
	if cfg.LogFile != "" {
		f, err := os.OpenFile(cfg.LogFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			logf("cannot open log file: %v", err)
			return 1
		}
		defer f.Close()
		// The file goes first and stderr is wrapped to swallow errors:
		// launched without a console (task scheduler, Explorer) stderr is
		// an invalid handle whose write failure would otherwise make
		// io.MultiWriter stop before reaching the file.
		logger.SetOutput(io.MultiWriter(f, bestEffortWriter{stderr}))
	}
	mode, _ := protect.ParseMode(cfg.Mode)

	ctrl, err := newController(cfg.DryRun, logf)
	if err != nil {
		logf("controller: %v", err)
		return 1
	}
	all, err := ctrl.ListDisks()
	if err != nil {
		logf("disk enumeration failed: %v", err)
		return 1
	}
	if *listDisks {
		printDisks(stdout, all)
		return 0
	}

	targets, err := protect.SelectDisks(all, cfg.Disks, cfg.ExcludeSystem())
	if err != nil {
		logf("disk selection: %v", err)
		return 1
	}
	if len(targets) == 0 {
		logf("no target disks found (no non-system HDD detected); nothing to protect")
		return 1
	}
	var nums []int
	for _, d := range targets {
		nums = append(nums, d.Number)
	}

	hold := time.Duration(cfg.HoldSeconds) * time.Second
	if *simulate && hold > simulateHold {
		hold = simulateHold
	}
	guard := protect.NewGuard(ctrl, targets, mode, hold, logf)
	defer guard.Close()

	if *simulate {
		logf("simulate: triggering protection for %s", hold)
		if err := guard.Trigger("simulated EEW"); err != nil {
			return 1
		}
		time.Sleep(hold + 250*time.Millisecond)
		return 0
	}

	opts := ui.Options{Tray: !*noTray, Tooltip: "eew-hddguard — 緊急地震速報でHDDヘッドを退避"}
	return runUI(opts, ui.Logf(logf), func(u ui.UI) int {
		guard.SetOnChange(func(active bool, reason string) {
			u.SetProtected(active, reason)
			if active {
				u.Notify("eew-hddguard: HDD保護を発動しました",
					reason+" — ヘッドを退避しています。揺れが収まったらメニューの「今すぐ解除」で早期復旧できます。")
			} else {
				u.Notify("eew-hddguard: HDD保護を解除しました", reason)
			}
		})
		defer guard.Close()
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		logf("eew-hddguard: watching EEW (mode=%s, hold=%s, disks=%v, prefs=%v, dryRun=%v)",
			mode, hold, nums, cfg.WatchPrefs, cfg.DryRun)
		return monitor(ctx, cfg, guard, newSource(cfg, logf), u, logf)
	})
}

// monitor is the main event loop: EEW events trigger the guard, UI actions
// drive manual tests and early release, and a source failure or context
// cancellation ends it.
func monitor(ctx context.Context, cfg *config.Config, guard *protect.Guard, src eew.Source, u ui.UI, logf protect.Logf) int {
	events := make(chan eew.Event, 16)
	errc := make(chan error, 1)
	go func() {
		errc <- src.Run(ctx, events)
		close(events)
	}()

	for {
		select {
		case ev := <-events:
			handleEvent(ev, cfg, guard, logf)
		case act := <-u.Actions():
			switch act {
			case ui.ActionTest:
				logf("ui: manual test requested (%s hold)", manualTestHold)
				guard.TriggerFor("手動テスト", manualTestHold)
			case ui.ActionRelease:
				logf("ui: early release requested")
				guard.ReleaseNow("手動解除")
			case ui.ActionQuit:
				logf("ui: quit requested")
				return 0
			}
		case err := <-errc:
			// Drain events emitted before the source stopped.
			for ev := range events {
				handleEvent(ev, cfg, guard, logf)
			}
			if err == nil || errors.Is(err, context.Canceled) {
				logf("eew-hddguard: shutting down")
				return 0
			}
			logf("eew-hddguard: EEW source failed: %v", err)
			return 1
		}
	}
}

func handleEvent(ev eew.Event, cfg *config.Config, guard *protect.Guard, logf protect.Logf) {
	ok, reason := eew.ShouldTrigger(ev, cfg.WatchPrefs, cfg.AllowTest)
	if !ok {
		logf("eew: ignoring event %s#%s (%s)", ev.EventID, ev.Serial, reason)
		return
	}
	logf("eew: WARNING %s#%s hypocenter=%s M%.1f areas=%v",
		ev.EventID, ev.Serial, ev.Hypocenter, ev.Magnitude, ev.Prefs)
	guard.Trigger(reason)
}

// bestEffortWriter forwards writes but never reports failure, so one dead
// destination cannot abort an io.MultiWriter chain.
type bestEffortWriter struct{ w io.Writer }

func (b bestEffortWriter) Write(p []byte) (int, error) {
	b.w.Write(p)
	return len(p), nil
}

func printDisks(w io.Writer, disks []protect.DiskInfo) {
	if len(disks) == 0 {
		fmt.Fprintln(w, "物理ディスクを検出できませんでした。管理者権限で実行しているか確認してください。")
		return
	}
	fmt.Fprintln(w, "NUM  TYPE  SYSTEM  MODEL                          VOLUMES")
	for _, d := range disks {
		typ := "SSD"
		if d.Rotational {
			typ = "HDD"
		}
		if d.Virtual {
			typ = "SPACE"
		}
		sys := "-"
		if d.System {
			sys = "yes"
		}
		fmt.Fprintf(w, "%-4d %-5s %-7s %-30s %d\n", d.Number, typ, sys, d.Model, len(d.Volumes))
	}
}
