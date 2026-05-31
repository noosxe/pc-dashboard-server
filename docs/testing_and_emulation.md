# Testing & Emulation Guide

This document outlines the testing, hardware mocking, and connection emulation strategy for the **PC Dashboard Server** daemon. It enables full end-to-end development, verification, and automated testing of the daemon inside virtualized environments (such as Docker, Devcontainers, or CI workflows) where physical target GPUs or Android devices are unavailable.

---

## 1. Local Emulation Architecture

The daemon utilizes modular interfaces (`MetricsReader`, `ADBClient`, and `MPRISManager`) defined in the system design specifications. By activating CLI flags or config options, the production drivers are swapped out for emulated mock engines:

```
[CLI Exec Flags]
   |
   +---> --emulate-metrics  ==> Activates MockMetricsReader & MockMPRISManager
   |
   +---> --mock-adb         ==> Activates MockADBClient (automocks hotplug loops)
```

---

## 2. Hardware Telemetry Emulation (`MockMetricsReader`)

When the daemon is started with the `--emulate-metrics` flag, it skips loading hardware sensors (sysfs, hwmon, NVML) and initializes the `MockMetricsReader`.

### 2.1. Telemetry Simulation Algorithms
To make the dashboard look realistic during client testing, the emulator generates smooth, continuous wave functions representing mock system load rather than static values.

*   **Timestep tracking**: The reader maintains an internal ticker incrementing a local float index ($t$) every second.
*   **CPU Utilization Formula**:
    $$\text{Usage} = 15.0 + \sin(t / 8.0) \cdot 10.0 + \text{random}(-2.0, 2.0)$$
    *Range*: $5.0\%$ to $27.0\%$ with slight jitter.
*   **CPU Temperature Formula**:
    $$\text{Temp} = 40.0 + \sin(t / 8.0) \cdot 5.0 + (\text{Usage} \cdot 0.3)$$
    *Range*: $41.5^\circ\text{C}$ to $53.1^\circ\text{C}$ directly correlated with emulated CPU usage load.
*   **GPU Statistics**:
    *   *Usage*: $30.0 + \cos(t / 15.0) \cdot 15.0 + \text{random}(-1.0, 1.0)$
    *   *Temp*: $50.0 + \cos(t / 15.0) \cdot 8.0 + (\text{GPU\_Usage} \cdot 0.2)$
    *   *VRAM*: Total `8,589,934,592` bytes (8GB); Used bytes dynamically scale in correlation with GPU usage: $3,221,225,472 + (\text{GPU\_Usage} \cdot 20,000,000)$ bytes.
*   **RAM Statistics**:
    *   *Total*: `34,359,738,368` bytes (32GB).
    *   *Used*: Fluctuates slowly: $12,884,901,888 + \sin(t / 30.0) \cdot 1,073,741,824$ bytes.

### 2.2. Media Playback Emulation (`MockMPRISManager`)
When the `--emulate-metrics` flag is enabled, the daemon also instantiates the `MockMPRISManager` to simulate media players and playback events.

#### A. Playback Simulation Algorithms
*   **Track Database**: The emulator maintains a pre-defined array of mock tracks:
    1.  *Track 1*: Title: "Stayin' Alive", Artist: "Bee Gees", Album: "Saturday Night Fever", Length: 284s, Art URL: `https://images.example.com/stayinalive.jpg`
    2.  *Track 2*: Title: "Billie Jean", Artist: "Michael Jackson", Album: "Thriller", Length: 294s, Art URL: `https://images.example.com/billiejean.jpg`
    3.  *Track 3*: Title: "Take On Me", Artist: "a-ha", Album: "Hunting High and Low", Length: 225s, Art URL: `https://images.example.com/takeonme.jpg`
*   **Progress Tracking**: The mock manager updates the current playback position ($p$) every second:
    *   If `PlaybackStatus` is "Playing": $p_{t+1} = p_t + 1,000,000$ microseconds.
    *   If $p \ge \text{Length}$, the manager automatically transitions to the next track in the list and resets $p = 0$.
*   **Command Responses**:
    *   `play_pause` / `play` / `pause`: Updates `PlaybackStatus` dynamically.
    *   `next` / `previous`: Cycles the track pointer, resetting position.
    *   `seek` / `set_position`: Adjusts the playback position variable accordingly (bounded between 0 and track length).
    *   `set_volume`: Directly updates the volume float property (bounded between 0.0 and 1.0).

