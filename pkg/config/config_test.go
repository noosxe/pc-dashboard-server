package config

import (
	"testing"
)

func TestLoadConfig_Defaults(t *testing.T) {
	cfg, err := LoadConfig("", nil)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	if cfg.Server.Host != "127.0.0.1" {
		t.Errorf("expected default server host 127.0.0.1, got %s", cfg.Server.Host)
	}

	if cfg.Server.Port != 12345 {
		t.Errorf("expected default server port 12345, got %d", cfg.Server.Port)
	}

	if cfg.Daemon.UpdateIntervalMS != 1000 {
		t.Errorf("expected default update interval 1000, got %d", cfg.Daemon.UpdateIntervalMS)
	}

	if cfg.ADB.ServerPort != 5037 {
		t.Errorf("expected default ADB port 5037, got %d", cfg.ADB.ServerPort)
	}

	if cfg.ADB.NoAppControl {
		t.Errorf("expected default NoAppControl to be false, got %v", cfg.ADB.NoAppControl)
	}
}

func TestLoadConfig_CLIOverrides(t *testing.T) {
	overrides := map[string]interface{}{
		"server.port":        54321,
		"adb.no_app_control": true,
	}

	cfg, err := LoadConfig("", overrides)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	if cfg.Server.Port != 54321 {
		t.Errorf("expected overridden server port 54321, got %d", cfg.Server.Port)
	}

	if !cfg.ADB.NoAppControl {
		t.Errorf("expected overridden adb.no_app_control to be true, got %v", cfg.ADB.NoAppControl)
	}

	// Unchanged default value should remain
	if cfg.Server.Host != "127.0.0.1" {
		t.Errorf("expected default server host 127.0.0.1, got %s", cfg.Server.Host)
	}
}
