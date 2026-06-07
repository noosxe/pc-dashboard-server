package metrics

import (
	"log/slog"
	"math"
	"math/rand"
	"time"
)

// MockMetricsReader generates smooth simulated telemetry waves.
type MockMetricsReader struct {
	logger    *slog.Logger
	startTime time.Time
	randSrc   *rand.Rand
}

// NewMockMetricsReader instantiates a simulation MetricsReader.
func NewMockMetricsReader(logger *slog.Logger) *MockMetricsReader {
	return &MockMetricsReader{
		logger:    logger,
		startTime: time.Now(),
		randSrc:   rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// ReadCPU generates simulated CPU usage and temperature.
func (r *MockMetricsReader) ReadCPU() (CPUMetrics, error) {
	t := time.Since(r.startTime).Seconds()
	jitter := (r.randSrc.Float64() * 4.0) - 2.0 // random(-2.0, 2.0)

	usage := 15.0 + math.Sin(t/8.0)*10.0 + jitter
	if usage < 0 {
		usage = 0
	} else if usage > 100 {
		usage = 100
	}

	temp := 40.0 + math.Sin(t/8.0)*5.0 + (usage * 0.3)

	jitterFreq := (r.randSrc.Float64() * 10.0) - 5.0 // random(-5.0, 5.0)
	freq := 2500.0 + (usage * 20.0) + jitterFreq

	jitterPower := (r.randSrc.Float64() * 2.0) - 1.0 // random(-1.0, 1.0)
	power := 10.0 + (usage * 0.8) + jitterPower
	if power < 5.0 {
		power = 5.0
	}

	return CPUMetrics{
		UsagePercent: math.Round(usage*100) / 100,
		TempCelsius:  math.Round(temp*100) / 100,
		FreqMHz:      math.Round(freq*100) / 100,
		PowerWatts:   math.Round(power*100) / 100,
	}, nil
}

// ReadRAM generates simulated memory stats.
func (r *MockMetricsReader) ReadRAM() (RAMMetrics, error) {
	t := time.Since(r.startTime).Seconds()
	total := uint64(34359738368) // 32GB

	// 12GB +/- 1GB slowly
	used := float64(12884901888) + math.Sin(t/30.0)*1073741824
	usedPercent := (used / float64(total)) * 100.0

	return RAMMetrics{
		TotalBytes: total,
		UsedBytes:  uint64(used),
		Percentage: math.Round(usedPercent*100) / 100,
	}, nil
}

// ReadGPU generates simulated graphics processor stats.
func (r *MockMetricsReader) ReadGPU() (GPUMetrics, error) {
	t := time.Since(r.startTime).Seconds()
	jitter := (r.randSrc.Float64() * 2.0) - 1.0 // random(-1.0, 1.0)

	usage := 30.0 + math.Cos(t/15.0)*15.0 + jitter
	if usage < 0 {
		usage = 0
	} else if usage > 100 {
		usage = 100
	}

	temp := 50.0 + math.Cos(t/15.0)*8.0 + (usage * 0.2)

	totalVram := uint64(8589934592) // 8GB
	usedVram := float64(3221225472) + (usage * 20000000.0)

	jitterFreq := (r.randSrc.Float64() * 4.0) - 2.0 // random(-2.0, 2.0)
	freq := 300.0 + (usage * 15.0) + jitterFreq

	jitterPower := (r.randSrc.Float64() * 4.0) - 2.0 // random(-2.0, 2.0)
	power := 30.0 + (usage * 1.5) + jitterPower
	if power < 10.0 {
		power = 10.0
	}

	jitterVramFreq := (r.randSrc.Float64() * 6.0) - 3.0 // random(-3.0, 3.0)
	vramFreq := 800.0 + (usage * 12.0) + jitterVramFreq

	vramTemp := 50.0 + math.Cos(t/15.0)*10.0 + (usage * 0.25)

	return GPUMetrics{
		UsagePercent:    math.Round(usage*100) / 100,
		TempCelsius:     math.Round(temp*100) / 100,
		VramUsedBytes:   uint64(usedVram),
		VramTotalBytes:  totalVram,
		FreqMHz:         math.Round(freq*100) / 100,
		PowerWatts:      math.Round(power*100) / 100,
		VramTempCelsius: math.Round(vramTemp*100) / 100,
		VramFreqMHz:     math.Round(vramFreq*100) / 100,
	}, nil
}

// ReadSwap generates simulated swap memory stats.
func (r *MockMetricsReader) ReadSwap() (SwapMetrics, error) {
	t := time.Since(r.startTime).Seconds()
	total := uint64(2147483648) // 2GB

	// 500MB +/- 100MB slowly
	used := float64(524288000) + math.Sin(t/20.0)*104857600
	usedPercent := (used / float64(total)) * 100.0

	return SwapMetrics{
		TotalBytes: total,
		UsedBytes:  uint64(used),
		Percentage: math.Round(usedPercent*100) / 100,
	}, nil
}

// ReadZRAM generates simulated ZRAM stats.
func (r *MockMetricsReader) ReadZRAM() (ZRAMMetrics, error) {
	t := time.Since(r.startTime).Seconds()
	total := uint64(4294967296) // 4GB

	// 1GB uncompressed memory slowly fluctuating
	orig := float64(1073741824) + math.Sin(t/15.0)*104857600
	// 350MB compressed size
	compr := orig / 2.85 // ~2.85 compression ratio
	// allocator memory usage slightly higher than compressed due to fragmentation
	memUsed := compr * 1.1

	ratio := 0.0
	if compr > 0 {
		ratio = orig / compr
	}

	return ZRAMMetrics{
		TotalBytes:         total,
		OrigDataSizeBytes:  uint64(orig),
		ComprDataSizeBytes: uint64(compr),
		MemUsedTotalBytes:  uint64(memUsed),
		CompressionRatio:   math.Round(ratio*100) / 100,
	}, nil
}

// GetFlags returns the support status of system metrics (always true for mock/emulation).
func (r *MockMetricsReader) GetFlags() TelemetryFlags {
	return TelemetryFlags{
		CPUUsageSupported:    true,
		CPUTempSupported:     true,
		CPUFreqSupported:     true,
		CPUPowerSupported:    true,
		RAMSupported:         true,
		SwapSupported:        true,
		ZRAMSupported:        true,
		GPUSupported:         true,
		GPUUsageSupported:    true,
		GPUTempSupported:     true,
		GPUVramSupported:     true,
		GPUFreqSupported:     true,
		GPUPowerSupported:    true,
		GPUVramTempSupported: true,
		GPUVramFreqSupported: true,
	}
}