#### B. Power Profiles Emulation (`MockPowerProfilesManager`)
When the `--emulate-metrics` flag is enabled, the daemon also instantiates the `MockPowerProfilesManager` to simulate system power profiles.

*   **Available Profiles**: The emulator exposes three standard profiles: `power-saver`, `balanced`, and `performance`.
*   **Initial State**: Defaults to `balanced` as the active profile.
*   **Command Responses**:
    - `power_profile_command`: Updates the in-memory active profile name if the requested profile matches one of the three available profiles, and triggers an immediate broadcast of the new `power_profile_state` payload.

---

## 3. ADB Loopback Emulation (`MockADBClient`)

When started with the `--mock-adb` flag, the daemon spins up the `MockADBClient` interface.

### 3.1. Physical Connection Simulation Loop
1.  Upon startup, the mock client blocks for **3 seconds** to simulate booting delays.
2.  After 3 seconds, it pushes a simulated connection event to the daemon's device channel:
    ```go
    DeviceEvent{ Serial: "MOCK_DEVICE_12345", State: StateOnline }
    ```
3.  The daemon picks up this event and initiates the bootstrap sequence:
    *   Calls `WakeDevice` (log outputted by mock client: `[MockADB] Waking screen for serial MOCK_DEVICE_12345`).
    *   Calls `LaunchApp` (log outputted by mock client: `[MockADB] Launching activity com.noosxe.pc_dashboard/MainActivity`).
    *   Calls `ReversePort` (log outputted by mock client: `[MockADB] Reversing device port 12345 to host 12345`).
4.  The WebSocket server begins listening on `127.0.0.1:12345`.
5.  If a SIGINT or config tear-down command is issued, the mock client emits:
    ```go
    DeviceEvent{ Serial: "MOCK_DEVICE_12345", State: StateOffline }
    ```
    This triggers a clean WebSocket closure and logs: `[MockADB] Cleared reverse port tunnels`.

---

## 4. Manual WebSocket Integration Testing

With the daemon running in emulation mode (`--emulate-metrics --mock-adb`), developers can easily verify connection frames without a mobile device.

