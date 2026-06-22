# Installation Guide

This guide details the different methods available to install the **PC Dashboard Server** daemon on your Linux host system.

## Prerequisites

- **Go Compiler**: Go `1.26` or higher.
- **Android Debug Bridge (ADB)**: Standard `adb` utility installed on the host.
  - On Debian/Ubuntu: `sudo apt install adb`
  - Ensure the ADB server is started: `adb start-server`

### CPU Power Telemetry Permissions (Optional)
Accessing RAPL energy counters on modern Linux kernels is restricted to root by default. To collect CPU power as a non-root user (when running as a systemd user service), configure permissions using either of these methods:

#### Option A: `sysfsutils` (File Mode Persistence)
Install `sysfsutils` (`sudo apt install sysfsutils` on Debian/Ubuntu, `sudo dnf install sysfsutils` on Fedora, or `sudo pacman -S sysfsutils` on Arch Linux) and add the following lines to `/etc/sysfs.conf`:
```text
mode class/powercap/intel-rapl:0/energy_uj = 0444
mode class/powercap/intel-rapl:0/intel-rapl:0:0/energy_uj = 0444
```

#### Option B: Udev Rules (Group-Based Access)
1. Create a dedicated group (e.g., `rapl`):
   ```bash
   sudo groupadd rapl
   ```
2. Add the user running the daemon (e.g., your active desktop user) to the new group:
   ```bash
   sudo usermod -aG rapl $USER
   ```
   *(Note: You will need to log out and log back in for the new group membership to take effect).*
3. Create a udev rules file at `/etc/udev/rules.d/70-intel-rapl.rules` to assign read permissions to the `rapl` group:
   ```text
   SUBSYSTEM=="powercap", ACTION=="add|change", KERNEL=="intel-rapl:*", RUN+="/usr/bin/chgrp rapl /sys/%p/energy_uj", RUN+="/usr/bin/chmod 0640 /sys/%p/energy_uj"
   ```
4. Reload and trigger the udev rules to apply the permissions immediately (without rebooting):
   ```bash
   sudo udevadm control --reload-rules && sudo udevadm trigger
   ```
*(Note: If no permissions are configured, the daemon will gracefully omit the `"power_watts"` CPU metric instead of failing).*

---

## 1. Primary Installation (`go install`)

Install the server daemon directly using Go's official package installer:

```bash
go install github.com/noosxe/pc-dashboard-server@latest
```

> [!TIP]
> Ensure your Go binary path is included in your shell's environment variables. You can add the following to your `~/.bashrc` or `~/.zshrc`:
> ```bash
> export PATH=$PATH:$(go env GOPATH)/bin
> ```

---

## 2. Building from Source

Alternatively, clone the repository and build the binary manually:

```bash
# Clone the repository
git clone https://github.com/noosxe/pc-dashboard-server.git
cd pc-dashboard-server

# Build the executable
go build -o pc-dashboard-server main.go
```

---

## 3. Nix Flake & NixOS Installation

If you are running NixOS or using the Nix package manager, you can use the provided Nix flake to install the daemon or configure it system-wide as a systemd user service.

### A. NixOS Module Prerequisites (ADB Setup)

The server daemon runs as a user-level service and communicates with ADB via TCP port `5037`. You must enable the ADB server system-wide on your NixOS host and assign your user to the `adbusers` group to allow physical USB port access:

```nix
# In your /etc/nixos/configuration.nix:
programs.adb.enable = true;

users.users.<your-username>.extraGroups = [ "adbusers" ];
```

By default, the daemon's NixOS module will automatically start the local ADB server daemon before launching (`services.pc-dashboard-server.adb.autoStartServer = true`). This spawns the ADB server under your unprivileged user session context (inheriting access to your local keys at `~/.android/` for device authentication) so the server is ready out-of-the-box. If you configure a remote ADB server or manage its lifecycle manually, you can set `autoStartServer = false;`.

### B. Adding the Flake & Configuring the Service

You can import this repository as a flake input, add its default overlay, and enable the module in your NixOS configuration:

```nix
# flake.nix
{
  inputs = {
    nixpkgs.url = "github:nixos/nixpkgs/nixos-unstable";
    
    pc-dashboard-server = {
      url = "github:noosxe/pc-dashboard-server";
      inputs.nixpkgs.follows = "nixpkgs";
    };
  };

  outputs = { nixpkgs, pc-dashboard-server, ... }: {
    nixosConfigurations.my-host = nixpkgs.lib.nixosSystem {
      modules = [
        ./configuration.nix
        
        # Import the module exposed by the flake
        pc-dashboard-server.nixosModules.default
        
        # Configure the overlay and enable the daemon
        ({ pkgs, ... }: {
          nixpkgs.overlays = [
            pc-dashboard-server.overlays.default
          ];

          services.pc-dashboard-server = {
            enable = true;
            # Package default automatically resolves to overlay's pkgs.pc-dashboard-server
            port = 12345;
            logLevel = "info";
            # Optional: Enable metrics emulation or mock states
            emulateMetrics = false; 
            # Optional: Enable udev rules for CPU RAPL energy (power_watts telemetry)
            enableCpuPowerMetrics = true;
          };
        })
      ];
    };
  };
}
```

Once applied, the systemd user service `pc-dashboard-server` will start automatically in your graphical session. You can manage it with:
```bash
systemctl --user status pc-dashboard-server
```
