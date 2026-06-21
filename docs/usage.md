# Usage & Configuration Guide

This guide details how to configure, run, and interact with the **PC Dashboard Server** daemon.

## Quick Start

1. Start the ADB daemon:
   ```bash
   adb start-server
   ```
2. Launch the PC Dashboard Server:
   ```bash
   pc-dashboard-server start
   ```
3. Connect your Android device with USB debugging enabled, and watch the server automatically bootstrap the app and start streaming live statistics.

---

## Command-Line Interface (CLI)

The server exposes the `start` subcommand to boot the daemon along with several flags to customize its execution.

### Subcommands

- `start`: Launches the telemetry aggregation engine, USB auto-discovery socket, and the loopback WebSocket server.
- `trigger`: Connects to the active background daemon via a local Unix socket to trigger various simulated events (e.g. lock/unlock transitions, notification alerts, media play states, or raw custom JSON) down the WebSocket pipe to active companion devices, receiving execution confirmations immediately.

### Flags for `start`

| Flag | Shorthand | Default | Description |
| :--- | :--- | :--- | :--- |
| `--config` | `-c` | `""` | Path to the YAML/TOML configuration file |
| `--port` | `-p` | `12345` | Overrides the WebSocket listening port |
| `--emulate-metrics`| | `false` | Enables simulated telemetry metrics using smooth sine-wave algorithms |
| `--mock-adb` | | `false` | Simulates USB hotplug connection ticks for local testing |
| `--mock-notifications`| | `false` | Simulates desktop D-Bus notification events for local testing |
| `--mock-lock` | | `false` | Simulates session lock/unlock events for local testing |
| `--log-level` | | `"info"` | Sets structured logging level (`debug`, `info`, `warn`, `error`) |
| `--log-format` | | `"text"` | Sets structured log output format (`text`, `json`) |
| `--verbose` | `-v` | `false` | Unconditionally forces log level to `debug` |
| `--no-app-control`| | `false` | Prevents launching or closing the companion Android app |

*Example (Emulation/Mock Mode for Sandbox Testing):*
```bash
pc-dashboard-server start --emulate-metrics --mock-adb --verbose
```

### Trigger Subcommands

The `trigger` command connects to the active background daemon via a local Unix socket to trigger simulated events down the WebSocket stream. It supports several event category subcommands:

*   `lock` / `unlock`: Simulates screen locking/unlocking.
*   `dpms`: Simulates display power management state transitions.
    *   `--state`: The display power state, either `on` or `off` (default `"off"`).
*   `notification`: Dispatches mock desktop notifications.
    *   `--app-name`: Name of the triggering application (default `"pc-dashboard"`).
    *   `--replaces-id`: The notification ID to replace/update (default `0`).
    *   `--icon`: The icon name or filepath (default `"dialog-information"`).
    *   `--summary`: The notification title summary (default `"Alert"`).
    *   `--body`: The detailed notification body (default `"System trigger action simulated."`).
    *   `--actions`: Comma-separated list of actions (e.g. `dismiss,Dismiss`).
    *   `--timeout`: Expire timeout in milliseconds (default `-1`).
*   `media`: Dispatches MPRIS player updates.
    *   `--player`: The target media player name (default `"spotify"`).
    *   `--status`: Playback status, either `Playing`, `Paused`, or `Stopped` (default `"Playing"`).
    *   `--volume`: Player volume ratio between `0.0` and `1.0` (default `0.75`).
    *   `--position`: Current track position in microseconds (default `45000000`).
    *   `--track-id`: Unique metadata track ID/URI (default `"spotify:track:uds-track"`).
    *   `--title`: Track song title (default `"UDS Trigger Track"`).
    *   `--artist`: Track artist names (comma-separated, default `"Antigravity,Agent"`).
    *   `--album`: Track album title (default `"UDS Testing Album"`).
    *   `--art-url`: Album cover art image URL (default `"https://localhost/art.png"`).
    *   `--length`: Track duration in microseconds (default `180000000`).
*   `telemetry`: Dispatches metrics reports and capability flags.
    *   **Metrics Flags**:
        *   `--cpu-usage`: Overall CPU usage percentage (default `25.5`).
        *   `--cpu-cores-usage`: Comma-separated list of individual core usage percentages (default `"10.0,20.0,30.0,40.0"`).
        *   `--cpu-temp`: Overall CPU package temperature in Celsius (default `45.0`).
        *   `--cpu-freq`: CPU active scaling frequency in MHz (default `2500.0`).
        *   `--cpu-power`: CPU package power consumption in Watts (default `35.0`).
        *   `--ram-used`: System RAM bytes used (default `8589934592` / 8GB).
        *   `--ram-total`: Total system RAM capacity in bytes (default `17179869184` / 16GB).
        *   `--ram-percentage`: Specific RAM usage percentage (if `0`, calculated dynamically).
        *   `--gpu-usage`: Overall GPU usage percentage (default `15.0`).
        *   `--gpu-temp`: Overall GPU temperature in Celsius (default `50.0`).
        *   `--gpu-freq`: GPU engine clock speed in MHz (default `1200.0`).
        *   `--gpu-power`: GPU active power consumption in Watts (default `75.0`).
        *   `--gpu-mem-used`: GPU VRAM bytes used (default `2147483648` / 2GB).
        *   `--gpu-mem-total`: Total GPU VRAM capacity in bytes (default `8589934592` / 8GB).
        *   `--gpu-vram-temp`: GPU VRAM temperature in Celsius (default `55.0`).
        *   `--gpu-vram-freq`: GPU VRAM clock speed in MHz (default `1500.0`).
    *   **Telemetry Support / Capability Flags (default `true`)**:
        *   `--cpu-usage-supported`: CPU usage support flag.
        *   `--cpu-cores-usage-supported`: CPU core usage support flag.
        *   `--cpu-temp-supported`: CPU temperature support flag.
        *   `--cpu-freq-supported`: CPU frequency support flag.
        *   `--cpu-power-supported`: CPU power support flag.
        *   `--ram-supported`: RAM metrics support flag.
        *   `--gpu-supported`: GPU metrics support flag.
        *   `--gpu-usage-supported`: GPU usage support flag.
        *   `--gpu-temp-supported`: GPU temperature support flag.
        *   `--gpu-vram-supported`: GPU VRAM stats support flag.
        *   `--gpu-freq-supported`: GPU frequency support flag.
        *   `--gpu-power-supported`: GPU power draw support flag.
        *   `--gpu-vram-temp-supported`: GPU VRAM temperature support flag.
        *   `--gpu-vram-freq-supported`: GPU VRAM frequency support flag.
