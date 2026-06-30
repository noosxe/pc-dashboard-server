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

	if len(metrics.CoresUsagePercent) != 8 {
		t.Errorf("expected CPU CoresUsagePercent to have 8 cores, got %d", len(metrics.CoresUsagePercent))
	} else {
		for i, coreVal := range metrics.CoresUsagePercent {
			if coreVal < 0.0 || coreVal > 100.0 {
				t.Errorf("expected CPU core %d utilization in [0, 100], got %f", i, coreVal)
			}
		}
	}

	if metrics.TempCelsius < 15.0 || metrics.TempCelsius > 110.0 {
		t.Errorf("expected CPU TempCelsius in realistic bounds, got %f", metrics.TempCelsius)
	}

	if metrics.FreqMHz < 500.0 || metrics.FreqMHz > 8000.0 {
		t.Errorf("expected CPU FreqMHz in realistic bounds, got %f", metrics.FreqMHz)
	}

	if metrics.PowerWatts < 0.0 || metrics.PowerWatts > 500.0 {
		t.Errorf("expected CPU PowerWatts in realistic bounds, got %f", metrics.PowerWatts)
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

	if metrics.FreqMHz < 100.0 || metrics.FreqMHz > 4000.0 {
		t.Errorf("expected GPU FreqMHz in realistic bounds, got %f", metrics.FreqMHz)
	}

	if metrics.PowerWatts < 0.0 || metrics.PowerWatts > 1000.0 {
		t.Errorf("expected GPU PowerWatts in realistic bounds, got %f", metrics.PowerWatts)
	}

	if metrics.VramTempCelsius < 20.0 || metrics.VramTempCelsius > 115.0 {
		t.Errorf("expected GPU VramTempCelsius in realistic bounds, got %f", metrics.VramTempCelsius)
	}

	if metrics.VramFreqMHz < 100.0 || metrics.VramFreqMHz > 4000.0 {
		t.Errorf("expected GPU VramFreqMHz in realistic bounds, got %f", metrics.VramFreqMHz)
	}
}

func TestMockMetricsReader_Swap(t *testing.T) {
	reader := NewMockMetricsReader(slog.New(slog.NewTextHandler(io.Discard, nil)))
	metrics, err := reader.ReadSwap()
	if err != nil {
		t.Fatalf("unexpected error reading Swap: %v", err)
	}

	if metrics.TotalBytes != 2147483648 {
		t.Errorf("expected Swap TotalBytes to be exactly 2GB (2147483648), got %d", metrics.TotalBytes)
	}

	if metrics.UsedBytes > metrics.TotalBytes {
		t.Errorf("expected Swap UsedBytes (%d) to be <= TotalBytes (%d)", metrics.UsedBytes, metrics.TotalBytes)
	}

	if metrics.Percentage < 0.0 || metrics.Percentage > 100.0 {
		t.Errorf("expected Swap Percentage in [0, 100], got %f", metrics.Percentage)
	}
}

func TestMockMetricsReader_ZRAM(t *testing.T) {
	reader := NewMockMetricsReader(slog.New(slog.NewTextHandler(io.Discard, nil)))
	metrics, err := reader.ReadZRAM()
	if err != nil {
		t.Fatalf("unexpected error reading ZRAM: %v", err)
	}

	if metrics.TotalBytes != 4294967296 {
		t.Errorf("expected ZRAM TotalBytes to be exactly 4GB (4294967296), got %d", metrics.TotalBytes)
	}

	if metrics.ComprDataSizeBytes > metrics.OrigDataSizeBytes {
		t.Errorf("expected ZRAM ComprDataSizeBytes (%d) to be <= OrigDataSizeBytes (%d)", metrics.ComprDataSizeBytes, metrics.OrigDataSizeBytes)
	}

	if metrics.CompressionRatio < 1.0 || metrics.CompressionRatio > 5.0 {
		t.Errorf("expected ZRAM CompressionRatio to be in realistic bounds, got %f", metrics.CompressionRatio)
	}
}

func TestMockMetricsReader_GetFlags(t *testing.T) {
	reader := NewMockMetricsReader(slog.New(slog.NewTextHandler(io.Discard, nil)))
	flags := reader.GetFlags()

	if !flags.CPUUsageSupported || !flags.CPUCoresUsageSupported || !flags.CPUTempSupported || !flags.CPUFreqSupported || !flags.CPUPowerSupported {
		t.Errorf("expected all CPU flags to be true, got %+v", flags)
	}
	if !flags.RAMSupported {
		t.Errorf("expected RAMSupported to be true, got %+v", flags)
	}
	if !flags.SwapSupported {
		t.Errorf("expected SwapSupported to be true, got %+v", flags)
	}
	if !flags.ZRAMSupported {
		t.Errorf("expected ZRAMSupported to be true, got %+v", flags)
	}
	if !flags.GPUSupported || !flags.GPUUsageSupported || !flags.GPUTempSupported || !flags.GPUVramSupported || !flags.GPUFreqSupported || !flags.GPUPowerSupported || !flags.GPUVramTempSupported || !flags.GPUVramFreqSupported {
		t.Errorf("expected all GPU flags to be true, got %+v", flags)
	}
}

func TestHostMetricsReader_GetFlags(t *testing.T) {
	reader := NewHostMetricsReader(slog.New(slog.NewTextHandler(io.Discard, nil)))

	// Initially flags might be unpopulated or partially populated by warming up CPU times
	_ = reader.GetFlags()

	// Trigger reads to populate flags
	_, _ = reader.ReadCPU()
	_, _ = reader.ReadRAM()
	_, _ = reader.ReadGPU()
	_, _ = reader.ReadSwap()
	_, _ = reader.ReadZRAM()

	flags := reader.GetFlags()
	// At least RAMSupported should be true on standard Linux environments
	if !flags.RAMSupported {
		t.Logf("Warning: RAMSupported is false (could be non-Linux environment in testing)")
	}
	t.Logf("Host flags dynamically resolved: %+v", flags)
}
