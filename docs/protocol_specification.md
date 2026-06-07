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
      "required": ["cpu", "gpu", "ram", "peripherals", "package_updates", "flags"],
      "properties": {
        "cpu": {
          "type": "object",
          "required": ["usage_percent", "temp_celsius", "power_watts", "tmax_celsius"],
          "properties": {
            "usage_percent": { "type": "number", "minimum": 0, "maximum": 100 },
            "temp_celsius": { "type": "number" },
            "power_watts": { "type": "number", "minimum": 0 },
            "tmax_celsius": { "type": "number", "description": "Maximum thermal throttle threshold in Celsius; 0.0 if unsupported" }
          }
        },
        "gpu": {
          "type": "object",
          "required": ["usage_percent", "temp_celsius", "vram_used_bytes", "vram_total_bytes", "power_watts", "vram_temp_celsius", "vram_freq_mhz", "tmax_celsius"],
          "properties": {
            "usage_percent": { "type": "number", "minimum": 0, "maximum": 100 },
            "temp_celsius": { "type": "number" },
            "vram_used_bytes": { "type": "integer", "minimum": 0 },
            "vram_total_bytes": { "type": "integer", "minimum": 0 },
            "power_watts": { "type": "number", "minimum": 0 },
            "vram_temp_celsius": { "type": "number", "description": "VRAM memory temperature in Celsius; 0.0 if unsupported" },
            "vram_freq_mhz": { "type": "number", "minimum": 0, "description": "VRAM memory clock speed in MHz; 0.0 if unsupported" },
            "tmax_celsius": { "type": "number", "description": "Maximum thermal throttle threshold in Celsius; 0.0 if unsupported" }
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
        },
        "peripherals": {
          "type": "object",
          "required": ["devices"],
          "properties": {
            "devices": {
              "type": "array",
              "items": {
                "type": "object",
                "required": ["name", "type", "battery_percent", "charging_state", "poll_rate_hz"],
                "properties": {
                  "name": { "type": "string" },
                  "type": { "type": "string", "enum": ["keyboard", "mouse"] },
                  "battery_percent": { "type": "number", "minimum": 0, "maximum": 100 },
                  "charging_state": { "type": "string", "enum": ["charging", "discharging", "full", "unknown"] },
                  "poll_rate_hz": { "type": "integer", "minimum": 0 }
                }
              }
            }
          }
        },
        "package_updates": {
          "type": "object",
          "required": ["updates_available", "security_updates_available"],
          "properties": {
            "updates_available": { "type": "integer", "minimum": 0 },
            "security_updates_available": { "type": "integer", "minimum": 0 }
          }
        },
        "flags": {
          "type": "object",
          "required": [
            "cpu_usage_supported", "cpu_temp_supported", "cpu_freq_supported", "cpu_power_supported", "cpu_temp_tmax_supported",
            "ram_supported", "gpu_supported", "gpu_usage_supported", "gpu_temp_supported",
            "gpu_vram_supported", "gpu_freq_supported", "gpu_power_supported",
            "gpu_vram_temp_supported", "gpu_vram_freq_supported", "gpu_temp_tmax_supported",
            "osd_supported", "peripherals_supported", "package_updates_supported"
          ],
          "properties": {
            "cpu_usage_supported": { "type": "boolean" },
            "cpu_temp_supported": { "type": "boolean" },
            "cpu_freq_supported": { "type": "boolean" },
            "cpu_power_supported": { "type": "boolean" },
            "cpu_temp_tmax_supported": { "type": "boolean" },
            "ram_supported": { "type": "boolean" },
            "gpu_supported": { "type": "boolean" },
            "gpu_usage_supported": { "type": "boolean" },
            "gpu_temp_supported": { "type": "boolean" },
            "gpu_vram_supported": { "type": "boolean" },
            "gpu_freq_supported": { "type": "boolean" },
            "gpu_power_supported": { "type": "boolean" },
            "gpu_vram_temp_supported": { "type": "boolean" },
            "gpu_vram_freq_supported": { "type": "boolean" },
            "gpu_temp_tmax_supported": { "type": "boolean" },
            "osd_supported": { "type": "boolean" },
            "peripherals_supported": { "type": "boolean" },
            "package_updates_supported": { "type": "boolean" }
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
              "identity": { "type": "string" },
              "desktop_entry": { "type": "string" },
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
                  "art_url": { "type": "string", "format": "uri", "description": "Remote HTTP/HTTPS URL or portable Base64-encoded Data URL (e.g. data:image/png;base64,...) for local media files" },
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
        "player_name": "firefox.instance_1_63",
        "identity": "Mozilla zen",
        "desktop_entry": "zen",
        "playback_status": "Playing",
        "volume": 0.85,
        "position_microseconds": 45120000,
        "metadata": {
          "track_id": "firefox:track:4PTG3Z6ehGkBFm5zOHYGaS",
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

### 3.5. Outbound Notification Event Payload (Host → Android Client)
This is an event-driven payload pushed asynchronously by the daemon whenever a desktop notification is intercepted/eavesdropped on the D-Bus session bus.

#### JSON Schema Spec
```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "title": "NotificationEventPush",
  "type": "object",
  "required": ["type", "timestamp", "data"],
  "properties": {
    "type": { "type": "string", "const": "notification_event" },
    "timestamp": { "type": "integer", "description": "Unix timestamp in seconds" },
    "data": {
      "type": "object",
      "required": ["id", "app_name", "replaces_id", "app_icon", "summary", "body", "actions", "hints", "expire_timeout"],
      "properties": {
        "id": { "type": "integer", "minimum": 0, "description": "Host-assigned unique notification ID" },
        "app_name": { "type": "string" },
        "replaces_id": { "type": "integer", "minimum": 0 },
        "app_icon": { "type": "string" },
        "app_icon_base64": { "type": "string", "description": "Optional Base64-encoded PNG or SVG image URI resolved from raw hints, file paths, or themes" },
        "summary": { "type": "string" },
        "body": { "type": "string" },
        "actions": { "type": "array", "items": { "type": "string" }, "description": "Interleaved action key/label pairs" },
        "hints": { "type": "object" },
        "expire_timeout": { "type": "integer" }
      }
    }
  }
}
```

#### JSON Payload Example (Standard Themed Lookup Fallback)
```json
{
  "type": "notification_event",
  "timestamp": 1716213825,
  "data": {
    "id": 1042,
    "app_name": "Slack",
    "replaces_id": 0,
    "app_icon": "slack",
    "app_icon_base64": "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAADAAAAAw...",
    "summary": "New message from Alice",
    "body": "Hey, are you free for a call?",
    "actions": ["default", "Activate", "dismiss", "Dismiss"],
    "hints": {
      "urgency": 1
    },
    "expire_timeout": 5000
  }
}
```

#### Application Icon Resolution Protocol (2-Tier Resolution)
To maximize visual fidelity on the companion app screen, the daemon resolves notification icons using a 2-tier architecture:

1. **Android Client-Side Cache (Tier 1)**:
   The Android app is pre-packaged with high-resolution vector and raster assets for the top 50 popular desktop applications (e.g. Slack, Discord, Spotify). If `app_icon` matches a known local asset identifier, the client renders the local high-fidelity resource directly to save bandwidth.

2. **Host Daemon Base64 Serialization (Tier 2 Fallback)**:
   If the icon is dynamic (e.g., chat avatar) or the application is not in the client-side library, the daemon attempts to populate `app_icon_base64` using the following tiered sequence:
   * **Step A: Capture raw image hints**: Check `image-data` (or deprecated `icon_data`) D-Bus notification hints containing signature `(iiibiiay)`. The daemon extracts the raw pixel buffer, compresses it into a high-performance PNG, Base64-encodes it, and prefixes with `data:image/png;base64,`.
   * **Step B: Resolve absolute file paths**: If `app_icon` or `image-path` contains a valid, readable local system path (e.g. `/tmp/avatar.png`), the daemon reads and Base64-encodes the image.
   * **Step C: Resolve themed icon names**: If `app_icon` is a themed name (e.g., `"gimp"`), the daemon scans standard XDG applications paths for `.desktop` matches, extracts the `Icon=` string, and searches standard icon directories (e.g., `/usr/share/icons/hicolor/`) for `.png` or `.svg` targets. SVGs are sent raw with the `data:image/svg+xml;base64,` prefix.

---

### 3.6. Inbound Notification Command Payload (Android Client → Host)
The companion app (or external WebSocket source) can transmit this command to trigger standard desktop notifications on the host system via D-Bus.

#### Request JSON Schema
```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "title": "NotificationCommand",
  "type": "object",
  "required": ["type", "summary", "body"],
  "properties": {
    "type": { "type": "string", "const": "notification_command" },
    "app_name": { "type": "string", "default": "pc-dashboard" },
    "replaces_id": { "type": "integer", "minimum": 0, "default": 0 },
    "app_icon": { "type": "string", "default": "dialog-information" },
    "summary": { "type": "string", "maxLength": 512 },
    "body": { "type": "string", "maxLength": 2048 },
    "actions": { "type": "array", "items": { "type": "string" }, "default": [] },
    "hints": { "type": "object", "default": {} },
    "expire_timeout": { "type": "integer", "default": -1 }
  }
}
```

#### JSON Payload Example
```json
{
  "type": "notification_command",
  "app_name": "pc-dashboard",
  "summary": "Companion Connected",
  "body": "Android companion app successfully established link.",
  "app_icon": "dialog-information",
  "expire_timeout": 3000
}
```

---

### 3.7. Inbound Notification Action Command Payload (Android Client → Host)
The companion app can transmit this command to trigger a specific action (button click) on a notification that was previously intercepted by the host.

#### Request JSON Schema
```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "title": "NotificationActionCommand",
  "type": "object",
  "required": ["type", "notification_id", "action_key"],
  "properties": {
    "type": { "type": "string", "const": "notification_action_command" },
    "notification_id": { "type": "integer", "minimum": 0, "description": "The unique system-assigned ID of the target notification" },
    "action_key": { "type": "string", "description": "The key of the action to trigger (e.g. 'default', 'dismiss')" }
  }
}
```

#### JSON Payload Example
```json
{
  "type": "notification_action_command",
  "notification_id": 1042,
  "action_key": "default"
}
```

---

### 3.8. Inbound Notification Dismiss Command Payload (Android Client → Host)
The companion app can transmit this command to explicitly close/dismiss a notification on the host system.

#### Request JSON Schema
```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "title": "NotificationDismissCommand",
  "type": "object",
  "required": ["type", "notification_id"],
  "properties": {
    "type": { "type": "string", "const": "notification_dismiss_command" },
    "notification_id": { "type": "integer", "minimum": 0, "description": "The unique system-assigned ID of the notification to close" }
  }
}
```

#### JSON Payload Example
```json
{
  "type": "notification_dismiss_command",
  "notification_id": 1042
}
```

---

### 3.9. Outbound Session Lock Payload (Host → Android Client)
This is an event-driven payload pushed asynchronously by the daemon whenever the host PC user session is locked or unlocked. Additionally, to establish immediate synchronization, this payload is sent to newly connected WebSocket clients immediately after a successful connection handshake, transmitting the current cached lock/unlock status of the host machine. This is used by the Android companion app to put the device screen into low-power sleeping mode after a configured timeout.


#### JSON Schema Spec
```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "title": "SessionLockPush",
  "type": "object",
  "required": ["type", "timestamp", "data"],
  "properties": {
    "type": { "type": "string", "const": "session_lock" },
    "timestamp": { "type": "integer", "description": "Unix timestamp in seconds" },
    "data": {
      "type": "object",
      "required": ["locked"],
      "properties": {
        "locked": { "type": "boolean", "description": "True if host user session is locked, false if unlocked" }
      }
    }
  }
}
```

#### JSON Payload Example
```json
{
  "type": "session_lock",
  "timestamp": 1716213825,
  "data": {
    "locked": true
  }
}
```

---

### 3.10. Outbound Power Profile State Payload (Host → Android Client)
This is an event-driven payload pushed asynchronously by the daemon whenever the active power profile changes on the host system, or when a new WebSocket client connects (relaying the cached status immediately).

#### JSON Schema Spec
```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "title": "PowerProfileStatePush",
  "type": "object",
  "required": ["type", "timestamp", "data"],
  "properties": {
    "type": { "type": "string", "const": "power_profile_state" },
    "timestamp": { "type": "integer", "description": "Unix timestamp in seconds" },
    "data": {
      "type": "object",
      "required": ["active_profile", "available_profiles"],
      "properties": {
        "active_profile": { "type": "string", "description": "The currently active power profile" },
        "available_profiles": {
          "type": "array",
          "items": {
            "type": "object",
            "required": ["profile"],
            "properties": {
              "profile": { "type": "string", "description": "The name of a supported power profile" }
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
  "type": "power_profile_state",
  "timestamp": 1716213825,
  "data": {
    "active_profile": "balanced",
    "available_profiles": [
      { "profile": "power-saver" },
      { "profile": "balanced" },
      { "profile": "performance" }
    ]
  }
}
```

---

### 3.11. Inbound Power Profile Command Payload (Android Client → Host)
The companion app can transmit this command over the WebSocket connection to request switching the active system power profile on the host PC.

#### Request JSON Schema
```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "title": "PowerProfileCommand",
  "type": "object",
  "required": ["type", "profile"],
  "properties": {
    "type": { "type": "string", "const": "power_profile_command" },
    "profile": { "type": "string", "description": "The name of the power profile to activate" }
  }
}
```

#### JSON Payload Example
```json
{
  "type": "power_profile_command",
  "profile": "power-saver"
}
```

---

### 3.12. Outbound Bluetooth State Payload (Host → Android Client)
This is an event-driven payload pushed asynchronously by the daemon whenever:
* A Bluetooth device connects or disconnects from the host system.
* A connected device's battery level or RSSI changes (periodic poll, default every 30 seconds).
* A new WebSocket client connects (cached state snapshot push).

#### JSON Schema Spec
```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "title": "BluetoothStatePush",
  "type": "object",
  "required": ["type", "timestamp", "data"],
  "properties": {
    "type": { "type": "string", "const": "bluetooth_state" },
    "timestamp": { "type": "integer", "description": "Unix timestamp in seconds" },
    "data": {
      "type": "object",
      "required": ["event_type", "device", "connected_devices"],
      "properties": {
        "event_type": {
          "type": "string",
          "enum": ["connected", "disconnected", "updated", "snapshot"],
          "description": "connected: device just connected; disconnected: device just disconnected; updated: battery/RSSI changed; snapshot: full initial sync on client connect"
        },
        "device": {
          "type": "object",
          "description": "The device that triggered this event (may be empty for snapshot events)",
          "properties": {
            "address": { "type": "string", "description": "Bluetooth MAC address (e.g. AA:BB:CC:DD:EE:FF)" },
            "name": { "type": "string", "description": "BlueZ Device1.Name property" },
            "alias": { "type": "string", "description": "User-assigned alias (may equal name)" },
            "class": { "type": "integer", "minimum": 0, "description": "Bluetooth device class bitmask" },
            "battery_percent": { "type": ["integer", "null"], "minimum": 0, "maximum": 100, "description": "Battery level percentage; null if Battery Service (BAS) not supported" },
            "rssi": { "type": ["integer", "null"], "description": "Signal strength in dBm; null if not available" },
            "connected": { "type": "boolean" },
            "paired": { "type": "boolean" },
            "trusted": { "type": "boolean" }
          }
        },
        "connected_devices": {
          "type": "array",
          "description": "Full list of currently connected Bluetooth devices after applying this event",
          "items": {
            "type": "object",
            "required": ["address", "name", "alias", "class", "connected", "paired", "trusted"],
            "properties": {
              "address": { "type": "string" },
              "name": { "type": "string" },
              "alias": { "type": "string" },
              "class": { "type": "integer", "minimum": 0 },
              "battery_percent": { "type": ["integer", "null"], "minimum": 0, "maximum": 100 },
              "rssi": { "type": ["integer", "null"] },
              "connected": { "type": "boolean" },
              "paired": { "type": "boolean" },
              "trusted": { "type": "boolean" }
            }
          }
        }
      }
    }
  }
}
```

#### JSON Payload Examples

* **Device Connected Event**:
```json
{
  "type": "bluetooth_state",
  "timestamp": 1716213825,
  "data": {
    "event_type": "connected",
    "device": {
      "address": "AA:BB:CC:11:22:33",
      "name": "Wireless Headphones",
      "alias": "My Headphones",
      "class": 2360328,
      "battery_percent": 87,
      "rssi": -58,
      "connected": true,
      "paired": true,
      "trusted": true
    },
    "connected_devices": [
      {
        "address": "AA:BB:CC:11:22:33",
        "name": "Wireless Headphones",
        "alias": "My Headphones",
        "class": 2360328,
        "battery_percent": 87,
        "rssi": -58,
        "connected": true,
        "paired": true,
        "trusted": true
      }
    ]
  }
}
```

* **Device Disconnected Event**:
```json
{
  "type": "bluetooth_state",
  "timestamp": 1716213860,
  "data": {
    "event_type": "disconnected",
    "device": {
      "address": "AA:BB:CC:11:22:33",
      "name": "Wireless Headphones",
      "alias": "My Headphones",
      "class": 2360328,
      "battery_percent": null,
      "rssi": null,
      "connected": false,
      "paired": true,
      "trusted": true
    },
    "connected_devices": []
  }
}
```

* **Battery/RSSI Update Event**:
```json
{
  "type": "bluetooth_state",
  "timestamp": 1716213890,
  "data": {
    "event_type": "updated",
    "device": {
      "address": "AA:BB:CC:11:22:33",
      "name": "Wireless Headphones",
      "alias": "My Headphones",
      "class": 2360328,
      "battery_percent": 86,
      "rssi": -62,
      "connected": true,
      "paired": true,
      "trusted": true
    },
    "connected_devices": [
      {
        "address": "AA:BB:CC:11:22:33",
        "name": "Wireless Headphones",
        "alias": "My Headphones",
        "class": 2360328,
        "battery_percent": 86,
        "rssi": -62,
        "connected": true,
        "paired": true,
        "trusted": true
      }
    ]
  }
}
```

### 3.13. Outbound OSD Event Payload (Host → Android Client)
This is an event-driven payload pushed asynchronously by the daemon whenever:
* A system master volume change or mute status change is detected.
* A keyboard Lock key indicator (Caps Lock, Num Lock, Scroll Lock) state changes.

#### JSON Schema Spec
```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "title": "OSDEventPush",
  "type": "object",
  "required": ["type", "timestamp", "data"],
  "properties": {
    "type": { "type": "string", "const": "osd_event" },
    "timestamp": { "type": "integer", "description": "Unix timestamp in seconds" },
    "data": {
      "type": "object",
      "required": ["event_type"],
      "properties": {
        "event_type": { "type": "string", "enum": ["volume", "mute", "capslock", "numlock", "scrolllock"] },
        "volume_percent": { "type": "integer", "minimum": 0, "maximum": 100, "description": "System master volume level; only present when event_type is volume or mute" },
        "muted": { "type": "boolean", "description": "System master mute state; only present when event_type is volume or mute" },
        "locked": { "type": "boolean", "description": "Keyboard lock key state; only present when event_type is capslock, numlock, or scrolllock" }
      }
    }
  }
}
```

#### JSON Payload Examples

* **Volume Changed Event**:
```json
{
  "type": "osd_event",
  "timestamp": 1716213825,
  "data": {
    "event_type": "volume",
    "volume_percent": 65,
    "muted": false
  }
}
```

* **Caps Lock Toggled Event**:
```json
{
  "type": "osd_event",
  "timestamp": 1716213830,
  "data": {
    "event_type": "capslock",
    "locked": true
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

---

## 5. Local Command UDS Socket Protocol Specification

To support local manual triggering of client-bound state updates, the daemon exposes a Unix Domain Socket (UDS) interface. Command clients connect, transmit trigger packets, and block awaiting process execution response before closing the channel.

### 5.1. Socket Connection Parameters
*   **Protocol**: Unix Domain Socket (`unix`)
*   **Path**: `$XDG_RUNTIME_DIR/pc-dashboard-server.sock` (Default) or configuration-overridden path.
*   **Timeout**: 3 seconds dial / write timeout.

---

### 5.2. Inbound UDS Trigger Request (`UDSRequest`)
Command clients transmit a single JSON object frame upon successful socket dial.

#### JSON Schema
```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "title": "UDSRequest",
  "type": "object",
  "required": ["type", "data"],
  "properties": {
    "type": { 
      "type": "string", 
      "enum": ["session_lock", "notification_event", "media_state", "telemetry", "power_profile_state", "bluetooth_state", "osd_event", "raw"] 
    },
    "data": { 
      "type": "object",
      "description": "Inner event data matching corresponding WebSocket outbound payload interfaces"
    }
  }
}
```

#### Supported Types & Payload Formats

##### A. `session_lock` (Lock/Unlock Event)
Triggers session lock/unlock updates.
*   **Request Data Schema**:
    ```json
    {
      "locked": true
    }
    ```
*   **Daemon Action**: Wraps under `SessionLockPayload` (attaching current server timestamp) and broadcasts to all WebSocket clients.

##### B. `notification_event` (Intercepted Notification Event)
Triggers mock notifications on the companion app.
*   **Request Data Schema**:
    ```json
    {
      "id": 1042,
      "app_name": "Slack",
      "replaces_id": 0,
      "app_icon": "slack",
      "summary": "Title of message",
      "body": "Body of notification message",
      "actions": ["default", "Open", "dismiss", "Dismiss"],
      "hints": {},
      "expire_timeout": 5000
    }
    ```
*   **Daemon Action**: Wraps under `NotificationEventPayload` (attaching timestamp) and broadcasts.

##### C. `media_state` (MPRIS State Event)
Triggers custom media states (rotating track information, playback state toggles, progress updates).
*   **Request Data Schema**: See MPRIS PlayerState schema defined in `3.3 Outbound Media State Payload`.
*   **Daemon Action**: Wraps under `MediaStatePayload` and broadcasts.

##### D. `telemetry` (System Telemetry Event)
Triggers CPU/RAM/GPU telemetry metrics update.
*   **Request Data Schema**: See SystemMetrics schema defined in `3.1 Outbound Telemetry Payload`.
*   **Daemon Action**: Wraps under `TelemetryPayload` and broadcasts.

##### E. `power_profile_state` (Power Profile Event)
Triggers power profile state update.
*   **Request Data Schema**: See PowerProfileState schema defined in `3.10 Outbound Power Profile State Payload`.
*   **Daemon Action**: Wraps under `PowerProfileStatePayload` (attaching current server timestamp) and broadcasts.

##### F. `osd_event` (On-Screen Display Event)
Triggers volume adjustments and lock status updates.
*   **Request Data Schema**: See OSDEventPush schema defined in `3.13. Outbound OSD Event Payload`.
*   **Daemon Action**: Wraps under `OSDEventPayload` (attaching current server timestamp) and broadcasts.

##### G. `raw` (Arbitrary Passthrough Payload)
Directly relays custom JSON types to WebSockets without daemon verification.
*   **Request Format**:
    ```json
    {
      "type": "raw",
      "data": {
        "type": "my_custom_client_payload",
        "custom_key": "custom_value"
      }
    }
    ```
*   **Daemon Action**: Directly serializes and broadcasts `data` as a raw WebSocket UTF-8 text frame.

---

### 5.3. Outbound UDS Processing Response (`UDSResponse`)
The daemon processes the trigger request, attempts WebSocket distribution, and returns a confirmation JSON frame back before unlinking the connection.

#### JSON Schema
```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "title": "UDSResponse",
  "type": "object",
  "required": ["success", "client_count"],
  "properties": {
    "success": { "type": "boolean" },
    "client_count": { "type": "integer", "minimum": 0, "description": "Number of active clients that received the event" },
    "error": { "type": "string", "description": "Error details if success is false" }
  }
}
```

#### JSON Payload Examples

*   **Success Response**:
    ```json
    {
      "success": true,
      "client_count": 1
    }
    ```

*   **Failure (Invalid Schema)**:
    ```json
    {
      "success": false,
      "client_count": 0,
      "error": "failed to decode inner trigger data: json: cannot unmarshal string into Go struct"
    }
    ```

