package config

import (
	"os"
	"path/filepath"
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

func TestLoadConfig_TOML(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "config_test_*.toml")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	tomlContent := `
[server]
host = "10.0.0.1"
port = 9876

[daemon]
update_interval_ms = 500
log_level = "debug"
log_format = "json"

[adb]
server_host = "192.168.1.10"
server_port = 5038
target_package = "com.custom.dashboard"
target_activity = "com.custom.MainActivity"
no_app_control = true
`
	if _, err := tmpFile.Write([]byte(tomlContent)); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}
	tmpFile.Close()

	cfg, err := LoadConfig(tmpFile.Name(), nil)
	if err != nil {
		t.Fatalf("failed to load TOML config: %v", err)
	}

	if cfg.Server.Host != "10.0.0.1" {
		t.Errorf("expected server host 10.0.0.1, got %s", cfg.Server.Host)
	}
	if cfg.Server.Port != 9876 {
		t.Errorf("expected server port 9876, got %d", cfg.Server.Port)
	}
	if cfg.Daemon.UpdateIntervalMS != 500 {
		t.Errorf("expected update interval 500, got %d", cfg.Daemon.UpdateIntervalMS)
	}
	if cfg.Daemon.LogLevel != "debug" {
		t.Errorf("expected log level debug, got %s", cfg.Daemon.LogLevel)
	}
	if cfg.Daemon.LogFormat != "json" {
		t.Errorf("expected log format json, got %s", cfg.Daemon.LogFormat)
	}
	if cfg.ADB.ServerHost != "192.168.1.10" {
		t.Errorf("expected ADB server host 192.168.1.10, got %s", cfg.ADB.ServerHost)
	}
	if cfg.ADB.ServerPort != 5038 {
		t.Errorf("expected ADB server port 5038, got %d", cfg.ADB.ServerPort)
	}
	if cfg.ADB.TargetPackage != "com.custom.dashboard" {
		t.Errorf("expected target package com.custom.dashboard, got %s", cfg.ADB.TargetPackage)
	}
	if cfg.ADB.TargetActivity != "com.custom.MainActivity" {
		t.Errorf("expected target activity com.custom.MainActivity, got %s", cfg.ADB.TargetActivity)
	}
	if !cfg.ADB.NoAppControl {
		t.Errorf("expected NoAppControl to be true, got %v", cfg.ADB.NoAppControl)
	}
}

func TestLoadConfig_Probing(t *testing.T) {
	// Create a temporary directory to act as the user's home
	tempHome, err := os.MkdirTemp("", "home_probe_test")
	if err != nil {
		t.Fatalf("failed to create temp home directory: %v", err)
	}
	defer os.RemoveAll(tempHome)

	// Override HOME environment variable
	origHome := os.Getenv("HOME")
	defer os.Setenv("HOME", origHome)
	os.Setenv("HOME", tempHome)

	configDir := filepath.Join(tempHome, ".config", "pc-dashboard")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}

	// 1. Create a config.toml
	tomlPath := filepath.Join(configDir, "config.toml")
	tomlContent := `
[server]
port = 11111
`
	if err := os.WriteFile(tomlPath, []byte(tomlContent), 0644); err != nil {
		t.Fatalf("failed to write config.toml: %v", err)
	}

	// Load config with empty path (should probe and find config.toml)
	cfg, err := LoadConfig("", nil)
	if err != nil {
		t.Fatalf("failed to load config during probing: %v", err)
	}
	if cfg.Server.Port != 11111 {
		t.Errorf("expected probed port 11111 from config.toml, got %d", cfg.Server.Port)
	}

	// 2. Create a config.yml (should take precedence over config.toml)
	ymlPath := filepath.Join(configDir, "config.yml")
	ymlContent := `
server:
  port: 22222
`
	if err := os.WriteFile(ymlPath, []byte(ymlContent), 0644); err != nil {
		t.Fatalf("failed to write config.yml: %v", err)
	}

	cfg, err = LoadConfig("", nil)
	if err != nil {
		t.Fatalf("failed to load config during probing: %v", err)
	}
	if cfg.Server.Port != 22222 {
		t.Errorf("expected probed port 22222 from config.yml, got %d", cfg.Server.Port)
	}

	// 3. Create a config.yaml (should take precedence over config.yml and config.toml)
	yamlPath := filepath.Join(configDir, "config.yaml")
	yamlContent := `
server:
  port: 33333
`
	if err := os.WriteFile(yamlPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("failed to write config.yaml: %v", err)
	}

	cfg, err = LoadConfig("", nil)
	if err != nil {
		t.Fatalf("failed to load config during probing: %v", err)
	}
	if cfg.Server.Port != 33333 {
		t.Errorf("expected probed port 33333 from config.yaml, got %d", cfg.Server.Port)
	}
}
