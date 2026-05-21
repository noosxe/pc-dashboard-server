# Protocol Specification: PC Dashboard Server & Android Companion

This document provides the exhaustive protocol specifications for physical link initialization, ADB socket bootstrapping, and bidirectional WebSocket message formats between the **PC Dashboard Server** (Go Daemon) and the **Android Companion Client**.

---

## 1. Protocol Architecture & Layers

```
+-------------------------------------------------------------+
|               Application Layer: WebSocket JSON             |
| (Telemetry/Media pushes, client command events, ping/pong)  |
+-------------------------------------------------------------+
|             Transport Layer: WebSocket (RFC 6455)           |
|                Binding strictly to 127.0.0.1                |
+-------------------------------------------------------------+
|             Tunnelling Layer: ADB Reverse Forward           |
|               Tunnels USB Port 12345 to 12345               |
+-------------------------------------------------------------+
|               Link Layer: Physical USB Connection           |
+-------------------------------------------------------------+
```

---

## 2. ADB Host-Server Socket Protocol

Before the application WebSocket channel is established, the Go daemon interacts directly with the local ADB server over TCP loopback (`127.0.0.1:5037`). This direct socket communication utilizes ADB’s length-prefixed framing.

### 2.1. Frame Format
All ADB commands and responses sent over the TCP socket are prefixed with a **4-character hexadecimal length string** representing the length of the command payload (excluding the prefix itself).

$$\text{Frame Structure} = \overbrace{\text{[4 Hex Chars (Length)]}}^{\text{UTF-8 Hex Length}} + \overbrace{\text{[Payload String]}}^{\text{UTF-8 Command Payload}}$$

*   *Example Request*: `host:version` (12 bytes) -> `000chost:version`

---

### 2.2. Handshake & Setup Sequence

#### Step 1: Physical Device Monitoring
The daemon establishes a persistent socket connection to ADB and listens for hotplug events using `host:track-devices`.
1.  **Daemon sends**: `0012host:track-devices`
2.  **ADB Server responds**: `OKAY` (4-byte handshake confirmation).
3.  **ADB Server streams** updates upon hardware change:
    *   *Payload format*: `[4-char hex length][device_serial]\t[state]\n`
    *   *Connection Event*: `00150123456789ABC    device\n` (Note: `device` corresponds to `online`)
    *   *Disconnection Event*: `00160123456789ABC    offline\n`

#### Step 2: Waking Target Screen
Once an authorized device serial enters the `device` state, the daemon sends a wakeup key event to prevent sleeping layouts:
1.  **Daemon sends**: `0012host:transport:[serial]` (e.g. `001fhost:transport:0123456789ABC`)
2.  **ADB Server responds**: `OKAY`
3.  **Daemon sends**: `001fshell:input keyevent KEYCODE_WAKEUP`
4.  **ADB Server responds**: `OKAY` followed by command execution response, then closes connection.

#### Step 3: Launching Android Dashboard Companion App
The daemon requests a shell launch of the pre-installed package activity:
1.  **Daemon sends**: `0012host:transport:[serial]`
2.  **ADB Server responds**: `OKAY`
3.  **Daemon sends**: `004eshell:am start -n com.noosxe.pc_dashboard/com.noosxe.pc_dashboard.MainActivity`
4.  **ADB Server responds**: `OKAY` followed by execution confirmation streams.

#### Step 4: Configuring USB Reverse Tunneling
To allow the Android app to connect locally to the host PC, the daemon reverses the port:
1.  **Daemon sends**: `0012host:transport:[serial]`
2.  **ADB Server responds**: `OKAY`
3.  **Daemon sends**: `0026reverse:forward:tcp:12345;tcp:12345`
4.  **ADB Server responds**: `OKAY`

#### Step 5: Closing Companion Application on Daemon Exit
To prevent leaving a stale monitoring UI when the Go daemon exits, it sends a command to stop the companion application package on all active, connected devices:
1.  **Daemon sends**: `0012host:transport:[serial]`
2.  **ADB Server responds**: `OKAY`
3.  **Daemon sends**: `002bshell:am force-stop com.noosxe.pc_dashboard`
4.  **ADB Server responds**: `OKAY` followed by execution confirmation, then closes connection.

---


## 3. Bidirectional WebSocket JSON API

