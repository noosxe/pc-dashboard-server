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

	return GPUMetrics{
		UsagePercent:   math.Round(usage*100) / 100,
		TempCelsius:    math.Round(temp*100) / 100,
		VramUsedBytes:  uint64(usedVram),
		VramTotalBytes: totalVram,
		FreqMHz:        math.Round(freq*100) / 100,
		PowerWatts:     math.Round(power*100) / 100,
	}, nil
}
