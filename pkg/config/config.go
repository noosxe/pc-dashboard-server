package config

// ServerConfig holds parameters for the WebSocket telemetry server.
type ServerConfig struct {
	Host string `koanf:"host"`
	Port int    `koanf:"port"`
}

// DaemonConfig holds operating controls for the server.
type DaemonConfig struct {
	UpdateIntervalMS int    `koanf:"update_interval_ms"`
	LogLevel         string `koanf:"log_level"`
	LogFormat        string `koanf:"log_format"`
	SocketPath       string `koanf:"socket_path"`
}

// ADBConfig holds target settings to connect and bootstrap Android devices.
type ADBConfig struct {
	ServerHost     string `koanf:"server_host"`
	ServerPort     int    `koanf:"server_port"`
	TargetPackage  string `koanf:"target_package"`
	TargetActivity string `koanf:"target_activity"`
	NoAppControl   bool   `koanf:"no_app_control"`
}

// Config represents the consolidated configuration settings.
type Config struct {
	Server ServerConfig `koanf:"server"`
	Daemon DaemonConfig `koanf:"daemon"`
	ADB    ADBConfig    `koanf:"adb"`
}
