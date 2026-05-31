package mpris

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"
)

func TestMockMPRISManager_StartAndLifecycle(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	manager := NewMockMPRISManager(logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events, err := manager.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start manager: %v", err)
	}

	// 1. Initial State Broadcast
	select {
	case ev := <-events:
		if len(ev.ActivePlayers) != 1 {
			t.Fatalf("Expected 1 active player, got %d", len(ev.ActivePlayers))
		}
		player := ev.ActivePlayers[0]
		if player.PlayerName != "spotify" {
			t.Errorf("Expected player spotify, got %s", player.PlayerName)
		}
		if player.Identity != "Spotify" {
			t.Errorf("Expected Identity Spotify, got %s", player.Identity)
		}
		if player.DesktopEntry != "spotify" {
			t.Errorf("Expected DesktopEntry spotify, got %s", player.DesktopEntry)
		}
		if player.PlaybackStatus != StatusPlaying {
			t.Errorf("Expected status Playing, got %s", player.PlaybackStatus)
		}
		if player.Volume != 0.85 {
			t.Errorf("Expected volume 0.85, got %f", player.Volume)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for initial state broadcast")
	}

	// 2. Test Pause Command
	err = manager.SendCommand(ctx, "spotify", "pause", nil)
	if err != nil {
		t.Fatalf("Failed to send pause command: %v", err)
	}

	select {
	case ev := <-events:
		player := ev.ActivePlayers[0]
		if player.PlaybackStatus != StatusPaused {
			t.Errorf("Expected status Paused, got %s", player.PlaybackStatus)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for pause command response broadcast")
	}

	// 3. Test Set Volume Command
	err = manager.SendCommand(ctx, "spotify", "set_volume", map[string]interface{}{"volume": 0.45})
	if err != nil {
		t.Fatalf("Failed to send set_volume command: %v", err)
	}

	select {
	case ev := <-events:
		player := ev.ActivePlayers[0]
		if player.Volume != 0.45 {
			t.Errorf("Expected volume 0.45, got %f", player.Volume)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for set_volume command response broadcast")
	}

	// 4. Test Seek Command
	err = manager.SendCommand(ctx, "spotify", "seek", map[string]interface{}{"offset_microseconds": int64(10000000)})
	if err != nil {
		t.Fatalf("Failed to send seek command: %v", err)
	}

	select {
	case ev := <-events:
		player := ev.ActivePlayers[0]
		// Starts at 45s (45000000us) + 10s = 55s (55000000us)
		if player.PositionMicro != 55000000 {
			t.Errorf("Expected position 55000000, got %d", player.PositionMicro)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for seek command response broadcast")
	}

	// 5. Test Set Position Command
	err = manager.SendCommand(ctx, "spotify", "set_position", map[string]interface{}{"position_microseconds": int64(20000000)})
	if err != nil {
		t.Fatalf("Failed to send set_position command: %v", err)
	}

	select {
	case ev := <-events:
		player := ev.ActivePlayers[0]
		if player.PositionMicro != 20000000 {
			t.Errorf("Expected position 20000000, got %d", player.PositionMicro)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for set_position command response broadcast")
	}
}