### 4.1. CLI Socket Testing with `websocat`
[`websocat`](https://github.com/vi/websocat) is a robust command-line utility for interacting with WebSockets.
*   **Install websocat**:
    ```bash
    cargo install websocat # or apt-get install websocat
    ```
*   **Listen to Live Telemetry & Media Updates**:
    ```bash
    websocat ws://127.0.0.1:12345/ws
    ```
    *Output in console (telemetry pushes every 1s, media state on event)*:
    ```json
    {"type":"telemetry","timestamp":1716214001,"data":{"cpu":{"usage_percent":18.45,"temp_celsius":48.2},"gpu":{"usage_percent":42.1,"temp_celsius":59.3,"vram_used_bytes":4063225472,"vram_total_bytes":8589934592},"ram":{"used_bytes":13421772800,"total_bytes":34359738368,"percentage":39.06}}}
    {"type":"media_state","timestamp":1716214002,"data":{"active_players":[{"player_name":"spotify","playback_status":"Playing","volume":0.85,"position_microseconds":45000000,"metadata":{"track_id":"spotify:track:4PTG3Z6ehGkBFm5zOHYGaS","title":"Stayin' Alive","artist":["Bee Gees"],"album":"Saturday Night Fever","art_url":"https://images.example.com/stayinalive.jpg","length_microseconds":284000000}}]}}
    ```
*   **Send Control Command via websocat**:
    Type JSON strings directly into the websocat terminal interface to verify client-to-host actions:
    ```json
    { "type": "ping" }
    ```
    *Daemon Response in terminal*: `{"type":"pong"}`
*   **Send Media Control Commands via websocat**:
    Type JSON strings directly to trigger D-Bus commands on active players:
    ```json
    { "type": "media_command", "player_name": "spotify", "command": "play_pause" }
    ```
*   **Send Power Profile Control Commands via websocat**:
    Type JSON strings directly to request a system power profile transition:
    ```json
    { "type": "power_profile_command", "profile": "power-saver" }
    ```

---

### 4.2. Diagnostic Browser HTML Rig
For a quick visual confirmation, developers can open a simple, zero-dependency HTML file in their host browser to display the live telemetry metrics.

Save the following code as `test_client.html` and open it in a browser:

```html
<!DOCTYPE html>
<html>
<head>
    <title>PC Dashboard Daemon Tester</title>
    <style>
        body { font-family: sans-serif; background: #121212; color: #e0e0e0; padding: 20px; }
        .card { background: #1e1e1e; padding: 15px; border-radius: 8px; margin-bottom: 10px; }
        .val { color: #00e676; font-family: monospace; font-size: 1.2em; }
        button { background: #333; color: white; border: none; padding: 8px 15px; border-radius: 4px; cursor: pointer; margin-right: 5px; }
        button:hover { background: #444; }
    </style>
</head>
<body>
    <h1>PC Dashboard Live Telemetry & Media</h1>
    <div class="card">CPU Usage: <span id="cpu-use" class="val">--</span> % | Temp: <span id="cpu-temp" class="val">--</span> &deg;C</div>
    <div class="card">GPU Usage: <span id="gpu-use" class="val">--</span> % | Temp: <span id="gpu-temp" class="val">--</span> &deg;C</div>
    <div class="card">RAM Usage: <span id="ram-use" class="val">--</span> %</div>
    
    <div class="card">
        <h3>Media Player Controls</h3>
        <div>Active Player: <span id="media-player" class="val">--</span></div>
        <div>Playing: <span id="media-title" class="val">--</span> - <span id="media-artist" class="val">--</span></div>
        <div>Position: <span id="media-position" class="val">--</span>s / <span id="media-length" class="val">--</span>s (<span id="media-status" class="val">--</span>)</div>
        <div style="margin-top: 10px;">
            <button onclick="sendMediaCmd('previous')">Prev</button>
            <button onclick="sendMediaCmd('play_pause')">Play/Pause</button>
            <button onclick="sendMediaCmd('next')">Next</button>
        </div>
    </div>
    
    <script>
        const ws = new WebSocket("ws://127.0.0.1:12345/ws");
        ws.onmessage = (event) => {
            const msg = JSON.parse(event.data);
            if (msg.type === "telemetry") {
                document.getElementById("cpu-use").textContent = msg.data.cpu.usage_percent.toFixed(1);
                document.getElementById("cpu-temp").textContent = msg.data.cpu.temp_celsius.toFixed(1);
                document.getElementById("gpu-use").textContent = msg.data.gpu.usage_percent.toFixed(1);
                document.getElementById("gpu-temp").textContent = msg.data.gpu.temp_celsius.toFixed(1);
                document.getElementById("ram-use").textContent = msg.data.ram.percentage.toFixed(1);
            }
            if (msg.type === "media_state") {
                const active = msg.data.active_players[0];
                if (active) {
                    document.getElementById("media-player").textContent = active.player_name;
                    document.getElementById("media-title").textContent = active.metadata.title;
                    document.getElementById("media-artist").textContent = active.metadata.artist.join(", ");
                    document.getElementById("media-position").textContent = (active.position_microseconds / 1000000).toFixed(0);
                    document.getElementById("media-length").textContent = (active.metadata.length_microseconds / 1000000).toFixed(0);
                    document.getElementById("media-status").textContent = active.playback_status;
                } else {
                    document.getElementById("media-player").textContent = "--";
                    document.getElementById("media-title").textContent = "--";
                    document.getElementById("media-artist").textContent = "--";
                    document.getElementById("media-position").textContent = "--";
                    document.getElementById("media-length").textContent = "--";
                    document.getElementById("media-status").textContent = "--";
                }
            }
        };
        ws.onopen = () => console.log("Connected to PC Dashboard Server");

        function sendMediaCmd(cmd) {
            const player = document.getElementById("media-player").textContent;
            if (player === "--") return;
            ws.send(JSON.stringify({
                type: "media_command",
                player_name: player,
                command: cmd
            }));
        }
    </script>
</body>
</html>
```

---

## 5. Automated Mocking (Unit Tests)

In Go automated test suites (e.g. `metrics_test.go`), hardware interfaces should be mocked without command line flags using Go's interface implementation standard:

```go
package metrics

import "testing"

type ConstantMetricsReader struct{}

func (c *ConstantMetricsReader) ReadCPU() (CPUMetrics, error) {
    return CPUMetrics{UsagePercent: 50.0, TempCelsius: 45.0}, nil
}
func (c *ConstantMetricsReader) ReadRAM() (RAMMetrics, error) {
    return RAMMetrics{UsedBytes: 16000000000, TotalBytes: 32000000000, Percentage: 50.0}, nil
}
func (c *ConstantMetricsReader) ReadGPU() (GPUMetrics, error) {
    return GPUMetrics{UsagePercent: 30.0, TempCelsius: 55.0, VramUsedBytes: 4000000000, VramTotalBytes: 8000000000}, nil
}

func TestTelemetryBroadcaster(t *testing.T) {
    mockReader := &ConstantMetricsReader{}
    // Inject mockReader into WebSocket broadcaster loop...
    metrics, _ := mockReader.ReadCPU()
    if metrics.UsagePercent != 50.0 {
        t.Errorf("Expected mocked CPU load 50.0, got %f", metrics.UsagePercent)
    }
}
```

---

## 6. Devcontainer Testing & Network Boundaries

Developing and verifying the PC Dashboard Server inside a virtualized environment (such as VS Code Devcontainers) introduces distinct networking and hardware boundary challenges for both human developers and LLM agents. 

To achieve full testability without compromising physical environments, the following patterns are established:

### 6.1. Exposing the WebSocket Server (Port Forwarding)
The daemon binds strictly to local loopback `127.0.0.1:12345` inside the devcontainer. To enable diagnostic browser rigs or external clients on the host system to interact with the containerized WebSocket server, port `12345` must be forwarded:
*   Add port `12345` to the `forwardPorts` list in `.devcontainer/devcontainer.json` (completed).
*   This automatically maps `127.0.0.1:12345` on the host machine to the running server in the container, enabling local tools (like browser diagnostic screens and `websocat` on the host machine) to connect transparently.

### 6.2. Connecting to the Host's Physical ADB Server
By default, the daemon attempts to connect to `127.0.0.1:5037` to track physical USB devices. Inside a devcontainer, `127.0.0.1` refers to the container itself, which lacks access to the host's USB controller and real ADB daemon.
To connect to the host's actual ADB server (connected to physical companion devices or Android emulator instances):
1.  **Configure Host Network Resolving**:
    Set the daemon configuration value `adb.server_host` to **`host.docker.internal`** instead of `127.0.0.1`:
    ```yaml
    adb:
      server_host: "host.docker.internal"
      server_port: 5037
    ```
2.  **Enable Host Port Exposure**:
    Ensure the ADB daemon running on the host system is configured to accept TCP loopback connections.
3.  This dynamically routes the socket tracking directly to the host's hardware state, allowing real-world testing of physical USB hotplug events entirely from inside the devcontainer.

### 6.3. Direct ADB TCP Socket Mocking (In-Memory Integration Tests)
To allow LLM agents to execute automated integration tests (such as `go test ./...`) inside headless CI environments or local sandboxes where no ADB server is running at all, tests must mock the TCP protocol layer:
*   **In-Memory Listeners**: Tests can instantiate a native Go TCP listener on a dynamic, local port (e.g. `:0`) and point `TCPADBClient` to it.
*   **Simulating ADB Responses**: The test-side TCP listener parses incoming command streams and returns correct raw ADB hex frames (such as `OKAY` or tab-separated connection strings):
    ```go
    // In-memory mock response to host:track-devices
    conn.Write([]byte("OKAY"))
    conn.Write([]byte("00150123456789ABC\tdevice\n"))
    ```
*   This pattern isolates the network socket logic from external environment states, giving both developers and AI agents instant, deterministic unit test validation of length-prefixed ADB frame parsing.

### 6.4. Session D-Bus Socket Mocking (In-Memory Unit Tests)
In automated unit test pipelines where no D-Bus Session Bus daemon is active (e.g., standard Docker CI runners), the D-Bus communication must be mocked to keep tests independent of the OS state:
*   **Go Mock Interfaces**: Automated tests query the `MPRISManager` interface and inject mock structs (`MockMPRISManager`) instead of launching a live `godbus` socket.
*   **Test Validation**: This enables standard Go tests to assert that media control commands dispatched through the loopback WebSocket server successfully trigger the corresponding method call handlers and state shifts, isolating testing to the daemon orchestrator itself:
    ```go
    func TestMediaCommandDispatch(t *testing.T) {
        mockManager := &MockMPRISManager{}
        // Inject mockManager into orchestrator...
        err := mockManager.SendCommand(context.Background(), "spotify", "play_pause", nil)
        if err != nil {
            t.Errorf("Expected successful command dispatch, got: %v", err)
        }
    }
    ```

