package config

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"quakeguard/internal/eew"
)

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDefault(t *testing.T) {
	cfg := Default()
	if cfg.SourceURL != eew.DefaultP2PQuakeURL {
		t.Errorf("SourceURL = %q", cfg.SourceURL)
	}
	if cfg.Mode != "dismount" || cfg.HoldSeconds != 180 {
		t.Errorf("defaults wrong: %+v", cfg)
	}
	if !cfg.ExcludeSystem() {
		t.Error("ExcludeSystem should default to true")
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("defaults must validate: %v", err)
	}
}

func TestLoadMinimal(t *testing.T) {
	cfg, err := Load(writeConfig(t, `{}`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Mode != "dismount" || cfg.HoldSeconds != 180 || cfg.SourceURL == "" {
		t.Errorf("defaults not applied: %+v", cfg)
	}
}

func TestLoadFull(t *testing.T) {
	cfg, err := Load(writeConfig(t, `{
		"sourceUrl": "wss://example.test/ws",
		"watchPrefs": ["東京都", "神奈川県"],
		"mode": "standby",
		"holdSeconds": 60,
		"disks": [1, 2],
		"excludeSystemDisk": false,
		"allowTest": true,
		"dryRun": true
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SourceURL != "wss://example.test/ws" || cfg.Mode != "standby" ||
		cfg.HoldSeconds != 60 || len(cfg.WatchPrefs) != 2 ||
		len(cfg.Disks) != 2 || !cfg.AllowTest || !cfg.DryRun {
		t.Errorf("fields not loaded: %+v", cfg)
	}
	if cfg.ExcludeSystem() {
		t.Error("excludeSystemDisk=false not honored")
	}
}

func TestLoadErrors(t *testing.T) {
	tests := map[string]string{
		"bad mode":        `{"mode": "yolo"}`,
		"bad holdSeconds": `{"holdSeconds": -5}`,
		"unknown field":   `{"modee": "standby"}`,
		"invalid json":    `{`,
	}
	for name, content := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(writeConfig(t, content)); err == nil {
				t.Error("expected error")
			}
		})
	}
}

func TestLoadMissingFile(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "nope.json"))
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("err = %v, want fs.ErrNotExist", err)
	}
}

func TestValidateEmptyFields(t *testing.T) {
	cfg := &Config{HoldSeconds: 30}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	if cfg.SourceURL == "" || cfg.Mode == "" {
		t.Errorf("empty fields not defaulted: %+v", cfg)
	}
}

func TestExcludeSystemExplicitTrue(t *testing.T) {
	v := true
	cfg := &Config{ExcludeSystemDisk: &v}
	if !cfg.ExcludeSystem() {
		t.Error("explicit true not honored")
	}
}
