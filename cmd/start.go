package cmd

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/noosxe/pc-dashboard-server/pkg/adb"
	"github.com/noosxe/pc-dashboard-server/pkg/config"
	"github.com/noosxe/pc-dashboard-server/pkg/daemon"
	"github.com/noosxe/pc-dashboard-server/pkg/metrics"
	"github.com/spf13/cobra"
)

var (
	configPath     string
	emulateMetrics bool
	mockADB        bool
	serverPort     int
	verbose        bool
	logLevel       string
	logFormat      string
)

// StartCmd represents the start subcommand that launches the core daemon.
var StartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the PC Dashboard Server daemon",
	Long: `Launches the telemetry aggregation, device hotplug monitoring,
and loopback WebSocket streaming server.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// 1. Gather CLI flag overrides
		cliFlags := make(map[string]interface{})
		if serverPort != 0 {
			cliFlags["server.port"] = serverPort
		}
		if logLevel != "" {
			cliFlags["daemon.log_level"] = logLevel
		}
		if logFormat != "" {
			cliFlags["daemon.log_format"] = logFormat
		}

		// 2. Load merged configurations via Koanf
		cfg, err := config.LoadConfig(configPath, cliFlags)
		if err != nil {
			return err
		}

		// Verbose overrides log level unconditionally
		if verbose {
			cfg.Daemon.LogLevel = "debug"
		}

		// Initialize structured logging handler
		var slogLevel slog.Level
		switch strings.ToLower(cfg.Daemon.LogLevel) {
		case "debug":
			slogLevel = slog.LevelDebug
		case "info":
			slogLevel = slog.LevelInfo
		case "warn", "warning":
			slogLevel = slog.LevelWarn
		case "error":
			slogLevel = slog.LevelError
		default:
			slogLevel = slog.LevelInfo
		}

		var handler slog.Handler
		opts := &slog.HandlerOptions{Level: slogLevel}
		if strings.ToLower(cfg.Daemon.LogFormat) == "json" {
			handler = slog.NewJSONHandler(os.Stderr, opts)
		} else {
			handler = slog.NewTextHandler(os.Stderr, opts)
		}
		logger := slog.New(handler)

		cliLogger := logger.With("module", "cli")
		metricsLogger := logger.With("module", "metrics")
		adbLogger := logger.With("module", "adb")
		websocketLogger := logger.With("module", "websocket")
		daemonLogger := logger.With("module", "daemon")

		cliLogger.Info("Config successfully loaded", "log_level", cfg.Daemon.LogLevel, "log_format", cfg.Daemon.LogFormat)

		// 3. Resolve metrics provider based on emulation flags
		var mr metrics.MetricsReader
		if emulateMetrics {
			cliLogger.Info("Emulation Mode enabled: Using MockMetricsReader (Sine-wave telemetry)")
			mr = metrics.NewMockMetricsReader(metricsLogger)
		} else {
			cliLogger.Info("Host Mode enabled: Using HostMetricsReader (Direct OS queries)")
			mr = metrics.NewHostMetricsReader(metricsLogger)
		}

		// 4. Resolve ADB provider based on mock flags
		var ac adb.ADBClient
		if mockADB {
			cliLogger.Info("Mock ADB Mode enabled: Using MockADBClient")
			ac = adb.NewMockADBClient(adbLogger)
		} else {
			cliLogger.Info("Production ADB Mode enabled: Using SocketADBClient", "host", cfg.ADB.ServerHost, "port", cfg.ADB.ServerPort)
			ac = adb.NewSocketADBClient(cfg.ADB.ServerHost, cfg.ADB.ServerPort, adbLogger)
		}

		// 5. Setup termination context
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()

		// 6. Build and start daemon engine
		engine := daemon.NewEngine(cfg, mr, ac, daemonLogger, websocketLogger)
		if err := engine.Start(ctx); err != nil {
			if !errors.Is(err, context.Canceled) {
				cliLogger.Error("Daemon terminated with error", "error", err)
				return err
			}
		}

		cliLogger.Info("Daemon cleanly terminated. Goodbye.")
		return nil
	},
}

func init() {
	StartCmd.Flags().StringVarP(&configPath, "config", "c", "", "Path to YAML configuration file")
	StartCmd.Flags().BoolVar(&emulateMetrics, "emulate-metrics", false, "Enable simulated sine-wave telemetry metrics")
	StartCmd.Flags().BoolVar(&mockADB, "mock-adb", false, "Enable simulated USB connection ticks")
	StartCmd.Flags().IntVarP(&serverPort, "port", "p", 0, "Overriding WebSocket local port")
	StartCmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Force log level to debug")
	StartCmd.Flags().StringVar(&logLevel, "log-level", "", "Structured logging level (debug, info, warn, error)")
	StartCmd.Flags().StringVar(&logFormat, "log-format", "", "Structured log output format (text, json)")

	RootCmd.AddCommand(StartCmd)
}
