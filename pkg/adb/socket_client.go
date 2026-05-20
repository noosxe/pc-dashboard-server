package adb

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"time"
)

// SocketADBClient connects directly to the ADB server via TCP loopback.
type SocketADBClient struct {
	logger     *slog.Logger
	serverHost string
	serverPort int
}

// NewSocketADBClient instantiates a production ADBClient.
func NewSocketADBClient(host string, port int, logger *slog.Logger) *SocketADBClient {
	if host == "" {
		host = "127.0.0.1"
	}
	if port == 0 {
		port = 5037
	}
	return &SocketADBClient{
		logger:     logger,
		serverHost: host,
		serverPort: port,
	}
}

// TrackDevices monitors devices online/offline state.
func (c *SocketADBClient) TrackDevices(ctx context.Context) (<-chan DeviceEvent, error) {
	out := make(chan DeviceEvent, 10)

	// Establish connection
	addr := net.JoinHostPort(c.serverHost, strconv.Itoa(c.serverPort))
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		close(out)
		return nil, fmt.Errorf("failed to connect to ADB server at %s: %w", addr, err)
	}

	// Close connection when context is cancelled to unblock read loop
	go func() {
		<-ctx.Done()
		conn.Close()
	}()

	// Set initial write deadline for handshakes
	if err := conn.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
		conn.Close()
		close(out)
		return nil, fmt.Errorf("failed to set handshake deadline: %w", err)
	}

	// Send track-devices command
	if err := writeFrame(conn, "host:track-devices"); err != nil {
		conn.Close()
		close(out)
		return nil, fmt.Errorf("failed to send track-devices: %w", err)
	}

	if err := checkOkay(conn); err != nil {
		conn.Close()
		close(out)
		return nil, fmt.Errorf("track-devices rejected by ADB server: %w", err)
	}

	// Clear deadlines for continuous streaming since it will wait indefinitely for hotplugs,
	// but remains fully cancelable due to the background closer goroutine.
	if err := conn.SetDeadline(time.Time{}); err != nil {
		conn.Close()
		close(out)
		return nil, fmt.Errorf("failed to clear tracking deadlines: %w", err)
	}

	// Run device tracking loop in a goroutine
	go func() {
		defer conn.Close()
		defer close(out)

		activeDevices := make(map[string]bool)

		// Loop reading events from the ADB server
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			// Read length (4-hex string)
			lenBuf := make([]byte, 4)
			_, err := io.ReadFull(conn, lenBuf)
			if err != nil {
				// Suppress logging if error is due to context cancellation or normal connection shutdown
				if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) || ctx.Err() != nil {
					return
				}
				c.logger.Error("Error reading event length", "error", err)
				return
			}

			length, err := parseHexLength(lenBuf)
			if err != nil {
				c.logger.Error("Invalid event length hex", "hex", string(lenBuf), "error", err)
				return
			}

			// Read payload
			payloadBuf := make([]byte, length)
			_, err = io.ReadFull(conn, payloadBuf)
			if err != nil {
				c.logger.Error("Error reading event payload", "length", length, "error", err)
				return
			}

			payload := string(payloadBuf)
			currentDevices := make(map[string]bool)

			lines := strings.Split(payload, "\n")
			for _, line := range lines {
				line = strings.TrimSpace(line)
				if line == "" {
					continue
				}

				parts := strings.Fields(line)
				if len(parts) < 2 {
					continue
				}

				serial := parts[0]
				state := parts[1]

				// ADB state "device" is what maps to StateOnline.
				if state == "device" {
					currentDevices[serial] = true
					if !activeDevices[serial] {
						activeDevices[serial] = true
						out <- DeviceEvent{Serial: serial, State: StateOnline}
					}
				} else {
					if activeDevices[serial] {
						delete(activeDevices, serial)
						out <- DeviceEvent{Serial: serial, State: StateOffline}
					}
				}
			}

			// Check for devices that went offline because they are missing from list
			for serial := range activeDevices {
				if !currentDevices[serial] {
					delete(activeDevices, serial)
					out <- DeviceEvent{Serial: serial, State: StateOffline}
				}
			}
		}
	}()

	return out, nil
}

// ReversePort requests ADB reverse port forwarding.
func (c *SocketADBClient) ReversePort(ctx context.Context, serial string, localPort, devicePort int) error {
	conn, err := c.connectToDevice(ctx, serial)
	if err != nil {
		return err
	}
	defer conn.Close()

	cmd := fmt.Sprintf("reverse:forward:tcp:%d;tcp:%d", localPort, devicePort)
	if err := writeFrame(conn, cmd); err != nil {
		return fmt.Errorf("failed to send reverse forward command: %w", err)
	}

	if err := checkOkay(conn); err != nil {
		return fmt.Errorf("reverse forward rejected: %w", err)
	}

	return nil
}