With the ADB reverse tunnel active, the Android app initiates a standard WebSocket handshake pointing to `ws://127.0.0.1:12345/ws`. 

All messages are represented as UTF-8 encoded text frames carrying valid JSON structures.

---

### 3.1. Outbound Telemetry Payload (Host → Android Client)
Pushed continuously every **1000ms**.

#### JSON Schema Spec
```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "title": "TelemetryPush",
  "type": "object",
  "required": ["type", "timestamp", "data"],
  "properties": {
    "type": { "type": "string", "const": "telemetry" },
    "timestamp": { "type": "integer", "description": "Unix timestamp in seconds" },
    "data": {
      "type": "object",
      "required": ["cpu", "gpu", "ram"],
      "properties": {
        "cpu": {
          "type": "object",
          "required": ["usage_percent", "temp_celsius"],
          "properties": {
            "usage_percent": { "type": "number", "minimum": 0, "maximum": 100 },
            "temp_celsius": { "type": "number" }
          }
        },
        "gpu": {
          "type": "object",
          "required": ["usage_percent", "temp_celsius", "vram_used_bytes", "vram_total_bytes"],
          "properties": {
            "usage_percent": { "type": "number", "minimum": 0, "maximum": 100 },
            "temp_celsius": { "type": "number" },
            "vram_used_bytes": { "type": "integer", "minimum": 0 },
            "vram_total_bytes": { "type": "integer", "minimum": 0 }
          }
        },
        "ram": {
          "type": "object",
          "required": ["used_bytes", "total_bytes", "percentage"],
          "properties": {
            "used_bytes": { "type": "integer", "minimum": 0 },
            "total_bytes": { "type": "integer", "minimum": 0 },
            "percentage": { "type": "number", "minimum": 0, "maximum": 100 }
          }
        }
      }
    }
  }
}
```

---

### 3.2. Inbound Control Payloads (Android Client → Host)

#### A. Keepalive Connection (Ping)
Ensures socket states remain alive. If the daemon receives a client ping, it responds with a pong frame within 100ms.
*   **Request Packet**:
    ```json
    { "type": "ping" }
    ```
*   **Response Packet**:
    ```json
    { "type": "pong" }
    ```

#### B. Daemon Config Update Event
Fired when the client requests adjustment of telemetry parameters (e.g. dynamic interval changes).
*   **Request Packet**:
    ```json
    {
      "type": "config",
      "settings": {
        "interval_ms": 500
      }
    }
    ```
*   **Fields**:
    *   `settings.interval_ms` (`integer`): Telemetry frequency target. Minimum value: `100` (10Hz max poll), Maximum value: `10000` (10s poll).

#### C. System Action Commands
Triggered by physical actions on the dashboard interface.
*   **Request Packet**:
    ```json
    {
      "type": "action",
      "command": "suspend"
    }
    ```
*   **Supported Commands**:
    *   `suspend`: Puts the Linux host system into low-power sleep (via systemd logind interfaces safely).
    *   `disconnect`: Requests clean shutdown of telemetry loops for the specific device session.

---

### 3.3. Outbound Media State Payload (Host → Android Client)
This is an event-driven payload pushed asynchronously by the daemon whenever:
* A new MPRIS player starts or stops (is detected on the D-Bus session bus).
* An active player changes its playback status (e.g. paused to playing).
* An active player changes the current track (metadata updates).
* The volume or position changes significantly.

#### JSON Schema Spec
```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "title": "MediaStatePush",
  "type": "object",
  "required": ["type", "timestamp", "data"],
  "properties": {
    "type": { "type": "string", "const": "media_state" },
    "timestamp": { "type": "integer", "description": "Unix timestamp in seconds" },
    "data": {
      "type": "object",
      "required": ["active_players"],
      "properties": {
        "active_players": {
          "type": "array",
          "items": {
            "type": "object",
            "required": ["player_name", "playback_status", "volume", "position_microseconds", "metadata"],
            "properties": {
              "player_name": { "type": "string" },
              "playback_status": { "type": "string", "enum": ["Playing", "Paused", "Stopped"] },
              "volume": { "type": "number", "minimum": 0.0, "maximum": 1.0 },
              "position_microseconds": { "type": "integer", "minimum": 0 },
              "metadata": {
                "type": "object",
                "required": ["track_id", "title", "artist", "album", "art_url", "length_microseconds"],
                "properties": {
                  "track_id": { "type": "string" },
                  "title": { "type": "string" },
                  "artist": { "type": "array", "items": { "type": "string" } },
                  "album": { "type": "string" },
                  "art_url": { "type": "string", "format": "uri" },
                  "length_microseconds": { "type": "integer", "minimum": 0 }
                }
              }
            }
          }
        }
      }
    }
  }
}
```

