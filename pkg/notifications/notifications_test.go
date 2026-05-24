package notifications

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestSanitizeHTML(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Hello World", "Hello World"},
		{"Hello <b>World</b>", "Hello World"},
		{"<script>alert(1)</script>Click Here", "alert(1)Click Here"},
		{"<iframe src='bad'></iframe>Safe Text", "Safe Text"},
		{"<div class=\"test\">Styled Text</div>", "Styled Text"},
	}

	for _, tc := range tests {
		got := sanitizeHTML(tc.input)
		if got != tc.expected {
			t.Errorf("sanitizeHTML(%q) = %q; expected %q", tc.input, got, tc.expected)
		}
	}
}

func TestMockNotificationManager_SendNotification(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	mgr := NewMockNotificationManager(logger)

	ctx := context.Background()
	req := NotificationRequest{
		AppName:       "Slack",
		Summary:       "New message",
		Body:          "Hello Bob",
		ExpireTimeout: 3000,
	}

	id, err := mgr.SendNotification(ctx, req)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if id != 101 {
		t.Errorf("expected mock ID to be 101, got %d", id)
	}

	longSummary := strings.Repeat("A", 600)
	longBody := strings.Repeat("B", 2500)
	reqSafety := NotificationRequest{
		Summary: longSummary + "<script>bad</script>",
		Body:    longBody + "<b>bold</b>",
	}

	id2, err := mgr.SendNotification(ctx, reqSafety)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if id2 != 102 {
		t.Errorf("expected mock ID to be 102, got %d", id2)
	}
}

func TestMockNotificationManager_Start(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	mgr := NewMockNotificationManager(logger)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events, err := mgr.Start(ctx)
	if err != nil {
		t.Fatalf("expected no error starting monitor: %v", err)
	}

	select {
	case ev, ok := <-events:
		if !ok {
			t.Fatal("channel closed unexpectedly")
		}
		if ev.AppName == "" || ev.Summary == "" {
			t.Errorf("mock event fields are empty: %+v", ev)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for initial mock event")
	}
}