*   `power`: Dispatches power profile state updates.
    *   `--active`: The active power profile (default `"balanced"`).
    *   `--available`: Comma-separated list of available power profiles (default `"power-saver,balanced,performance"`).
*   `raw`: Dispatches arbitrary passthrough payloads.
    *   `--type` / `-t`: Custom type wrapper name (required).
    *   `--data` / `-d`: Valid JSON string payload to broadcast (required).

*Examples:*
```bash
# Trigger a session lock screen
pc-dashboard-server trigger lock

# Trigger a display power off event
pc-dashboard-server trigger dpms --state off

# Trigger a custom notification toast
pc-dashboard-server trigger notification --summary "Antigravity Alert" --body "Everything is operating correctly"

# Trigger a mock power profile state
pc-dashboard-server trigger power --active performance --available power-saver,balanced,performance

# Trigger a raw custom payload
pc-dashboard-server trigger raw -t custom_sensor -d '{"utilization": 85.5}'
```

---

## Configuration Management

The server merges configurations dynamically from the following sources (ordered from highest precedence to lowest):

1. **CLI Flags** (e.g. `--port 12345`)
2. **Environment Variables** prefixed with `PCD_`
3. **Local Configuration File**: Probed and loaded in order from `~/.config/pc-dashboard/config.yaml` (YAML), `config.yml` (YAML), or `config.toml` (TOML).
4. **Internal Default Settings**

### Local YAML Configuration File (`config.yaml`)

Create a custom YAML file to define persistent properties:

```yaml
server:
  host: "127.0.0.1"          # Strict loopback binding (strongly recommended)
  port: 12345                # WebSocket server listening port

daemon:
  update_interval_ms: 1000   # Polling frequency for host statistics
  log_level: "info"          # Logger level (debug, info, warn, error)
  log_format: "text"         # Output style (text or json)

adb:
  server_host: "127.0.0.1"   # Host address of your local ADB daemon
  server_port: 5037          # Port of your local ADB daemon
  target_package: "com.noosxe.pc_dashboard"
  target_activity: "com.noosxe.pc_dashboard.MainActivity"
  no_app_control: false      # Prevents launching or closing the companion Android app

modules:
  metrics: true              # Toggle host metrics telemetry collection
  adb: true                  # Toggle ADB device connection tracking
  mpris: true                # Toggle MPRIS media player remote controls
  notifications: true        # Toggle D-Bus notifications forwarding
  lock: true                 # Toggle session lock/unlock monitoring
  power_profiles: true       # Toggle power profile control loop
  bluetooth: true            # Toggle BlueZ Bluetooth device monitoring
  osd: true                  # Toggle OSD volume & lock key status alerts
  peripherals: true          # Toggle keyboard & mouse battery tracking
  package_updates: true      # Toggle package manager updates checking
```

### Local TOML Configuration File (`config.toml`)

Alternatively, create a custom TOML file to define persistent properties:

```toml
[server]
host = "127.0.0.1"          # Strict loopback binding (strongly recommended)
port = 12345                # WebSocket server listening port

[daemon]
update_interval_ms = 1000   # Polling frequency for host statistics
log_level = "info"          # Logger level (debug, info, warn, error)
log_format = "text"         # Output style (text or json)

[adb]
server_host = "127.0.0.1"   # Host address of your local ADB daemon
server_port = 5037          # Port of your local ADB daemon
target_package = "com.noosxe.pc_dashboard"
target_activity = "com.noosxe.pc_dashboard.MainActivity"
no_app_control = false      # Prevents launching or closing the companion Android app

[modules]
metrics = true              # Toggle host metrics telemetry collection
adb = true                  # Toggle ADB device connection tracking
mpris = true                # Toggle MPRIS media player remote controls
notifications = true        # Toggle D-Bus notifications forwarding
lock = true                 # Toggle session lock/unlock monitoring
power_profiles = true       # Toggle power profile control loop
bluetooth = true            # Toggle BlueZ Bluetooth device monitoring
osd = true                  # Toggle OSD volume & lock key status alerts
peripherals = true          # Toggle keyboard & mouse battery tracking
package_updates = true      # Toggle package manager updates checking
```

### Environment Variables

Environment variables are prefixed with `PCD_` and nested by replacing underscores with dots. For example, `PCD_SERVER_PORT` maps to `server.port`. 

> [!NOTE]
> For configuration keys that have embedded underscores within their leaf name (such as `log_level` or `update_interval_ms`), environmental maps will undergo underscore-to-dot translation (e.g., yielding `daemon.log.level`). To override nested leaf properties with underscores, it is highly recommended to configure them via the YAML configuration file or CLI flags.
