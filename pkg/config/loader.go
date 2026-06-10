package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/knadh/koanf/parsers/toml"
	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/providers/structs"
	"github.com/knadh/koanf/v2"
)

// DefaultConfig provides compile-time fallback settings.
func DefaultConfig() Config {
	var socketPath string
	xdgRuntime := os.Getenv("XDG_RUNTIME_DIR")
	if xdgRuntime != "" {
		socketPath = filepath.Join(xdgRuntime, "pc-dashboard-server.sock")
	} else {
		socketPath = filepath.Join(os.TempDir(), "pc-dashboard-server.sock")
	}

	return Config{
		Server: ServerConfig{
			Host: "127.0.0.1",
			Port: 12345,
		},
		Daemon: DaemonConfig{
			UpdateIntervalMS:       1000,
			LockedUpdateIntervalMS: 5000,
			LogLevel:               "info",
			LogFormat:              "text",
			SocketPath:             socketPath,
		},
		ADB: ADBConfig{
			ServerHost:     "127.0.0.1",
			ServerPort:     5037,
			TargetPackage:  "com.noosxe.pc_dashboard",
			TargetActivity: "com.noosxe.pc_dashboard.MainActivity",
			NoAppControl:   false,
		},
	}
}

// LoadConfig resolves application settings. It merges the internal defaults,
// optional YAML file (found at configPath or ~/.config/pc-dashboard/config.yaml),
// environment variables (prefixed with PCD_), and command line flags map.
func LoadConfig(configPath string, cliFlags map[string]interface{}) (*Config, error) {
	k := koanf.New(".")

	// 1. Load internal defaults
	defaults := DefaultConfig()
	if err := k.Load(structs.Provider(defaults, "koanf"), nil); err != nil {
		return nil, fmt.Errorf("failed to load default config: %w", err)
	}

	// 2. Load optional configuration file
	if configPath == "" {
		homeDir, err := os.UserHomeDir()
		if err == nil {
			candidates := []string{
				filepath.Join(homeDir, ".config", "pc-dashboard", "config.yaml"),
				filepath.Join(homeDir, ".config", "pc-dashboard", "config.yml"),
				filepath.Join(homeDir, ".config", "pc-dashboard", "config.toml"),
			}
			for _, candidate := range candidates {
				if _, err := os.Stat(candidate); err == nil {
					configPath = candidate
					break
				}
			}
		}
	}

	if configPath != "" {
		if _, err := os.Stat(configPath); err == nil {
			var parser koanf.Parser
			if strings.HasSuffix(configPath, ".toml") {
				parser = toml.Parser()
			} else {
				parser = yaml.Parser()
			}
			if err := k.Load(file.Provider(configPath), parser); err != nil {
				return nil, fmt.Errorf("failed to load config file %s: %w", configPath, err)
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("error reading config path: %w", err)
		}
	}

	// 3. Load environment variables prefixed with PCD_
	// Mapping PCD_SERVER_PORT to server.port, PCD_DAEMON_UPDATE_INTERVAL_MS to daemon.update_interval_ms, etc.
	err := k.Load(env.Provider("PCD_", ".", func(s string) string {
		trimmed := strings.TrimPrefix(s, "PCD_")
		lowered := strings.ToLower(trimmed)
		return strings.ReplaceAll(lowered, "_", ".")
	}), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to load env config: %w", err)
	}

	// 4. Merge CLI flags (if any)
	// We iterate through map keys to selectively override parsed values.
	for key, val := range cliFlags {
		if val == nil {
			continue
		}
		// Skip empty strings/zeros to avoid blindly overriding with flag defaults
		if s, ok := val.(string); ok && s == "" {
			continue
		}
		if i, ok := val.(int); ok && i == 0 {
			continue
		}
		if err := k.Set(key, val); err != nil {
			return nil, fmt.Errorf("failed to set CLI flag override %s: %w", key, err)
		}
	}

	var conf Config
	if err := k.Unmarshal("", &conf); err != nil {
		return nil, fmt.Errorf("failed to unmarshal configuration: %w", err)
	}

	return &conf, nil
}
