self:
{ config, lib, pkgs, ... }:

with lib;

let
  cfg = config.services.pc-dashboard-server;
  
  tomlFormat = pkgs.formats.toml { };
  configFile = tomlFormat.generate "pc-dashboard-server-config.toml" {
    server = {
      host = cfg.host;
      port = cfg.port;
    };
    daemon = {
      update_interval_ms = cfg.updateIntervalMs;
      locked_update_interval_ms = cfg.lockedUpdateIntervalMs;
      log_level = cfg.logLevel;
      log_format = cfg.logFormat;
    } // optionalAttrs (cfg.socketPath != null) {
      # Leave the key out when unset: an empty socket_path disables the
      # control socket instead of falling back to the default path.
      socket_path = cfg.socketPath;
    };
    adb = {
      server_host = cfg.adb.serverHost;
      server_port = cfg.adb.serverPort;
      target_package = cfg.adb.targetPackage;
      target_activity = cfg.adb.targetActivity;
      no_app_control = cfg.adb.noAppControl;
    };
  };

  # Construct command line flags based on option settings
  flags = [
    "--config" "${configFile}"
  ] ++ optional cfg.emulateMetrics "--emulate-metrics"
    ++ optional cfg.mockAdb "--mock-adb"
    ++ optional cfg.mockNotifications "--mock-notifications"
    ++ optional cfg.mockLock "--mock-lock"
    ++ optional cfg.mockDpms "--mock-dpms"
    ++ cfg.extraFlags;

  manageAdbServer = cfg.adb.autoStartServer
    && (cfg.adb.serverHost == "127.0.0.1" || cfg.adb.serverHost == "localhost");
in
{
  options.services.pc-dashboard-server = {
    enable = mkEnableOption "PC Dashboard Server daemon";

    package = mkOption {
      type = types.package;
      default = pkgs.pc-dashboard-server or self.packages.${pkgs.stdenv.hostPlatform.system}.default;
      defaultText = literalExpression "pkgs.pc-dashboard-server or self.packages.\${pkgs.stdenv.hostPlatform.system}.default";
      description = "The pc-dashboard-server package to use.";
    };

    installBinary = mkOption {
      type = types.bool;
      default = false;
      description = "Add the pc-dashboard-server package to the system PATH (all users).";
    };

    host = mkOption {
      type = types.str;
      default = "127.0.0.1";
      description = "Loopback IP address interface to bind the WebSocket server to.";
    };

    port = mkOption {
      type = types.port;
      default = 12345;
      description = "Local port for the WebSocket server.";
    };

    updateIntervalMs = mkOption {
      type = types.int;
      default = 1000;
      description = "Frequency of standard telemetry updates in milliseconds.";
    };

    lockedUpdateIntervalMs = mkOption {
      type = types.int;
      default = 5000;
      description = "Frequency of telemetry updates when the session is locked in milliseconds.";
    };

    logLevel = mkOption {
      type = types.enum [ "debug" "info" "warn" "error" ];
      default = "info";
      description = "Structured logging level.";
    };

    logFormat = mkOption {
      type = types.enum [ "text" "json" ];
      default = "text";
      description = "Structured log output format.";
    };

    socketPath = mkOption {
      type = types.nullOr types.str;
      default = null;
      description = "Path to the control Unix Domain Socket (defaults to XDG_RUNTIME_DIR/pc-dashboard-server.sock).";
    };

    adb = {
      serverHost = mkOption {
        type = types.str;
        default = "127.0.0.1";
        description = "ADB server host to connect to.";
      };

      serverPort = mkOption {
        type = types.port;
        default = 5037;
        description = "ADB server port.";
      };

      targetPackage = mkOption {
        type = types.str;
        default = "com.noosxe.pc_dashboard";
        description = "Companion Android app package identifier.";
      };

      targetActivity = mkOption {
        type = types.str;
        default = "com.noosxe.pc_dashboard.MainActivity";
        description = "Companion Android app launch activity name.";
      };

      noAppControl = mkOption {
        type = types.bool;
        default = false;
        description = "Prevent the daemon from launching or closing the companion app.";
      };

      autoStartServer = mkOption {
        type = types.bool;
        default = true;
        description = "Run the local ADB server as a supervised user service (pc-dashboard-adb.service) alongside the daemon.";
      };
    };

    emulateMetrics = mkOption {
      type = types.bool;
      default = false;
      description = "Enable simulated sine-wave telemetry metrics.";
    };

    mockAdb = mkOption {
      type = types.bool;
      default = false;
      description = "Enable simulated USB connection ticks.";
    };

    mockNotifications = mkOption {
      type = types.bool;
      default = false;
      description = "Enable simulated desktop notifications sync.";
    };

    mockLock = mkOption {
      type = types.bool;
      default = false;
      description = "Enable simulated session lock/unlock events.";
    };

    mockDpms = mkOption {
      type = types.bool;
      default = false;
      description = "Enable simulated DPMS display power events.";
    };

    enableCpuPowerMetrics = mkOption {
      type = types.bool;
      default = false;
      description = "Configure system-wide udev rules to make RAPL energy files readable by standard users (mode 0444), enabling CPU power telemetry for the non-root daemon service.";
    };

    extraFlags = mkOption {
      type = types.listOf types.str;
      default = [];
      description = "Additional command line arguments to pass to the daemon start command.";
    };
  };

  config = mkIf cfg.enable {
    environment.systemPackages = mkIf cfg.installBinary [ cfg.package ];

    # The ADB server runs in the foreground as its own unit. `adb start-server`
    # forks the daemon and exits, which would leave it in the dashboard's
    # cgroup ("left-over process") and unsupervised. The dashboard reconnects
    # to the server on its own, so it only needs a soft dependency.
    systemd.user.services.pc-dashboard-adb = mkIf manageAdbServer {
      description = "ADB server for PC Dashboard";
      after = [ "graphical-session-pre.target" ];
      partOf = [ "graphical-session.target" ];
      serviceConfig = {
        Type = "simple";
        ExecStart = "${pkgs.android-tools}/bin/adb -P ${toString cfg.adb.serverPort} server nodaemon";
        Restart = "always";
        RestartSec = "3s";
      };
    };

    systemd.user.services.pc-dashboard-server = {
      description = "PC Dashboard Server Daemon";
      wants = optional manageAdbServer "pc-dashboard-adb.service";
      after = [ "graphical-session-pre.target" ] ++ optional manageAdbServer "pc-dashboard-adb.service";
      partOf = [ "graphical-session.target" ];
      wantedBy = [ "graphical-session.target" ];
      path = optionals (elem "nvidia" (config.services.xserver.videoDrivers or [ ])) [
        config.hardware.nvidia.package
      ];
      serviceConfig = {
        Type = "simple";
        ExecStart = "${cfg.package}/bin/pc-dashboard-server start ${escapeShellArgs flags}";
        Restart = "on-failure";
        RestartSec = "3s";
      };
    };

    services.udev.extraRules = mkIf cfg.enableCpuPowerMetrics ''
      SUBSYSTEM=="powercap", ACTION=="add|change", KERNEL=="intel-rapl:*", RUN+="${pkgs.coreutils}/bin/chmod 0444 /sys/%p/energy_uj"
    '';
  };
}
