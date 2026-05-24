package lock

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"
)

func TestMockLockManager(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	mgr := NewMockLockManager(logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events, err := mgr.Start(ctx)
	if err != nil {
		t.Fatalf("expected no error starting mock manager: %v", err)
	}

	select {
	case ev, ok := <-events:
		if !ok {
			t.Fatal("channel closed unexpectedly")
		}
		if !ev.Locked {
			t.Errorf("expected initial mock event to be Locked=true, got Locked=%v", ev.Locked)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for initial mock event")
	}
}

func TestDbusLockManagerDeduplication(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	// Instantiate manually with nil connections to test private methods
	mgr := &DbusLockManager{
		logger:       logger,
		isFirstEvent: true,
	}

	out := make(chan SessionLockEvent, 10)

	// 1. First event should be dispatched immediately
	mgr.updateState(true, out)
	select {
	case ev := <-out:
		if !ev.Locked {
			t.Errorf("expected first event to be Locked=true, got %v", ev.Locked)
		}
	default:
		t.Fatal("expected event in channel, got none")
	}

	// 2. Identical subsequent state should be deduplicated (dropped)
	mgr.updateState(true, out)
	select {
	case ev := <-out:
		t.Fatalf("expected no event due to deduplication, got %+v", ev)
	default:
		// success
	}

	// 3. Genuine transition should be dispatched
	mgr.updateState(false, out)
	select {
	case ev := <-out:
		if ev.Locked {
			t.Errorf("expected transitioned event to be Locked=false, got %v", ev.Locked)
		}
	default:
		t.Fatal("expected event in channel, got none")
	}
}
