package metrics

// CPUMetrics holds processor performance telemetry.
type CPUMetrics struct {
	UsagePercent      float64   `json:"usage_percent"`
	CoresUsagePercent []float64 `json:"cores_usage_percent"`
	TempCelsius       float64   `json:"temp_celsius"`
	FreqMHz           float64   `json:"freq_mhz"`
	PowerWatts        float64   `json:"power_watts"`
}

// RAMMetrics holds physical memory telemetry.
type RAMMetrics struct {
	UsedBytes  uint64  `json:"used_bytes"`
	TotalBytes uint64  `json:"total_bytes"`
	Percentage float64 `json:"percentage"`
}

// SwapMetrics holds swap memory telemetry.
type SwapMetrics struct {
	UsedBytes  uint64  `json:"used_bytes"`
	TotalBytes uint64  `json:"total_bytes"`
	Percentage float64 `json:"percentage"`
}

// ZRAMMetrics holds zram device telemetry.
type ZRAMMetrics struct {
	OrigDataSizeBytes  uint64  `json:"orig_data_size_bytes"`
	ComprDataSizeBytes uint64  `json:"compr_data_size_bytes"`
	MemUsedTotalBytes  uint64  `json:"mem_used_total_bytes"`
	TotalBytes         uint64  `json:"total_bytes"`
	CompressionRatio   float64 `json:"compression_ratio"`
}

// GPUMetrics holds graphics processor telemetry.
type GPUMetrics struct {
	UsagePercent    float64 `json:"usage_percent"`
	TempCelsius     float64 `json:"temp_celsius"`
	VramUsedBytes   uint64  `json:"vram_used_bytes"`
	VramTotalBytes  uint64  `json:"vram_total_bytes"`
	FreqMHz         float64 `json:"freq_mhz"`
	PowerWatts      float64 `json:"power_watts"`
	VramTempCelsius float64 `json:"vram_temp_celsius"`
	VramFreqMHz     float64 `json:"vram_freq_mhz"`
}

// TelemetryFlags represents supported telemetry features on the host machine.
type TelemetryFlags struct {
	CPUUsageSupported      bool `json:"cpu_usage_supported"`
	CPUCoresUsageSupported bool `json:"cpu_cores_usage_supported"`
	CPUTempSupported       bool `json:"cpu_temp_supported"`
	CPUFreqSupported       bool `json:"cpu_freq_supported"`
	CPUPowerSupported      bool `json:"cpu_power_supported"`
	RAMSupported           bool `json:"ram_supported"`
	SwapSupported          bool `json:"swap_supported"`
	ZRAMSupported          bool `json:"zram_supported"`
	GPUSupported           bool `json:"gpu_supported"`
	GPUUsageSupported      bool `json:"gpu_usage_supported"`
	GPUTempSupported       bool `json:"gpu_temp_supported"`
	GPUVramSupported       bool `json:"gpu_vram_supported"`
	GPUFreqSupported       bool `json:"gpu_freq_supported"`
	GPUPowerSupported      bool `json:"gpu_power_supported"`
	GPUVramTempSupported   bool `json:"gpu_vram_temp_supported"`
	GPUVramFreqSupported   bool `json:"gpu_vram_freq_supported"`
}

// SystemMetrics combines all gathered hardware statistics.
type SystemMetrics struct {
	CPU   CPUMetrics     `json:"cpu"`
	RAM   RAMMetrics     `json:"ram"`
	GPU   GPUMetrics     `json:"gpu"`
	Swap  SwapMetrics    `json:"swap"`
	ZRAM  ZRAMMetrics    `json:"zram"`
	Flags TelemetryFlags `json:"flags"`
}

// MetricsReader defines the contract for reading system performance telemetry.
type MetricsReader interface {
	// ReadCPU returns overall CPU usage percentages and core package temperatures.
	ReadCPU() (CPUMetrics, error)

	// ReadRAM returns physical host RAM stats (used, total, percentage).
	ReadRAM() (RAMMetrics, error)

	// ReadGPU returns graphics processor core usage, temperature, and VRAM utilization.
	ReadGPU() (GPUMetrics, error)

	// ReadSwap returns swap memory stats (used, total, percentage).
	ReadSwap() (SwapMetrics, error)

	// ReadZRAM returns compressed ZRAM stats.
	ReadZRAM() (ZRAMMetrics, error)

	// GetFlags returns the support status of system metrics.
	GetFlags() TelemetryFlags
}
