// Command quakeguard receives 緊急地震速報 (EEW) warnings and parks the
// heads of local hard disks — optionally dismounting their volumes so I/O
// cannot wake them — until the shaking has passed.
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

	"quakeguard/internal/config"
	"quakeguard/internal/eew"
	"quakeguard/internal/protect"
	"quakeguard/internal/windisk"
)

// Indirection points so tests can substitute fakes.
var (
	newController = windisk.NewController
	newSource     = func(cfg *config.Config, logf protect.Logf) eew.Source {
		return &eew.P2PQuakeSource{URL: cfg.SourceURL, Logf: logf}
	}
	// simulateHold caps the protection hold used by -simulate.
	simulateHold = 2 * time.Second
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("quakeguard", flag.ContinueOnError)
	flags.SetOutput(stderr)
	cfgPath := flags.String("config", "config.json", "path to config file")
	listDisks := flags.Bool("list-disks", false, "list detected disks and exit")
	simulate := flags.Bool("simulate", false, "run one protect/release cycle and exit")
	dryRun := flags.Bool("dry-run", false, "log disk actions instead of performing them")
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

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	src := newSource(cfg, logf)
	events := make(chan eew.Event, 16)
	errc := make(chan error, 1)
	go func() {
		errc <- src.Run(ctx, events)
		close(events)
	}()

	logf("quakeguard: watching EEW (mode=%s, hold=%s, disks=%v, prefs=%v, dryRun=%v)",
		mode, hold, nums, cfg.WatchPrefs, cfg.DryRun)

	for {
		select {
		case ev := <-events:
			handleEvent(ev, cfg, guard, logf)
		case err := <-errc:
			// Drain events emitted before the source stopped.
			for ev := range events {
				handleEvent(ev, cfg, guard, logf)
			}
			if err == nil || errors.Is(err, context.Canceled) {
				logf("quakeguard: shutting down")
				return 0
			}
			logf("quakeguard: EEW source failed: %v", err)
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

func printDisks(w io.Writer, disks []protect.DiskInfo) {
	fmt.Fprintln(w, "NUM  TYPE  SYSTEM  MODEL                          VOLUMES")
	for _, d := range disks {
		typ := "SSD"
		if d.Rotational {
			typ = "HDD"
		}
		sys := "-"
		if d.System {
			sys = "yes"
		}
		fmt.Fprintf(w, "%-4d %-5s %-7s %-30s %d\n", d.Number, typ, sys, d.Model, len(d.Volumes))
	}
}
