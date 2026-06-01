package cmd

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/noosxe/pc-dashboard-server/pkg/daemon"
	"github.com/noosxe/pc-dashboard-server/pkg/dpms"
	"github.com/noosxe/pc-dashboard-server/pkg/lock"
	"github.com/noosxe/pc-dashboard-server/pkg/metrics"
	"github.com/noosxe/pc-dashboard-server/pkg/mpris"
	"github.com/noosxe/pc-dashboard-server/pkg/notifications"
	"github.com/noosxe/pc-dashboard-server/pkg/power"
	"github.com/spf13/cobra"
)

var triggerSocketPath string

// TriggerCmd represents the base trigger command
var TriggerCmd = &cobra.Command{
	Use:   "trigger",
	Short: "Trigger mock system events and telemetry updates to WebSocket clients",
	Long:  `Sends real-time event notifications to the active daemon via a Unix Domain Socket, which broadcasts them to connected companion devices.`,
}

// LockCmd triggers a session lock event (locked=true)
var LockCmd = &cobra.Command{
	Use:   "lock",
	Short: "Trigger a host session lock event",
	RunE: func(cmd *cobra.Command, args []string) error {
		data, err := json.Marshal(lock.SessionLockEvent{Locked: true})
		if err != nil {
			return err
		}
		return sendUDSRequest(triggerSocketPath, daemon.UDSRequest{Type: "session_lock", Data: data})
	},
}

// UnlockCmd triggers a session unlock event (locked=false)
var UnlockCmd = &cobra.Command{
	Use:   "unlock",
	Short: "Trigger a host session unlock event",
	RunE: func(cmd *cobra.Command, args []string) error {
		data, err := json.Marshal(lock.SessionLockEvent{Locked: false})
		if err != nil {
			return err
		}
		return sendUDSRequest(triggerSocketPath, daemon.UDSRequest{Type: "session_lock", Data: data})
	},
}

// DpmsState variable
var dpmsState string

// DpmsCmd triggers a display power (DPMS) state event
var DpmsCmd = &cobra.Command{
	Use:   "dpms",
	Short: "Trigger a mock DPMS display power event",
	RunE: func(cmd *cobra.Command, args []string) error {
		state := strings.ToLower(dpmsState)
		if state != "on" && state != "off" {
			return fmt.Errorf("flag --state must be either 'on' or 'off'")
		}
		data, err := json.Marshal(dpms.DpmsEvent{State: state})
		if err != nil {
			return err
		}
		return sendUDSRequest(triggerSocketPath, daemon.UDSRequest{Type: "dpms", Data: data})
	},
}

// Notification variables
var (
	notifAppName       string
	notifReplacesID    uint32
	notifAppIcon       string
	notifSummary       string
	notifBody          string
	notifActions       string
	notifExpireTimeout int32
)

// NotificationCmd triggers a desktop notification event
var NotificationCmd = &cobra.Command{
	Use:   "notification",
	Short: "Trigger a mock desktop notification event",
	RunE: func(cmd *cobra.Command, args []string) error {
		var actions []string
		if notifActions != "" {
			actions = strings.Split(notifActions, ",")
			for i := range actions {
				actions[i] = strings.TrimSpace(actions[i])
			}
		}

		ev := notifications.NotificationEvent{
			AppName:       notifAppName,
			ReplacesID:    notifReplacesID,
			AppIcon:       notifAppIcon,
			Summary:       notifSummary,
			Body:          notifBody,
			Actions:       actions,
			Hints:         make(map[string]interface{}),
			ExpireTimeout: notifExpireTimeout,
		}

		data, err := json.Marshal(ev)
		if err != nil {
			return err
		}
		return sendUDSRequest(triggerSocketPath, daemon.UDSRequest{Type: "notification_event", Data: data})
	},
}

// Media state variables
var (
	mediaPlayerName    string
	mediaStatus        string
	mediaVolume        float64
	mediaPositionMicro int64
	mediaTrackID       string
	mediaTitle         string
	mediaArtist        string
	mediaAlbum         string
	mediaArtURL        string
	mediaLengthMicro   int64
)

// MediaCmd triggers an MPRIS media state event
var MediaCmd = &cobra.Command{
	Use:   "media",
	Short: "Trigger a mock media state event",
	RunE: func(cmd *cobra.Command, args []string) error {
		var artists []string
		if mediaArtist != "" {
			artists = strings.Split(mediaArtist, ",")
			for i := range artists {
				artists[i] = strings.TrimSpace(artists[i])
			}
		}

		ev := mpris.MediaEvent{
			ActivePlayers: []mpris.PlayerState{
				{
					PlayerName:     mediaPlayerName,
					PlaybackStatus: mpris.PlaybackStatus(mediaStatus),
					Volume:         mediaVolume,
					PositionMicro:  mediaPositionMicro,
					Metadata: mpris.PlayerMetadata{
						TrackID:     mediaTrackID,
						Title:       mediaTitle,
						Artist:      artists,
						Album:       mediaAlbum,
						ArtURL:      mediaArtURL,
						LengthMicro: mediaLengthMicro,
					},
				},
			},
		}

		data, err := json.Marshal(ev)
		if err != nil {
			return err
		}
		return sendUDSRequest(triggerSocketPath, daemon.UDSRequest{Type: "media_state", Data: data})
	},
}

