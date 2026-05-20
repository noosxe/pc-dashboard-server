package adb

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strings"
	"testing"
	"time"
)

// readTestFrame helper reads a length-prefixed frame from net.Conn
func readTestFrame(r io.Reader) (string, error) {
	lenBuf := make([]byte, 4)
	if _, err := io.ReadFull(r, lenBuf); err != nil {
		return "", err
	}
	length, err := parseHexLength(lenBuf)
	if err != nil {
		return "", err
	}
	payloadBuf := make([]byte, length)
	if _, err := io.ReadFull(r, payloadBuf); err != nil {
		return "", err
	}
	return string(payloadBuf), nil
}

// TestSocketADBClient_TrackDevices verifies connection discovery and hex parsing.
func TestSocketADBClient_TrackDevices(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start test TCP listener: %v", err)
	}
	defer listener.Close()

	addr := listener.Addr().String()
	parts := strings.Split(addr, ":")
	host := parts[0]
	portStr := parts[1]
	var port int
	fmt.Sscanf(portStr, "%d", &port)

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		// Read track-devices request
		cmd, err := readTestFrame(conn)
		if err != nil || cmd != "host:track-devices" {
			return
		}

		// Write OKAY handshake
		if _, err := conn.Write([]byte("OKAY")); err != nil {
			return
		}

		// Write length-prefixed tracked device info
		// "MOCK_DEVICE_12345\tdevice\n" has 25 chars -> Hex "0019"
		if _, err := conn.Write([]byte("0019MOCK_DEVICE_12345\tdevice\n")); err != nil {
			return
		}

		// Keep connection open briefly
		time.Sleep(100 * time.Millisecond)
	}()

	client := NewSocketADBClient(host, port, slog.Default())
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	events, err := client.TrackDevices(ctx)
	if err != nil {
		t.Fatalf("TrackDevices returned error: %v", err)
	}

	select {
	case ev, ok := <-events:
		if !ok {
			t.Fatal("events channel closed prematurely")
		}
		if ev.Serial != "MOCK_DEVICE_12345" {
			t.Errorf("Expected Serial 'MOCK_DEVICE_12345', got '%s'", ev.Serial)
		}
		if ev.State != StateOnline {
			t.Errorf("Expected State '%s', got '%s'", StateOnline, ev.State)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for device online event")
	}
}

// TestSocketADBClient_BootstrapActions verifies wake, app launch, and reverse port calls.
func TestSocketADBClient_BootstrapActions(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start test TCP listener: %v", err)
	}
	defer listener.Close()

	addr := listener.Addr().String()
	parts := strings.Split(addr, ":")
	host := parts[0]
	portStr := parts[1]
	var port int
	fmt.Sscanf(portStr, "%d", &port)

	// Mock server that processes a full bootstrap handshake chain
	go func() {
		for i := 0; i < 3; i++ {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			func() {
				defer conn.Close()

				// Read select-device command ("host:transport:TEST_123")
				cmd, err := readTestFrame(conn)
				if err != nil || !strings.HasPrefix(cmd, "host:transport:") {
					return
				}
				if _, err := conn.Write([]byte("OKAY")); err != nil {
					return
				}

				// Read subsequent bootstrap action
				_, err = readTestFrame(conn)
				if err != nil {
					return
				}
				// Accept commands unconditionally and return OKAY
				if _, err := conn.Write([]byte("OKAY")); err != nil {
					return
				}
			}()
		}
	}()

	client := NewSocketADBClient(host, port, slog.Default())
	ctx := context.Background()

	// 1. Test WakeDevice
	if err := client.WakeDevice(ctx, "TEST_123"); err != nil {
		t.Errorf("WakeDevice failed: %v", err)
	}

	// 2. Test LaunchApp
	if err := client.LaunchApp(ctx, "TEST_123", "com.noosxe.pc_dashboard", "MainActivity"); err != nil {
		t.Errorf("LaunchApp failed: %v", err)
	}

	// 3. Test ReversePort
	if err := client.ReversePort(ctx, "TEST_123", 12345, 12345); err != nil {
		t.Errorf("ReversePort failed: %v", err)
	}
}
