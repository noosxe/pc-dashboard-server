package metrics

import (
	"io"
	"log/slog"
	"testing"
)

func TestMockMetricsReader_CPU(t *testing.T) {
	reader := NewMockMetricsReader(slog.New(slog.NewTextHandler(io.Discard, nil)))
	metrics, err := reader.ReadCPU()
	if err != nil {
		t.Fatalf("unexpected error reading CPU: %v", err)
	}

	if metrics.UsagePercent < 0.0 || metrics.UsagePercent > 100.0 {
		t.Errorf("expected CPU UsagePercent in [0, 100], got %f", metrics.UsagePercent)
	}

	if metrics.TempCelsius < 15.0 || metrics.TempCelsius > 110.0 {
		t.Errorf("expected CPU TempCelsius in realistic bounds, got %f", metrics.TempCelsius)
	}
}

func TestMockMetricsReader_RAM(t *testing.T) {
	reader := NewMockMetricsReader(slog.New(slog.NewTextHandler(io.Discard, nil)))
	metrics, err := reader.ReadRAM()
	if err != nil {
		t.Fatalf("unexpected error reading RAM: %v", err)
	}

	if metrics.TotalBytes != 34359738368 {
		t.Errorf("expected RAM TotalBytes to be exactly 32GB (34359738368), got %d", metrics.TotalBytes)
	}

	if metrics.UsedBytes > metrics.TotalBytes {
		t.Errorf("expected RAM UsedBytes (%d) to be <= TotalBytes (%d)", metrics.UsedBytes, metrics.TotalBytes)
	}

	if metrics.Percentage < 0.0 || metrics.Percentage > 100.0 {
		t.Errorf("expected RAM Percentage in [0, 100], got %f", metrics.Percentage)
	}
}

func TestMockMetricsReader_GPU(t *testing.T) {
	reader := NewMockMetricsReader(slog.New(slog.NewTextHandler(io.Discard, nil)))
	metrics, err := reader.ReadGPU()
	if err != nil {
		t.Fatalf("unexpected error reading GPU: %v", err)
	}

	if metrics.UsagePercent < 0.0 || metrics.UsagePercent > 100.0 {
		t.Errorf("expected GPU UsagePercent in [0, 100], got %f", metrics.UsagePercent)
	}

	if metrics.TempCelsius < 20.0 || metrics.TempCelsius > 115.0 {
		t.Errorf("expected GPU TempCelsius in realistic bounds, got %f", metrics.TempCelsius)
	}

	if metrics.VramTotalBytes != 8589934592 {
		t.Errorf("expected GPU VramTotalBytes to be exactly 8GB (8589934592), got %d", metrics.VramTotalBytes)
	}

	if metrics.VramUsedBytes > metrics.VramTotalBytes {
		t.Errorf("expected GPU VramUsedBytes (%d) to be <= VramTotalBytes (%d)", metrics.VramUsedBytes, metrics.VramTotalBytes)
	}
}
