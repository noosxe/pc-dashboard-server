package power

import (
	"context"
	"testing"
	"time"

	"log/slog"
	"os"
)

func TestMockPowerProfilesManager(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	mgr := NewMockPowerProfilesManager(logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, err := mgr.Start(ctx)
	if err != nil {
		t.Fatalf("failed to start mock power manager: %v", err)
	}

	// 1. Check initial state
	select {
	case state := <-ch:
		if state.ActiveProfile != "balanced" {
			t.Errorf("expected active profile to be balanced, got %s", state.ActiveProfile)
		}
		if len(state.AvailableProfiles) != 3 {
			t.Errorf("expected 3 available profiles, got %d", len(state.AvailableProfiles))
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for initial state")
	}

	// 2. Set valid power profile
	err = mgr.SetPowerProfile(ctx, "power-saver")
	if err != nil {
		t.Fatalf("failed to set power profile: %v", err)
	}

	select {
	case state := <-ch:
		if state.ActiveProfile != "power-saver" {
			t.Errorf("expected active profile to transition to power-saver, got %s", state.ActiveProfile)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for profile change state")
	}

	// 3. Set invalid power profile (should fail)
	err = mgr.SetPowerProfile(ctx, "invalid-profile")
	if err == nil {
		t.Error("expected error setting invalid profile, got nil")
	}
}
