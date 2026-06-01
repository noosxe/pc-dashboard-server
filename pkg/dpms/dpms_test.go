package dpms

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"
)

func TestMockDpmsManager(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	mgr := NewMockDpmsManager(logger)

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
		if ev.State != "off" {
			t.Errorf("expected initial mock event to be State='off', got State=%q", ev.State)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("timeout waiting for initial mock event")
	}
}

func TestDbusDpmsManagerDeduplication(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	// Instantiate manually with nil connection to test private methods
	mgr := &DbusDpmsManager{
		logger:       logger,
		isFirstEvent: true,
	}

	out := make(chan DpmsEvent, 10)

	// 1. First event should be dispatched immediately
	mgr.updateState("off", out)
	select {
	case ev := <-out:
		if ev.State != "off" {
			t.Errorf("expected first event to be State='off', got %q", ev.State)
		}
	default:
		t.Fatal("expected event in channel, got none")
	}

	// 2. Identical subsequent state should be deduplicated (dropped)
	mgr.updateState("off", out)
	select {
	case ev := <-out:
		t.Fatalf("expected no event due to deduplication, got %+v", ev)
	default:
		// success
	}

	// 3. Genuine transition should be dispatched
	mgr.updateState("on", out)
	select {
	case ev := <-out:
		if ev.State != "on" {
			t.Errorf("expected transitioned event to be State='on', got %q", ev.State)
		}
	default:
		t.Fatal("expected event in channel, got none")
	}
}
