# ConnectRPC Protocol & Service Specification

This document provides the technical specifications, architectural details, and endpoint definitions for the **ConnectRPC** communication route. ConnectRPC runs as a plaintext, high-performance alternative to the JSON WebSocket channel on the **PC Dashboard Server**.

---

## 1. Architectural Overview

```
+-------------------------------------------------------------+
|             Application Layer: Protocol Buffers (v3)        |
|  (Type-safe, reflection-free, compiled Go & Kotlin structs) |
|   (Raw binary bytes for image artwork and system icons)     |
+-------------------------------------------------------------+
|             Transport Layer: ConnectRPC (HTTP/2 / H2C)      |
|           Plaintext cleartext multiplexed streaming         |
+-------------------------------------------------------------+
|             Tunnelling Layer: ADB Reverse Forward           |
|               Tunnels USB Port 12345 to 12345               |
+-------------------------------------------------------------+
|               Link Layer: Physical USB Connection           |
+-------------------------------------------------------------+
```

### 1.1. Cleartext HTTP/2 (H2C) Transport
Since the communication happens entirely within the local machine loopback (`127.0.0.1`) over an ADB reverse TCP tunnel, TLS encryption is not used. 
To support streaming over HTTP/2 without TLS, the server implements **H2C (HTTP/2 Cleartext)** wrapping using Go's `golang.org/x/net/http2/h2c`. 

### 1.2. Shared Port Multiplexing
The ConnectRPC handlers share the exact same TCP port (`12345`) and listener as the WebSocket server. ConnectRPC requests are routed to `/pcd.v1.*` endpoints on the main `http.ServeMux` alongside the `/ws` path, which avoids any modifications to the ADB port forwarding configuration.

---

## 2. Service Definitions & Endpoints

All services reside in the `pcd.v1` package, compiled using Buf into Go and Kotlin models.

### 2.1. Telemetry Service
Handles periodic streaming of system hardware performance metrics.

*   **Endpoint**: `/pcd.v1.TelemetryService/StreamTelemetry`
*   **Request Type**: `StreamTelemetryRequest` (specifies desired interval rate)
*   **Response Type**: `stream StreamTelemetryResponse` (pushed at requested rate)

#### Schema Definitions
```protobuf
message TelemetryPayload {
  int64 timestamp = 1;
  CPUUsage cpu = 2;
  GPUUsage gpu = 3;
  RAMUsage ram = 4;
  SwapUsage swap = 5;
  ZRAMUsage zram = 6;
  TelemetryFlags flags = 7;
}

message StreamTelemetryResponse {
  TelemetryPayload payload = 1;
}
```

---

### 2.2. Notification Service
Handles streaming intercepted desktop notifications, triggering custom notification actions, and sending new notifications from the client.

*   **RPCs**:
    *   `StreamNotifications(StreamNotificationsRequest) returns (stream StreamNotificationsResponse)`
    *   `SendNotification(SendNotificationRequest) returns (SendNotificationResponse)`
    *   `TriggerNotificationAction(TriggerNotificationActionRequest) returns (TriggerNotificationActionResponse)`
    *   `DismissNotification(DismissNotificationRequest) returns (DismissNotificationResponse)`

#### Image Binary Optimization
Unlike the WebSocket JSON payload which encodes notification avatars into base64, the ConnectRPC notification payload utilizes a raw `bytes app_icon_raw = 5` field. This eliminates Base64 processing overhead and reduces payload size by ~33%.

---

### 2.3. Media Service
Subscribes to active MPRIS player states (identity, track metadata, progress) and allows dispatching MPRIS playback commands.

*   **RPCs**:
    *   `StreamMediaState(StreamMediaStateRequest) returns (stream StreamMediaStateResponse)`
    *   `SendMediaCommand(SendMediaCommandRequest) returns (SendMediaCommandResponse)`

#### Schema Definitions
```protobuf
message MediaTrackMetadata {
  string track_id = 1;
  string title = 2;
  repeated string artist = 3;
  string album = 4;
  string art_url = 5;
  bytes art_raw = 6; // Raw image byte payload (no base64)
  int64 length_microseconds = 7;
}
```

---

### 2.4. System Service
Provides endpoints to monitor system states (session lock transitions, power profile switches, DPMS display status) and dispatch system adjustments.

*   **RPCs**:
    *   `StreamSystemState(StreamSystemStateRequest) returns (stream StreamSystemStateResponse)`
    *   `ExecuteSystemAction(ExecuteSystemActionRequest) returns (ExecuteSystemActionResponse)`
    *   `SetPowerProfile(SetPowerProfileRequest) returns (SetPowerProfileResponse)`

---

### 2.5. Bluetooth Service
Monitors connected wireless peripherals and battery states.

*   **RPCs**:
    *   `StreamBluetoothState(StreamBluetoothStateRequest) returns (stream StreamBluetoothStateResponse)`

---

## 3. Security & Boundary Isolation

1.  **Strict Loopback Binding**: The HTTP server wrapping the ConnectRPC endpoints binds strictly to `127.0.0.1` or `::1`. 
2.  **No Public Exposure**: Because H2C does not use TLS, the server must never bind to public interfaces. Any request arriving from non-loopback addresses must be rejected immediately by the listener.
3.  **Command Validation**: System commands (like `suspend`) and power profile options will run through the exact same sanitization boundaries and predefined whitelists as the WebSocket routes.
