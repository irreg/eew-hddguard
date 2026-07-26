// Package config loads the quakeguard configuration file.
package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"

	"quakeguard/internal/eew"
	"quakeguard/internal/protect"
)

// Config is the on-disk configuration (JSON).
type Config struct {
	// SourceURL is the P2P地震情報 WebSocket endpoint.
	SourceURL string `json:"sourceUrl"`
	// WatchPrefs limits triggering to warnings whose areas include one of
	// these prefectures (e.g. "東京都"). Empty means trigger on any warning.
	WatchPrefs []string `json:"watchPrefs"`
	// Mode is "standby" (park heads only) or "dismount" (also lock and
	// dismount volumes so I/O cannot wake the disk during the hold).
	Mode string `json:"mode"`
	// HoldSeconds is how long protection lasts after the last trigger.
	HoldSeconds int `json:"holdSeconds"`
	// Disks lists explicit physical disk numbers to protect. Empty means
	// all non-system rotational disks.
	Disks []int `json:"disks"`
	// ExcludeSystemDisk refuses to touch the disk holding Windows.
	// Defaults to true.
	ExcludeSystemDisk *bool `json:"excludeSystemDisk"`
	// AllowTest also triggers on test/training messages.
	AllowTest bool `json:"allowTest"`
	// DryRun logs disk actions instead of performing them.
	DryRun bool `json:"dryRun"`
	// LogFile additionally appends logs to this file (useful when running
	// without a console, e.g. as a tray app).
	LogFile string `json:"logFile"`
}

// Default returns the built-in defaults: dismount mode, 180 s hold, all
// non-system HDDs, any warning area.
func Default() *Config {
	return &Config{
		SourceURL:   eew.DefaultP2PQuakeURL,
		Mode:        protect.ModeDismount.String(),
		HoldSeconds: 180,
	}
}

// Load reads and validates the config file at path, applying defaults for
// omitted fields.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	cfg := Default()
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, nil
}

// Validate checks field values, filling empty ones with defaults.
func (c *Config) Validate() error {
	if c.SourceURL == "" {
		c.SourceURL = eew.DefaultP2PQuakeURL
	}
	if c.Mode == "" {
		c.Mode = protect.ModeDismount.String()
	}
	if _, err := protect.ParseMode(c.Mode); err != nil {
		return err
	}
	if c.HoldSeconds <= 0 {
		return fmt.Errorf("holdSeconds must be positive, got %d", c.HoldSeconds)
	}
	return nil
}

// ExcludeSystem returns the effective excludeSystemDisk setting.
func (c *Config) ExcludeSystem() bool {
	if c.ExcludeSystemDisk == nil {
		return true
	}
	return *c.ExcludeSystemDisk
}
