# Design Document: System Libraries & Toolchain Selection

This document outlines the selected tools, libraries, and protocols to implement the **PC Dashboard Server** daemon in Go. The libraries are chosen to satisfy lightweight operations, security boundaries, performance efficiency, and cross-compilation safety.

---

## 1. Programming Language & Core Platform

*   **Language**: Go (Golang) `1.26.3`
*   **Rationale**: Go is ideal for system daemons. It compiles into a single, dependency-free static binary that executes with extremely low memory overhead (~10–15 MB). Go has powerful concurrency primitives (`goroutines`, `channels`, `select`) which allow managing independent monitoring loops, ADB tracking connections, and WebSocket clients concurrently without thread exhaustion.

---

## 2. Component Toolchain & Dependency Matrix

| Component Area | Selected Tool / Library | Role & Rationale | CGO Dependency | Security / Safety Profile |
| :--- | :--- | :--- | :--- | :--- |
| **CLI & Command Routing** | `github.com/spf13/cobra` | Base command and subcommand routing (e.g. `start`, `config`). Already integrated. | None (Pure Go) | **Safe**: No command injection vulnerabilities; handles flag parsing through rigid type-safe interfaces. |
| **Host System Metrics** | `github.com/shirou/gopsutil/v4` | High-level tracking of host CPU ticks and Virtual Memory (RAM) statistics. | None (Pure Go) | **Safe**: Standard library-backed `/proc` and `/sys` parser. Does not load unverified binary libraries. |
| **Linux Temperature Sensors** | Native `/sys` Reader (`os`, `io`) | Native sysfs node parser (`/sys/class/thermal` and `/sys/class/hwmon`). | None (Pure Go) | **Safe**: Handled via secure read-only standard file APIs. Avoids external dependencies entirely. |
| **NVIDIA GPU & VRAM** | `github.com/NVIDIA/go-nvml` | Official Go bindings for NVIDIA Management Library (NVML) to get temp, utilization, and memory metrics. | **Yes** (Requires `libnvidia-ml.so` at runtime) | **Safe**: Communicates through official NVIDIA driver libraries. Includes safe query bindings. |
| **AMD/Intel GPU & VRAM** | Native `/sys` Reader (`os`, `io`) | Reads GPU busy percent and VRAM metrics from open-source Linux kernel interfaces. | None (Pure Go) | **Safe**: Simple, direct filesystem access; immune to binary exploitation. |
| **ADB USB Protocol Interface** | Direct TCP Protocol Wrapper / `github.com/zach-klippenstein/goadb` | Establishes a TCP streaming socket with local ADB server (`127.0.0.1:5037`) for tracking and reverse tunneling. | None (Pure Go) | **Highly Safe**: By interacting directly over standard TCP rather than executing external shell commands, it completely eliminates shell injection vectors. |
| **WebSocket Networking** | `github.com/gorilla/websocket` or `nhooyr.io/websocket` | Standard Go framework for high-throughput, asynchronous WebSocket routing. | None (Pure Go) | **Highly Safe**: Complies fully with RFC 6455. Restricts listener strictly to the local loopback `127.0.0.1`. |
| **Configuration Management** | `github.com/knadh/koanf/v2` | Lightweight, extensible configuration engine supporting YAML configs, environment variables, and CLI flags. | None (Pure Go) | **Safe**: Minimal, modern codebase. Parses strict schemas without arbitrary code execution vectors. |

---

## 3. Library Deep Dives & Architectural Rationales

### 3.1. Telemetry Reader: `gopsutil` vs Custom Parsing
*   **Decision**: Use `gopsutil/v4` for general OS metrics (CPU usage ticks, total/available memory).
*   **Rationale**: `gopsutil` handles OS differences dynamically and operates with zero CGO dependencies on Linux (it reads directly from `/proc/stat` and `/proc/meminfo` using Go standard library).
*   **Fallback Strategy**: To retrieve CPU/GPU temperatures, the daemon falls back to standard sysfs pathways. Caching directory descriptors prevents high-frequency directory walking.

### 3.2. ADB Communication: Pure Socket Protocol vs `adb` CLI Binary Execution
*   **Decision**: Use pure TCP sockets to connect directly to the background ADB daemon on port `5037`.
*   **Rationale**: 
    1.  **Safety**: Invoking external binaries like `exec.Command("adb", "devices")` introduces risks of argument injection and binary search path manipulation. Socket interfaces eliminate these issues.
    2.  **Performance**: Streaming hotplug notifications via `host:track-devices` over a persistent TCP socket has near-zero overhead. Spawning new system processes to run `adb` commands every second wastes CPU cycles.
*   **Mechanism**:
    *   Open TCP connection to `127.0.0.1:5037`.
    *   Write formatted frame headers, such as:
        *   `0012host:track-devices` (for hotplug tracking).
        *   `001Fhost:transport:<serial>` followed by `0015reverse:forward:tcp:12345;tcp:12345` (for reverse port configuration).

### 3.3. WebSocket Engine: `gorilla/websocket`
*   **Decision**: Standardize on `github.com/gorilla/websocket` for real-time messaging.
*   **Rationale**: It is the most robust, battle-tested Go WebSocket library. It includes mature mechanisms for:
    1.  Handling connection loss through Keepalive Pings/Pongs.
    2.  Framing telemetry data as compact, serialized UTF-8 JSON.
    3.  Throttling and writing concurrency guards (safe concurrent connection writes).

### 3.4. Configuration Engine: `koanf`
*   **Decision**: Use `github.com/knadh/koanf/v2` as the unified config parsing and state management layer.
*   **Rationale**:
    1.  **Extreme Efficiency**: `koanf` is significantly lighter and faster than alternatives like `viper`, using an extremely clean, modular design.
    2.  **Extensible Providers & Parsers**: Easily merges configurations from multiple layers (defaults -> YAML/JSON file -> Env Variables -> CLI flags) using a structured abstraction.
    3.  **Strict Typings**: Safe configuration mapping using `koanf.Unmarshal` into strongly typed Go configuration structs, preventing runtime type coercion errors.

---

## 4. Security Integration (TODO(security))

*   **Secure Dependency Verification**: Every library will be checked to confirm it contains zero known CVEs (Common Vulnerabilities and Exposures) prior to final inclusion.
*   **Local TCP Boundary**:
    WebSocket routes are restricted to the local loopback (`127.0.0.1`). Physical access via a USB connection (secured via ADB client authentication keys on the host machine) is the single path of access to the WebSocket port, keeping the surface area completely isolated.
