package cmd

import (
	"context"
	"errors"
	"log"
	"os"
	"os/signal"
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

		// 2. Load merged configurations via Koanf
		cfg, err := config.LoadConfig(configPath, cliFlags)
		if err != nil {
			return err
		}

		log.Printf("[CLI] Config successfully loaded. Log level: %s", cfg.Daemon.LogLevel)

		// 3. Resolve metrics provider based on emulation flags
		var mr metrics.MetricsReader
		if emulateMetrics {
			log.Printf("[CLI] Emulation Mode enabled: Using MockMetricsReader (Sine-wave telemetry)")
			mr = metrics.NewMockMetricsReader()
		} else {
			log.Printf("[CLI] Host Mode enabled: Using HostMetricsReader (Direct OS queries)")
			mr = metrics.NewHostMetricsReader()
		}

		// 4. Resolve ADB provider based on mock flags
		var ac adb.ADBClient
		if mockADB {
			log.Printf("[CLI] Mock ADB Mode enabled: Using MockADBClient")
			ac = adb.NewMockADBClient()
		} else {
			log.Printf("[CLI] Production ADB Mode enabled: Using SocketADBClient (TCP:%d)", cfg.ADB.ServerPort)
			ac = adb.NewSocketADBClient(cfg.ADB.ServerHost, cfg.ADB.ServerPort)
		}

		// 5. Setup termination context
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()

		// 6. Build and start daemon engine
		engine := daemon.NewEngine(cfg, mr, ac)
		if err := engine.Start(ctx); err != nil {
			if !errors.Is(err, context.Canceled) {
				log.Printf("[CLI] Daemon terminated with error: %v", err)
				return err
			}
		}

		log.Printf("[CLI] Daemon cleanly terminated. Goodbye.")
		return nil
	},
}

func init() {
	StartCmd.Flags().StringVarP(&configPath, "config", "c", "", "Path to YAML configuration file")
	StartCmd.Flags().BoolVar(&emulateMetrics, "emulate-metrics", false, "Enable simulated sine-wave telemetry metrics")
	StartCmd.Flags().BoolVar(&mockADB, "mock-adb", false, "Enable simulated USB connection ticks")
	StartCmd.Flags().IntVarP(&serverPort, "port", "p", 0, "Overriding WebSocket local port")

	RootCmd.AddCommand(StartCmd)
}