// Telemetry variables
var (
	telCPUUsage    float64
	telCPUTemp     float64
	telRAMUsed     uint64
	telRAMTotal    uint64
	telRAMPerc     float64
	telGPUUsage    float64
	telGPUTemp     float64
	telGPUMemUsed  uint64
	telGPUMemTotal uint64
)

// TelemetryCmd triggers system telemetry metrics updates
var TelemetryCmd = &cobra.Command{
	Use:   "telemetry",
	Short: "Trigger a mock system metrics telemetry update",
	RunE: func(cmd *cobra.Command, args []string) error {
		if telRAMPerc == 0 && telRAMTotal > 0 {
			telRAMPerc = (float64(telRAMUsed) / float64(telRAMTotal)) * 100
		}

		sysMetrics := metrics.SystemMetrics{
			CPU: metrics.CPUMetrics{
				UsagePercent: telCPUUsage,
				TempCelsius:  telCPUTemp,
			},
			RAM: metrics.RAMMetrics{
				UsedBytes:  telRAMUsed,
				TotalBytes: telRAMTotal,
				Percentage: telRAMPerc,
			},
			GPU: metrics.GPUMetrics{
				UsagePercent:   telGPUUsage,
				TempCelsius:    telGPUTemp,
				VramUsedBytes:  telGPUMemUsed,
				VramTotalBytes: telGPUMemTotal,
			},
		}

		data, err := json.Marshal(sysMetrics)
		if err != nil {
			return err
		}
		return sendUDSRequest(triggerSocketPath, daemon.UDSRequest{Type: "telemetry", Data: data})
	},
}

// Power variables
var (
	powerActiveProfile     string
	powerAvailableProfiles string
)

// PowerCmd triggers a mock power profile state event
var PowerCmd = &cobra.Command{
	Use:   "power",
	Short: "Trigger a mock power profile state event",
	RunE: func(cmd *cobra.Command, args []string) error {
		var profiles []power.PowerProfile
		if powerAvailableProfiles != "" {
			parts := strings.Split(powerAvailableProfiles, ",")
			for _, part := range parts {
				trimmed := strings.TrimSpace(part)
				if trimmed != "" {
					profiles = append(profiles, power.PowerProfile{Profile: trimmed})
				}
			}
		}

		ev := power.PowerProfileState{
			ActiveProfile:     powerActiveProfile,
			AvailableProfiles: profiles,
		}

		data, err := json.Marshal(ev)
		if err != nil {
			return err
		}
		return sendUDSRequest(triggerSocketPath, daemon.UDSRequest{Type: "power_profile_state", Data: data})
	},
}

// Raw variables
var (
	rawType string
	rawData string
)

// RawCmd triggers a raw unvalidated trigger JSON payload
var RawCmd = &cobra.Command{
	Use:   "raw",
	Short: "Trigger a raw custom JSON payload to all clients",
	RunE: func(cmd *cobra.Command, args []string) error {
		if rawType == "" {
			return fmt.Errorf("flag --type is required")
		}
		if rawData == "" {
			return fmt.Errorf("flag --data is required")
		}

		// Ensure rawData is valid JSON
		var check map[string]interface{}
		if err := json.Unmarshal([]byte(rawData), &check); err != nil {
			// Check if it is a JSON array
			var checkArr []interface{}
			if err2 := json.Unmarshal([]byte(rawData), &checkArr); err2 != nil {
				return fmt.Errorf("flag --data must be a valid JSON string: %w", err)
			}
		}

		return sendUDSRequest(triggerSocketPath, daemon.UDSRequest{
			Type: rawType,
			Data: json.RawMessage(rawData),
		})
	},
}