#### JSON Payload Example
```json
{
  "type": "media_state",
  "timestamp": 1716213825,
  "data": {
    "active_players": [
      {
        "player_name": "spotify",
        "playback_status": "Playing",
        "volume": 0.85,
        "position_microseconds": 45120000,
        "metadata": {
          "track_id": "spotify:track:4PTG3Z6ehGkBFm5zOHYGaS",
          "title": "Stayin' Alive",
          "artist": ["Bee Gees"],
          "album": "Saturday Night Fever",
          "art_url": "https://i.scdn.co/image/ab67616d0000b27382b243023b937ebe57acfac2",
          "length_microseconds": 284000000
        }
      }
    ]
  }
}
```

---

### 3.4. Inbound Media Command Payload (Android Client → Host)
The companion app can transmit media commands to control any active player on the host PC. The daemon will parse these commands and invoke the matching D-Bus methods on the specified MPRIS player service.

#### Request JSON Schema
```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "title": "MediaCommand",
  "type": "object",
  "required": ["type", "player_name", "command"],
  "properties": {
    "type": { "type": "string", "const": "media_command" },
    "player_name": { "type": "string", "description": "The name of the target player (e.g. spotify)" },
    "command": { 
      "type": "string", 
      "enum": ["play", "pause", "play_pause", "stop", "next", "previous", "seek", "set_position", "set_volume"] 
    },
    "args": {
      "type": "object",
      "properties": {
        "offset_microseconds": { "type": "integer", "description": "Used with command: seek" },
        "position_microseconds": { "type": "integer", "description": "Used with command: set_position" },
        "track_id": { "type": "string", "description": "Used with command: set_position" },
        "volume": { "type": "number", "minimum": 0.0, "maximum": 1.0, "description": "Used with command: set_volume" }
      }
    }
  }
}
```

#### JSON Payload Examples

* **Play/Pause Toggle**:
  ```json
  {
    "type": "media_command",
    "player_name": "spotify",
    "command": "play_pause"
  }
  ```

* **Next Track**:
  ```json
  {
    "type": "media_command",
    "player_name": "spotify",
    "command": "next"
  }
  ```

* **Set Volume to 70%**:
  ```json
  {
    "type": "media_command",
    "player_name": "spotify",
    "command": "set_volume",
    "args": {
      "volume": 0.7
    }
  }
  ```

* **Seek Relative Forward by 10s**:
  ```json
  {
    "type": "media_command",
    "player_name": "spotify",
    "command": "seek",
    "args": {
      "offset_microseconds": 10000000
    }
  }
  ```

---

## 4. Connection State Machine & Recovery

```
                      +-------------------+
                      |   USB Disposed    |
                      +---------+---------+
                                |
                                | USB Hotplug Detect
                                v
                      +-------------------+
                      |   Device Online   |
                      +---------+---------+
                                |
                                | ADB Bootstrap Setup
                                v
                      +-------------------+
                      | Tunnel Provisioned|
                      +---------+---------+
                                |
                                | Ws Handshake Connect
                                v
                      +-------------------+
            +-------->|  Active Streaming |
            |         +---------+---------+
            |                   |
            | Keepalive Ping    | Connection Interrupted /
            |                   | USB unplugged
            +-------------------+
                                v
                      +-------------------+
                      |   Offline Loop    |
                      |   (Tear down)     |
                      +-------------------+
```

1.  **Heartbeat Timeout**: The daemon maintains a read timeout of **5 seconds** for WebSocket connections. If no telemetry frame acknowledgement or `ping` is received within 5 seconds, the WebSocket connection is dropped.
2.  **Resource Cleanup**: Upon WebSocket disconnection, the daemon tears down the device-specific reverse tunnel mapping (`adb reverse --remove tcp:12345`) to prevent stale system port states.
3.  **USB Reconnect**: If the device is unplugged and reattached, the `track-devices` socket triggers a new `online` flow, initiating the full ADB handshake sequence from Step 1.
