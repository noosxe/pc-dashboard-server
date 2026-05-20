package adb

import "context"

// DeviceState represents the status of the Android USB connection.
type DeviceState string

const (
	StateOnline  DeviceState = "online"
	StateOffline DeviceState = "offline"
)

// DeviceEvent details hotplug device connection changes.
type DeviceEvent struct {
	Serial string
	State  DeviceState
}

// ADBClient defines standard communications to query/bootstrap Android devices.
type ADBClient interface {
	// TrackDevices streams hotplug device connection changes.
	TrackDevices(ctx context.Context) (<-chan DeviceEvent, error)

	// ReversePort maps local TCP port on mobile back to a host interface.
	ReversePort(ctx context.Context, serial string, localPort, devicePort int) error

	// LaunchApp starts target companion application activity on the client.
	LaunchApp(ctx context.Context, serial string, pkg, activity string) error

	// WakeDevice wakes screen on client if in sleep.
	WakeDevice(ctx context.Context, serial string) error
}
