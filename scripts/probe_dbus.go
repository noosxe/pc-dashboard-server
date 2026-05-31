package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/godbus/dbus/v5"
)

func main() {
	fmt.Println("=== D-Bus MPRIS Diagnostic Tool ===")

	addr := os.Getenv("DBUS_SESSION_BUS_ADDRESS")
	fmt.Printf("DBUS_SESSION_BUS_ADDRESS: %q\n", addr)
	if addr == "" {
		fmt.Println("⚠️ WARNING: DBUS_SESSION_BUS_ADDRESS is empty. D-Bus session bus queries will likely fail or fall back.")
	}

	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		fmt.Printf("❌ ERROR: Failed to connect to D-Bus Session Bus: %v\n", err)
		fmt.Println("\nPossible Causes:")
		fmt.Println("1. Isolation: If you are running this inside a Docker/Devcontainer environment, the host D-Bus session socket is not mounted/shared by default.")
		fmt.Println("2. No D-Bus running: Ensure a D-Bus daemon is active in your current session.")
		os.Exit(1)
	}
	defer conn.Close()
	fmt.Println("✅ SUCCESS: Connected to D-Bus Session Bus!")

	obj := conn.Object("org.freedesktop.DBus", "/org/freedesktop/DBus")

	var names []string
	err = obj.Call("org.freedesktop.DBus.ListNames", 0).Store(&names)
	if err != nil {
		fmt.Printf("❌ ERROR: Failed to list D-Bus names: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("\nScanning for active MPRIS Media Players...")
	found := 0
	for _, name := range names {
		if strings.HasPrefix(name, "org.mpris.MediaPlayer2.") {
			found++
			var owner string
			err = obj.Call("org.freedesktop.DBus.GetNameOwner", 0, name).Store(&owner)
			if err != nil {
				fmt.Printf("  - %s (Failed to get owner: %v)\n", name, err)
			} else {
				fmt.Printf("  - %s (Owner: %s)\n", name, owner)
			}
		}
	}
	if found == 0 {
		fmt.Println("  No active MPRIS players found on this D-Bus session bus.")
		fmt.Println("\nPossible Causes:")
		fmt.Println("1. Spotify / Twitch browser are running on a different bus (e.g. host bus while this tool runs inside an isolated container).")
		fmt.Println("2. Sandboxing: If Spotify or your browser is running via Flatpak or Snap, sandbox restrictions might block them from communicating on the D-Bus session bus.")
	} else {
		fmt.Printf("Found %d active player(s).\n", found)
	}

	fmt.Println("\nListening for MPRIS PropertiesChanged signals (Ctrl+C to exit)...")
	err = obj.Call("org.freedesktop.DBus.AddMatch", 0, "type='signal',interface='org.freedesktop.DBus.Properties',member='PropertiesChanged',path='/org/mpris/MediaPlayer2'").Err
	if err != nil {
		fmt.Printf("❌ ERROR: Failed to add match rule: %v\n", err)
		os.Exit(1)
	}

	ch := make(chan *dbus.Signal, 10)
	conn.Signal(ch)

	for sig := range ch {
		fmt.Printf("\n[Signal Caught] Name: %s, Path: %s, Sender: %s\n", sig.Name, sig.Path, sig.Sender)
		for i, val := range sig.Body {
			fmt.Printf("  Body[%d]: %v\n", i, val)
		}
	}
}
