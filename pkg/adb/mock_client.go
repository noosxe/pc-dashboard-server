package adb

import (
	"context"
	"log"
	"time"
)

// MockADBClient simulates hotplug USB and companion bootstrapping actions.
type MockADBClient struct {
	serial string
}

// NewMockADBClient instantiates a mock client with a default serial.
func NewMockADBClient() *MockADBClient {
	return &MockADBClient{
		serial: "MOCK_DEVICE_12345",
	}
}

// TrackDevices simulates device connection event after 3 seconds.
func (c *MockADBClient) TrackDevices(ctx context.Context) (<-chan DeviceEvent, error) {
	out := make(chan DeviceEvent, 5)

	go func() {
		defer close(out)

		// 1. Wait for 3 seconds to simulate booting delays
		select {
		case <-ctx.Done():
			return
		case <-time.After(3 * time.Second):
		}

		// 2. Dispatch simulated online event
		log.Printf("[MockADB] Device %s online", c.serial)
		out <- DeviceEvent{Serial: c.serial, State: StateOnline}

		// 3. Keep running until context is cancelled
		<-ctx.Done()

		// 4. Log teardown
		log.Printf("[MockADB] Device %s offline. Cleared reverse port tunnels.", c.serial)
	}()

	return out, nil
}

// ReversePort mocks reverse port configuration.
func (c *MockADBClient) ReversePort(ctx context.Context, serial string, localPort, devicePort int) error {
	log.Printf("[MockADB] Reversing device port %d to host %d for serial %s", devicePort, localPort, serial)
	return nil
}

// LaunchApp mocks starting target Android companion app activity.
func (c *MockADBClient) LaunchApp(ctx context.Context, serial string, pkg, activity string) error {
	log.Printf("[MockADB] Launching activity %s/%s for serial %s", pkg, activity, serial)
	return nil
}

// WakeDevice mocks screen wakeup KEYCODE_WAKEUP event.
func (c *MockADBClient) WakeDevice(ctx context.Context, serial string) error {
	log.Printf("[MockADB] Waking screen for serial %s", serial)
	return nil
}
