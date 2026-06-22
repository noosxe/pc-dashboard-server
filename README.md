# PC Dashboard Server

A Go-based server designed for system monitoring, metrics collection, and dashboard services.

> [!WARNING]
> **LLM Agent Co-Authored Codebase**
> The code, documentation, and configuration in this repository are co-authored by an LLM (Large Language Model) Agent. This is a blanket notice for anyone interested in exploring, reviewing, or using this codebase: some code patterns, logic, or scripts may have been generated or updated by an AI agent under user direction. Please review all code carefully before executing or utilizing it in a production environment.

## 🚀 Overview

**PC Dashboard Server** is a lightweight, low-overhead system daemon written in Go for Linux host systems. It works in tandem with a companion Android application ([com.noosxe.pc_dashboard](https://github.com/noosxe/pc-dashboard)) to transform any Android mobile device connected via USB into a dedicated, real-time hardware status monitor and dashboard.

By using physical USB connections instead of local Wi-Fi networks, the system achieves sub-millisecond network latencies, eliminates wireless bandwidth contention, runs securely inside local host loops, and is completely isolated from external network eavesdropping or packet injection.

---## ✨ Features

- **⚡ Lightweight Telemetry Engine**: Asynchronously collects CPU, RAM, and GPU stats every second with a minimal memory footprint (<15MB).
- **💾 Swap and ZRAM Monitoring**: Tracks swap space and aggregates ZRAM statistics (disk size, compression ratios, and memory usage) across devices.
- **📊 Adaptive Sensor Support**: Automatically detects host driver capabilities and permissions to output diagnostic telemetry flags.
- **🔌 Advanced Thermal & Power Telemetry**: Collects active core/edge temperatures, power draw (Watts), and sensor bounds for hardware protection.
- **📱 Physical USB Bootstrapping**: Uses raw ADB TCP sockets on port `5037` to wake screens, launch companion apps, and tunnel WebSockets over USB.
- **🎵 MPRIS Media Player Sync**: Remote controls media players via D-Bus session bindings, including local cover art extraction/encoding.
- **🔔 Desktop Notification Sync**: Bi-directionally proxies desktop notifications, action clicks, and app icons over local loopback.
- **🔒 Active Session Lock Throttling**: Monitors screen lock states to transition companion dashboards to lockscreen interfaces and throttle polling rates.
- **🖥️ Display Power ADB Sync**: Syncs physical monitor sleep/wake power states with the connected Android device's screen.
- **🔋 System Power Profile Management**: Controls and validates active system profiles (`balanced`, `performance`, `power-saver`) via system D-Bus.
- **🛡️ Secure Loopback Binding**: Exposes zero network ports by binding all WebSocket and ConnectRPC APIs strictly to the `127.0.0.1` interface.
- **⚙️ Modular Configuration Engine**: Uses `koanf` to cleanly merge static YAML/TOML configs, environment variables, and CLI parameters.
- **📊 Full Sandbox Emulation**: Supports complete metrics, ADB, D-Bus, and MPRIS mocking to run/test anywhere without physical hardware.
- **❄️ Nix Flake & NixOS Module**: Includes reproducible developer shells and direct declarative user systemd service deployments on NixOS, featuring automated ADB server lifecycle management.

---

## 📚 Documentation Index

To keep this repository clean and easy to navigate, detailed guides and references have been organized into the following documents:

*   **[Installation Guide](docs/installation.md)**: Prerequisites, compiler/source builds, CPU permissions setup, and NixOS package deployment.
*   **[Usage & Configuration Guide](docs/usage.md)**: Command-line parameters, trigger subcommands, and YAML/TOML/environment configuration schemas.
*   **[Architecture & API Reference](docs/architecture.md)**: Data flows, WebSocket JSON protocol messages, and standard systemd user daemon setups.
*   **[Technical Specifications](docs/specifications.md)**: Core functional requirements, sensors path mappings, and security policies.
*   **[Design Document](docs/design_document.md)**: Code architecture layout, package boundaries, and interface definitions.
*   **[Protocol Specification](docs/protocol_specification.md)**: ADB TCP length-prefixed protocol frames and custom JSON structures.
*   **[ConnectRPC Specification](docs/connectrpc_specification.md)**: Plaintext HTTP/2 Connect-protocol endpoint references and streaming schemas.
*   **[Testing & Emulation Guide](docs/testing_and_emulation.md)**: Metrics simulation formulas, mock ADB connection cycles, and testing mock environments.
*   **[Agent Developer Guide](AGENTS.md)**: Git branch structures, commit regulations, and feature lifecycle workflows.

---

## 🗺️ Roadmap

We are continuously expanding the capabilities of the PC Dashboard ecosystem. Below are the key initiatives currently planned or underway:

### 1. 🔔 Desktop Notification Actions (D-Bus) 🟡 *[Design Phase]*
Integrate with the Linux host's D-Bus session bus to correlate system-assigned notification IDs and allow the companion Android app to trigger action buttons (e.g. Reply, Dismiss, Custom actions) on intercepted notifications and close them remotely.
- **Outbound Stream**: Intercept both method calls and method returns of desktop notifications, correlate their properties using call/reply serial numbers, and push events complete with unique notification IDs and action options.
- **Inbound Commands**: Support WebSocket commands from the companion app to execute a notification action (`notification_action_command`) or close/dismiss a notification (`notification_dismiss_command`).
- *Status*: Detailed design and protocols have been established. Awaiting design review and approval.

### 2. 🔵 Bluetooth Device Monitoring (D-Bus / BlueZ) 🟡 *[Design Phase]*
Passively monitor host Bluetooth devices via the Linux BlueZ D-Bus system service and stream real-time events to the companion Android app.
- **Outbound Stream**: Emit `connected`, `disconnected`, and `updated` events whenever a Bluetooth device connects, disconnects, or changes battery/RSSI. Push a full `connected_devices` snapshot to newly connected clients. Cache state for instant synchronization.
- **Periodic Battery & RSSI Reporting**: Poll `org.bluez.Battery1.Percentage` and `org.bluez.Device1.RSSI` for connected devices at a configurable interval (default 30s). Only emit updates when values actually change.
- **Event-Driven Architecture**: Uses `GetManagedObjects` bootstrap, `InterfacesAdded`/`InterfacesRemoved` and `PropertiesChanged` D-Bus signals for zero-poll connect/disconnect detection. No active scanning or device mutation.
- **Emulation Support**: Dedicated `--mock-bluetooth` flag activates `MockBluetoothManager` with a scripted 3-device roster (headphones, keyboard, game controller) simulating a realistic connection sequence, battery drain, and RSSI oscillation.
- *Status*: Architecture and protocol design established. Awaiting design review and approval.
### 3. 🎛️ On-Screen Display (OSD) Events Sync 🟡 *[Design Phase]*
Synchronize master audio volume levels, mute states, and keyboard indicator lock statuses (Caps Lock, Num Lock, Scroll Lock) in real-time.
- **Outbound Stream**: Broadcast volume percent, mute state, and Lock key states immediately upon host adjustments.
- **Mechanisms**: Subscribes to PulseAudio (`pactl subscribe`) changes and runs a 200ms polling loop on `/sys/class/leds/` keyboard LED files.
- **Emulation Support**: Includes `--mock-osd` command-line flag and `MockOSDManager` to simulate OSD toggles locally.
- *Status*: Protocol schema and design established. Awaiting design review and approval.

### 4. 🌡️ CPU/GPU Critical Tmax Metrics 🟡 *[Design Phase]*
Expose maximum thermal limits (`tmax_celsius`) alongside CPU and GPU temperatures to allow companion apps to dynamically render temperature charts relative to host threshold scales.
- **Mechanisms**: Reads `/sys/class/hwmon/hwmon*/temp*_crit` for AMD/Intel CPUs and GPUs, and queries NVIDIA NVML thermal thresholds slowdown/shutdown points.
- *Status*: Telemetry schemas updated. Awaiting design review and approval.

### 5. ⚙️ Modular Subsystem Configuration Toggles 🟡 *[Design Phase]*
Support selective initialization of daemon subsystems (Metrics, ADB, MPRIS, Notifications, Session Lock, Power Profiles, Bluetooth, OSD, Peripherals, Package Updates) via configuration properties.
- **Mechanisms**: Configured via the `modules` block in the YAML config file, with corresponding environment variables and command-line flags (e.g., `--disable-mpris`).
- *Status*: Configuration schemas updated. Awaiting design review and approval.

### 6. 🖱️ Keyboard & Mouse Peripherals Telemetry 🟡 *[Design Phase]*
Monitor battery capacity levels, charging states, and nominal polling frequencies for connected wireless keyboards and mice.
- **Mechanisms**: Accesses `org.freedesktop.UPower` on the system D-Bus to capture peripheral device statistics, and parses sysfs USB descriptors (`bInterval`) to compute nominal device polling frequencies in Hertz (Hz).
- **Emulation Support**: Dedicated `--mock-peripherals` flag triggers a simulation of mouse/keyboard batteries draining in memory.
- *Status*: Design and interfaces established. Awaiting design review and approval.

### 7. 📦 Package Manager Updates indications 🟡 *[Design Phase]*
Provide a live notification count of available package updates and standard security updates on the host Linux machine.
- **Mechanisms**: Subscribes to PackageKit's system D-Bus signals (`UpdatesChanged`) and runs asynchronous transactions to count updates; includes a periodic fallback parser reading `/var/lib/update-notifier/updates-available`.
- **Emulation Support**: Supported via `--mock-package-updates` flag.
- *Status*: Design and interfaces established. Awaiting design review and approval.

### 8. 🚀 Host App Launcher 🟡 *[Design Phase]*
Allow launching pre-configured host applications (e.g., Steam, Discord, browsers) directly from the companion Android app dashboard.
- **Strict Whitelist Execution Model**: To prevent Remote Code Execution (RCE) vulnerabilities, arbitrary terminal commands or scripts are strictly prohibited. The daemon will only spawn processes mapping to predefined keys declared in the user's local `config.yaml`.
- **Inbound WebSocket Command**: Implements a `launch_app_command` payload containing a valid whitelisted `app_key`.
- **Inherited Session Context**: Spawns GUI applications asynchronously within the user's systemd session context, automatically resolving graphical display settings (`DISPLAY`, `WAYLAND_DISPLAY`).
- *Status*: Protocol schema, configuration keys, and security constraints established. Awaiting design review and approval.

### 10. ❄️ NixOS NVIDIA GPU Path Propagation 🟡 *[Design Phase]*
Automatically propagate the host's NVIDIA driver package into the systemd user service PATH on NixOS systems.
- **Mechanisms**: Dynamically query `config.hardware.nvidia.enable`. If enabled, append `config.hardware.nvidia.package` to the `path` list of the `systemd.user.services.pc-dashboard-server` unit.
- **Benefits**: Resolves "command not found" errors when executing `nvidia-smi` inside systemd user service contexts without requiring manual user path overrides.

### 9. ⚡ Additional Planned Enhancements
- **🌐 Network & Disk I/O Metrics**: Add real-time network throughput (upload/download rates) and disk read/write bandwidth metrics to the telemetry payload.
- **🔋 Battery & Power States**: Support tracking connected Android device power/battery telemetry or power state flags to hibernate/resume polling loops.

---

## 💻 Development & Contributing

All active development is expected to take place within the provided **Devcontainer** (`.devcontainer`). It has pre-installed tools and environments to support smooth and secure contributions.

Refer to the [Agent Developer Guide](AGENTS.md) for branch policies, check-out requirements, and code style rules before starting development or opening a pull request.