func init() {
	// Global Socket path flag for trigger subcommands
	TriggerCmd.PersistentFlags().StringVarP(&triggerSocketPath, "socket", "s", "", "Unix Domain Socket override path")

	// Notification subcommand flags
	NotificationCmd.Flags().StringVar(&notifAppName, "app-name", "pc-dashboard", "Triggering app name")
	NotificationCmd.Flags().Uint32Var(&notifReplacesID, "replaces-id", 0, "Notification ID to replace")
	NotificationCmd.Flags().StringVar(&notifAppIcon, "icon", "dialog-information", "App icon name")
	NotificationCmd.Flags().StringVar(&notifSummary, "summary", "Alert", "Notification title summary")
	NotificationCmd.Flags().StringVar(&notifBody, "body", "System trigger action simulated.", "Notification body description")
	NotificationCmd.Flags().StringVar(&notifActions, "actions", "", "Comma-separated key,label pairs (e.g. 'dismiss,Dismiss')")
	NotificationCmd.Flags().Int32Var(&notifExpireTimeout, "timeout", -1, "Expire timeout in milliseconds")

	// Media subcommand flags
	MediaCmd.Flags().StringVar(&mediaPlayerName, "player", "spotify", "Target media player name")
	MediaCmd.Flags().StringVar(&mediaStatus, "status", "Playing", "Playback status (Playing, Paused, Stopped)")
	MediaCmd.Flags().Float64Var(&mediaVolume, "volume", 0.75, "Player volume ratio (0.0 to 1.0)")
	MediaCmd.Flags().Int64Var(&mediaPositionMicro, "position", 45000000, "Current track position in microseconds")
	MediaCmd.Flags().StringVar(&mediaTrackID, "track-id", "spotify:track:uds-track", "Metadata unique track URI")
	MediaCmd.Flags().StringVar(&mediaTitle, "title", "UDS Trigger Track", "Track song title")
	MediaCmd.Flags().StringVar(&mediaArtist, "artist", "Antigravity,Agent", "Track artist names (comma-separated)")
	MediaCmd.Flags().StringVar(&mediaAlbum, "album", "UDS Testing Album", "Track album title")
	MediaCmd.Flags().StringVar(&mediaArtURL, "art-url", "https://localhost/art.png", "Cover art image URL")
	MediaCmd.Flags().Int64Var(&mediaLengthMicro, "length", 180000000, "Track length duration in microseconds")

	// Telemetry subcommand flags
	TelemetryCmd.Flags().Float64Var(&telCPUUsage, "cpu-usage", 25.5, "Overall CPU busy percentage")
	TelemetryCmd.Flags().Float64Var(&telCPUTemp, "cpu-temp", 45.0, "Overall CPU core packages temperature")
	TelemetryCmd.Flags().Uint64Var(&telRAMUsed, "ram-used", 8*1024*1024*1024, "Total system RAM bytes used")
	TelemetryCmd.Flags().Uint64Var(&telRAMTotal, "ram-total", 16*1024*1024*1024, "Total system RAM bytes capacity")
	TelemetryCmd.Flags().Float64Var(&telRAMPerc, "ram-percentage", 0, "Specific RAM usage percentage (optional)")
	TelemetryCmd.Flags().Float64Var(&telGPUUsage, "gpu-usage", 15.0, "Overall GPU busy percentage")
	TelemetryCmd.Flags().Float64Var(&telGPUTemp, "gpu-temp", 50.0, "Overall GPU temperature")
	TelemetryCmd.Flags().Uint64Var(&telGPUMemUsed, "gpu-mem-used", 2*1024*1024*1024, "GPU VRAM bytes used")
	TelemetryCmd.Flags().Uint64Var(&telGPUMemTotal, "gpu-mem-total", 8*1024*1024*1024, "GPU VRAM total capacity")

	// Power subcommand flags
	PowerCmd.Flags().StringVar(&powerActiveProfile, "active", "balanced", "Active power profile (e.g. 'power-saver', 'balanced', 'performance')")
	PowerCmd.Flags().StringVar(&powerAvailableProfiles, "available", "power-saver,balanced,performance", "Comma-separated list of available power profiles")

	// Raw subcommand flags
	RawCmd.Flags().StringVarP(&rawType, "type", "t", "", "Custom type wrapper name (e.g. 'custom_metrics')")
	RawCmd.Flags().StringVarP(&rawData, "data", "d", "", "Valid JSON string payload (e.g. '{\"key\": \"val\"}')")

	// DPMS subcommand flags
	DpmsCmd.Flags().StringVar(&dpmsState, "state", "off", "Display power state (on or off)")

	// Wire child subcommands into TriggerCmd
	TriggerCmd.AddCommand(LockCmd)
	TriggerCmd.AddCommand(UnlockCmd)
	TriggerCmd.AddCommand(DpmsCmd)
	TriggerCmd.AddCommand(NotificationCmd)
	TriggerCmd.AddCommand(MediaCmd)
	TriggerCmd.AddCommand(TelemetryCmd)
	TriggerCmd.AddCommand(PowerCmd)
	TriggerCmd.AddCommand(RawCmd)

	// Register trigger to RootCmd
	RootCmd.AddCommand(TriggerCmd)
}

func sendUDSRequest(socketPath string, req daemon.UDSRequest) error {
	if socketPath == "" {
		xdgRuntime := os.Getenv("XDG_RUNTIME_DIR")
		if xdgRuntime != "" {
			socketPath = filepath.Join(xdgRuntime, "pc-dashboard-server.sock")
		} else {
			socketPath = filepath.Join(os.TempDir(), "pc-dashboard-server.sock")
		}
	}

	conn, err := net.DialTimeout("unix", socketPath, 3*time.Second)
	if err != nil {
		return fmt.Errorf("failed to connect to daemon command socket at %s (is the daemon running?): %w", socketPath, err)
	}
	defer conn.Close()

	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return fmt.Errorf("failed to send trigger request: %w", err)
	}

	var resp daemon.UDSResponse
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return fmt.Errorf("failed to decode response from daemon: %w", err)
	}

	if !resp.Success {
		return fmt.Errorf("daemon failed to process trigger: %s", resp.Error)
	}

	fmt.Printf("Successfully triggered event %q! Broadcasted to %d active WebSocket client(s).\n", req.Type, resp.ClientCount)
	return nil
}