// LaunchApp launches companion application activity via raw shell command.
func (c *SocketADBClient) LaunchApp(ctx context.Context, serial string, pkg, activity string) error {
	conn, err := c.connectToDevice(ctx, serial)
	if err != nil {
		return err
	}
	defer conn.Close()

	cmd := fmt.Sprintf("shell:am start -n %s/%s", pkg, activity)
	if err := writeFrame(conn, cmd); err != nil {
		return fmt.Errorf("failed to send launch command: %w", err)
	}

	if err := checkOkay(conn); err != nil {
		return fmt.Errorf("launch app command rejected: %w", err)
	}

	// Read response until EOF to ensure the command executes to completion.
	_, _ = io.Copy(io.Discard, conn)
	return nil
}

// WakeDevice sends screen wakeup keyevent.
func (c *SocketADBClient) WakeDevice(ctx context.Context, serial string) error {
	conn, err := c.connectToDevice(ctx, serial)
	if err != nil {
		return err
	}
	defer conn.Close()

	cmd := "shell:input keyevent KEYCODE_WAKEUP"
	if err := writeFrame(conn, cmd); err != nil {
		return fmt.Errorf("failed to send wakeup command: %w", err)
	}

	if err := checkOkay(conn); err != nil {
		return fmt.Errorf("wakeup command rejected: %w", err)
	}

	// Read response until EOF to ensure the command executes to completion.
	_, _ = io.Copy(io.Discard, conn)
	return nil
}

// connectToDevice connects to the ADB server and sets up transport to device serial.
func (c *SocketADBClient) connectToDevice(ctx context.Context, serial string) (net.Conn, error) {
	addr := net.JoinHostPort(c.serverHost, strconv.Itoa(c.serverPort))
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("failed to dial ADB server at %s: %w", addr, err)
	}

	// Apply context deadline or a default 10-second timeout to protect against hangs
	var deadlineErr error
	if deadline, ok := ctx.Deadline(); ok {
		deadlineErr = conn.SetDeadline(deadline)
	} else {
		deadlineErr = conn.SetDeadline(time.Now().Add(10 * time.Second))
	}
	if deadlineErr != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to set connection deadline: %w", deadlineErr)
	}

	transportCmd := fmt.Sprintf("host:transport:%s", serial)
	if err := writeFrame(conn, transportCmd); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to send transport cmd: %w", err)
	}

	if err := checkOkay(conn); err != nil {
		conn.Close()
		return nil, fmt.Errorf("transport rejected for serial %s: %w", serial, err)
	}

	return conn, nil
}

// writeFrame writes a string payload with a 4-hex prefix.
func writeFrame(w io.Writer, payload string) error {
	length := len(payload)
	header := fmt.Sprintf("%04x", length)
	_, err := fmt.Fprintf(w, "%s%s", header, payload)
	return err
}

// checkOkay reads and verifies ADB OKAY handshake.
func checkOkay(r io.Reader) error {
	status := make([]byte, 4)
	if _, err := io.ReadFull(r, status); err != nil {
		return fmt.Errorf("failed to read status response: %w", err)
	}

	if string(status) == "OKAY" {
		return nil
	}

	if string(status) == "FAIL" {
		// Read 4-hex error length
		lenBuf := make([]byte, 4)
		if _, err := io.ReadFull(r, lenBuf); err != nil {
			return fmt.Errorf("failed to read error payload length after FAIL: %w", err)
		}
		length, err := parseHexLength(lenBuf)
		if err != nil {
			return fmt.Errorf("invalid error length hex after FAIL: %w", err)
		}

		errBuf := make([]byte, length)
		if _, err := io.ReadFull(r, errBuf); err != nil {
			return fmt.Errorf("failed to read error message after FAIL: %w", err)
		}
		return errors.New(string(errBuf))
	}

	return fmt.Errorf("unexpected ADB response: %s", string(status))
}

// parseHexLength parses 4-hex UTF-8 characters to integer.
func parseHexLength(buf []byte) (int, error) {
	bytesVal, err := hex.DecodeString(string(buf))
	if err != nil {
		return 0, err
	}
	if len(bytesVal) < 2 {
		return 0, fmt.Errorf("insufficient hex bytes decoded")
	}
	return int(bytesVal[0])<<8 | int(bytesVal[1]), nil
}
