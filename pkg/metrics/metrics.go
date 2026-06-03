package metrics

// CPUMetrics holds processor performance telemetry.
type CPUMetrics struct {
	UsagePercent float64 `json:"usage_percent"`
	TempCelsius  float64 `json:"temp_celsius"`
	FreqMHz      float64 `json:"freq_mhz"`
	PowerWatts   float64 `json:"power_watts"`
}

// RAMMetrics holds physical memory telemetry.
type RAMMetrics struct {
	UsedBytes  uint64  `json:"used_bytes"`
	TotalBytes uint64  `json:"total_bytes"`
	Percentage float64 `json:"percentage"`
}

// GPUMetrics holds graphics processor telemetry.
type GPUMetrics struct {
	UsagePercent   float64 `json:"usage_percent"`
	TempCelsius    float64 `json:"temp_celsius"`
	VramUsedBytes  uint64  `json:"vram_used_bytes"`
	VramTotalBytes uint64  `json:"vram_total_bytes"`
	FreqMHz        float64 `json:"freq_mhz"`
	PowerWatts     float64 `json:"power_watts"`
}

// SystemMetrics combines all gathered hardware statistics.
type SystemMetrics struct {
	CPU CPUMetrics `json:"cpu"`
	RAM RAMMetrics `json:"ram"`
	GPU GPUMetrics `json:"gpu"`
}

// MetricsReader defines the contract for reading system performance telemetry.
type MetricsReader interface {
	// ReadCPU returns overall CPU usage percentages and core package temperatures.
	ReadCPU() (CPUMetrics, error)

	// ReadRAM returns physical host RAM stats (used, total, percentage).
	ReadRAM() (RAMMetrics, error)

	// ReadGPU returns graphics processor core usage, temperature, and VRAM utilization.
	ReadGPU() (GPUMetrics, error)
}
