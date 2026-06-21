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
      socket_path = if cfg.socketPath != null then cfg.socketPath else "";
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
in
{
  options.services.pc-dashboard-server = {
    enable = mkEnableOption "PC Dashboard Server daemon";

    package = mkOption {
      type = types.package;
      default = pkgs.pc-dashboard-server or self.packages.${pkgs.system}.default;
      defaultText = literalExpression "pkgs.pc-dashboard-server or self.packages.\${pkgs.system}.default";
      description = "The pc-dashboard-server package to use.";
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
    systemd.user.services.pc-dashboard-server = {
      description = "PC Dashboard Server Daemon";
      after = [ "graphical-session-pre.target" ];
      partOf = [ "graphical-session.target" ];
      wantedBy = [ "graphical-session.target" ];
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
