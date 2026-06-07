package metrics

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/mem"
)

// HostMetricsReader reads telemetry from physical Linux hosts.
type HostMetricsReader struct {
	logger         *slog.Logger
	mu             sync.Mutex
	lastCPUTimes   cpu.TimesStat
	hasCPUTimes    bool
	lastEnergy     uint64
	lastEnergyTime time.Time
	hasEnergy      bool
	flags          TelemetryFlags
}

// NewHostMetricsReader instantiates a production MetricsReader.
func NewHostMetricsReader(logger *slog.Logger) *HostMetricsReader {
	r := &HostMetricsReader{
		logger: logger,
	}
	// Warm up CPU times
	if times, err := cpu.Times(false); err == nil && len(times) > 0 {
		r.lastCPUTimes = times[0]
		r.hasCPUTimes = true
	}
	return r
}

// ReadCPU retrieves host CPU utilization and temperature.
func (r *HostMetricsReader) ReadCPU() (CPUMetrics, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	var m CPUMetrics

	// Calculate CPU usage percent
	times, err := cpu.Times(false)
	r.flags.CPUUsageSupported = err == nil && len(times) > 0
	if r.flags.CPUUsageSupported {
		curr := times[0]
		if r.hasCPUTimes {
			m.UsagePercent = calculateCPUUsage(r.lastCPUTimes, curr)
		}
		r.lastCPUTimes = curr
		r.hasCPUTimes = true
	}

	// Retrieve CPU temperature from sysfs / hwmon
	var tempSupported bool
	m.TempCelsius, tempSupported = readCPUTemperature()
	r.flags.CPUTempSupported = tempSupported

	// Retrieve CPU frequency in MHz
	var freqSupported bool
	m.FreqMHz, freqSupported = readCPUFrequency()
	r.flags.CPUFreqSupported = freqSupported

	// Retrieve CPU power in Watts via RAPL energy_uj
	var power float64
	energyFile := "/sys/class/powercap/intel-rapl/intel-rapl:0/energy_uj"
	energyBytes, err := os.ReadFile(energyFile)
	if err == nil {
		r.flags.CPUPowerSupported = true
		energyVal, err := strconv.ParseUint(strings.TrimSpace(string(energyBytes)), 10, 64)
		if err == nil {
			now := time.Now()
			if r.hasEnergy && now.After(r.lastEnergyTime) {
				duration := now.Sub(r.lastEnergyTime).Seconds()
				if duration > 0 {
					if energyVal >= r.lastEnergy {
						diff := energyVal - r.lastEnergy
						power = float64(diff) / (duration * 1000000.0)
					}
				}
			}
			r.lastEnergy = energyVal
			r.lastEnergyTime = now
			r.hasEnergy = true
		}
	} else {
		r.flags.CPUPowerSupported = false
	}
	m.PowerWatts = math.Round(power*100) / 100

	return m, nil
}

// ReadRAM retrieves physical RAM information.
func (r *HostMetricsReader) ReadRAM() (RAMMetrics, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	var m RAMMetrics
	v, err := mem.VirtualMemory()
	r.flags.RAMSupported = err == nil
	if err != nil {
		return m, err
	}
	m.TotalBytes = v.Total
	m.UsedBytes = v.Used
	m.Percentage = v.UsedPercent
	return m, nil
}

// ReadSwap retrieves swap memory information.
func (r *HostMetricsReader) ReadSwap() (SwapMetrics, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	var m SwapMetrics
	v, err := mem.SwapMemory()
	r.flags.SwapSupported = err == nil
	if err != nil {
		return m, err
	}
	m.TotalBytes = v.Total
	m.UsedBytes = v.Used
	m.Percentage = v.UsedPercent
	return m, nil
}

// ReadZRAM retrieves compressed ZRAM information.
func (r *HostMetricsReader) ReadZRAM() (ZRAMMetrics, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	var m ZRAMMetrics
	// Glob for zram block devices
	matches, err := filepath.Glob("/sys/block/zram*")
	if err != nil || len(matches) == 0 {
		r.flags.ZRAMSupported = false
		return m, nil
	}

	var totalDiskSize uint64
	var totalOrigDataSize uint64
	var totalComprDataSize uint64
	var totalMemUsed uint64
	hasAnyZram := false

	for _, dir := range matches {
		// Read disksize
		disksizeBytes, err := os.ReadFile(filepath.Join(dir, "disksize"))
		if err != nil {
			continue
		}
		disksize, err := strconv.ParseUint(strings.TrimSpace(string(disksizeBytes)), 10, 64)
		if err != nil {
			continue
		}

		// Read mm_stat
		mmStatBytes, err := os.ReadFile(filepath.Join(dir, "mm_stat"))
		if err != nil {
			continue
		}
		fields := strings.Fields(strings.TrimSpace(string(mmStatBytes)))
		if len(fields) < 3 {
			continue
		}

		origDataSize, err := strconv.ParseUint(fields[0], 10, 64)
		if err != nil {
			continue
		}
		comprDataSize, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			continue
		}
		memUsed, err := strconv.ParseUint(fields[2], 10, 64)
		if err != nil {
			continue
		}

		totalDiskSize += disksize
		totalOrigDataSize += origDataSize
		totalComprDataSize += comprDataSize
		totalMemUsed += memUsed
		hasAnyZram = true
	}

	r.flags.ZRAMSupported = hasAnyZram
	if !hasAnyZram {
		return m, nil
	}

	m.TotalBytes = totalDiskSize
	m.OrigDataSizeBytes = totalOrigDataSize
	m.ComprDataSizeBytes = totalComprDataSize
	m.MemUsedTotalBytes = totalMemUsed

	if totalComprDataSize > 0 {
		ratio := float64(totalOrigDataSize) / float64(totalComprDataSize)
		m.CompressionRatio = math.Round(ratio*100) / 100
	} else {
		m.CompressionRatio = 0.0
	}

	return m, nil
}

// ReadGPU retrieves graphics processor metrics.
func (r *HostMetricsReader) ReadGPU() (GPUMetrics, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Attempt to query NVIDIA via nvidia-smi first
	if isNvidiaAvailable() {
		m, err := readNvidiaGPU()
		if err == nil {
			r.flags.GPUSupported = true
			r.flags.GPUUsageSupported = true
			r.flags.GPUTempSupported = true
			r.flags.GPUVramSupported = true
			r.flags.GPUFreqSupported = true
			r.flags.GPUPowerSupported = true
			r.flags.GPUVramTempSupported = false
			r.flags.GPUVramFreqSupported = true
			return m, nil
		}
		r.logger.Warn("Failed to read NVIDIA GPU via nvidia-smi fallback", "error", err)
	}

	// Attempt to query AMD/Intel via sysfs
	m, err, flags := readSysfsGPU()
	if err == nil {
		r.flags.GPUSupported = true
		r.flags.GPUUsageSupported = flags.GPUUsageSupported
		r.flags.GPUTempSupported = flags.GPUTempSupported
		r.flags.GPUVramSupported = flags.GPUVramSupported
		r.flags.GPUFreqSupported = flags.GPUFreqSupported
		r.flags.GPUPowerSupported = flags.GPUPowerSupported
		r.flags.GPUVramTempSupported = flags.GPUVramTempSupported
		r.flags.GPUVramFreqSupported = flags.GPUVramFreqSupported
		return m, nil
	}

	// Return graceful empty metrics if no GPU is detected or readable
	r.flags.GPUSupported = false
	r.flags.GPUUsageSupported = false
	r.flags.GPUTempSupported = false
	r.flags.GPUVramSupported = false
	r.flags.GPUFreqSupported = false
	r.flags.GPUPowerSupported = false
	r.flags.GPUVramTempSupported = false
	r.flags.GPUVramFreqSupported = false
	return GPUMetrics{}, nil
}

// GetFlags returns the support status of system metrics.
func (r *HostMetricsReader) GetFlags() TelemetryFlags {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.flags
}

// calculateCPUUsage computes CPU busy time ratio over delta interval.
func calculateCPUUsage(last, current cpu.TimesStat) float64 {
	lastBusy := last.User + last.System + last.Nice + last.Irq + last.Softirq + last.Steal
	lastIdle := last.Idle + last.Iowait
	lastTotal := lastBusy + lastIdle

	currBusy := current.User + current.System + current.Nice + current.Irq + current.Softirq + current.Steal
	currIdle := current.Idle + current.Iowait
	currTotal := currBusy + currIdle

	totalDiff := currTotal - lastTotal
	if totalDiff <= 0 {
		return 0
	}
	busyDiff := currBusy - lastBusy
	return (busyDiff / totalDiff) * 100.0
}

// readCPUTemperature tries to discover and read CPU package temperature.
func readCPUTemperature() (float64, bool) {
	// 1. Primary Route: Thermal Zones
	zones, err := filepath.Glob("/sys/class/thermal/thermal_zone*")
	if err == nil {
		for _, zone := range zones {
			typeBytes, err := os.ReadFile(filepath.Join(zone, "type"))
			if err != nil {
				continue
			}
			zoneType := strings.TrimSpace(string(typeBytes))
			if strings.Contains(zoneType, "x86_pkg_temp") ||
				strings.Contains(zoneType, "cpu-thermal") ||
				strings.Contains(zoneType, "coretemp") {

				tempBytes, err := os.ReadFile(filepath.Join(zone, "temp"))
				if err != nil {
					continue
				}
				if tempVal, err := strconv.ParseFloat(strings.TrimSpace(string(tempBytes)), 64); err == nil {
					return tempVal / 1000.0, true
				}
			}
		}
	}

	// 2. Secondary Route: Hwmon Sensors
	hwmons, err := filepath.Glob("/sys/class/hwmon/hwmon*")
	if err == nil {
		for _, hw := range hwmons {
			inputs, err := filepath.Glob(filepath.Join(hw, "temp*_input"))
			if err != nil {
				continue
			}
			for _, input := range inputs {
				// Match label file if exists
				labelPath := strings.Replace(input, "_input", "_label", 1)
				if _, err := os.Stat(labelPath); err == nil {
					lblBytes, err := os.ReadFile(labelPath)
					if err != nil {
						continue
					}
					lbl := strings.TrimSpace(string(lblBytes))
					if strings.Contains(lbl, "Package") || strings.Contains(lbl, "Core") {
						tempBytes, err := os.ReadFile(input)
						if err != nil {
							continue
						}
						if tempVal, err := strconv.ParseFloat(strings.TrimSpace(string(tempBytes)), 64); err == nil {
							return tempVal / 1000.0, true
						}
					}
				}
			}
		}
	}

	return 0.0, false
}

// readCPUFrequency reads active clock speeds across cores in MHz.
func readCPUFrequency() (float64, bool) {
	// 1. Primary Route: scaling_cur_freq across all cores
	files, err := filepath.Glob("/sys/devices/system/cpu/cpu*/cpufreq/scaling_cur_freq")
	if err == nil && len(files) > 0 {
		var totalFreq float64
		var count int
		for _, file := range files {
			freqBytes, err := os.ReadFile(file)
			if err != nil {
				continue
			}
			freqKHz, err := strconv.ParseFloat(strings.TrimSpace(string(freqBytes)), 64)
			if err == nil {
				totalFreq += freqKHz / 1000.0
				count++
			}
		}
		if count > 0 {
			return totalFreq / float64(count), true
		}
	}

	// 2. Secondary Route: Fallback to gopsutil cpu.Info()
	info, err := cpu.Info()
	if err == nil && len(info) > 0 {
		var totalFreq float64
		var count int
		for _, stat := range info {
			if stat.Mhz > 0 {
				totalFreq += stat.Mhz
				count++
			}
		}
		if count > 0 {
			return totalFreq / float64(count), true
		}
	}

	return 0.0, false
}

// isNvidiaAvailable checks if nvidia-smi executable exists.
func isNvidiaAvailable() bool {
	_, err := exec.LookPath("nvidia-smi")
	return err == nil
}

// readNvidiaGPU invokes nvidia-smi with rigid hardcoded queries.
func readNvidiaGPU() (GPUMetrics, error) {
	var m GPUMetrics
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "nvidia-smi",
		"--query-gpu=temperature.gpu,utilization.gpu,memory.used,memory.total,clocks.current.graphics,power.draw,clocks.current.memory",
		"--format=csv,noheader,nounits")

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return m, fmt.Errorf("nvidia-smi execution failed: %w (stderr: %s)", err, stderr.String())
	}

	parts := strings.Split(strings.TrimSpace(stdout.String()), ",")
	if len(parts) < 4 {
		return m, fmt.Errorf("unexpected nvidia-smi output format: %s", stdout.String())
	}

	temp, err := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	if err == nil {
		m.TempCelsius = temp
	}

	util, err := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	if err == nil {
		m.UsagePercent = util
	}

	// VRAM is reported in MiB by nvidia-smi, convert to bytes.
	vramUsedMiB, err := strconv.ParseUint(strings.TrimSpace(parts[2]), 10, 64)
	if err == nil {
		m.VramUsedBytes = vramUsedMiB * 1024 * 1024
	}

	vramTotalMiB, err := strconv.ParseUint(strings.TrimSpace(parts[3]), 10, 64)
	if err == nil {
		m.VramTotalBytes = vramTotalMiB * 1024 * 1024
	}

	if len(parts) >= 5 {
		freq, err := strconv.ParseFloat(strings.TrimSpace(parts[4]), 64)
		if err == nil {
			m.FreqMHz = freq
		}
	}

	if len(parts) >= 6 {
		power, err := strconv.ParseFloat(strings.TrimSpace(parts[5]), 64)
		if err == nil {
			m.PowerWatts = power
		}
	}

	if len(parts) >= 7 {
		memFreq, err := strconv.ParseFloat(strings.TrimSpace(parts[6]), 64)
		if err == nil {
			m.VramFreqMHz = memFreq
		}
	}

	m.VramTempCelsius = 0.0

	return m, nil
}

// readSysfsGPU queries open-source AMD / Intel sysfs nodes.
func readSysfsGPU() (GPUMetrics, error, TelemetryFlags) {
	var m GPUMetrics
	var flags TelemetryFlags
	deviceDir := "/sys/class/drm/card0/device"
	if _, err := os.Stat(deviceDir); os.IsNotExist(err) {
		return m, err, flags
	}

	// 1. GPU Busy percent
	busyBytes, err := os.ReadFile(filepath.Join(deviceDir, "gpu_busy_percent"))
	if err == nil {
		if val, err := strconv.ParseFloat(strings.TrimSpace(string(busyBytes)), 64); err == nil {
			m.UsagePercent = val
			flags.GPUUsageSupported = true
		}
	} else {
		// Fallback pm_info check
		pmBytes, err := os.ReadFile(filepath.Join(deviceDir, "pm_info"))
		if err == nil {
			lines := strings.Split(string(pmBytes), "\n")
			for _, line := range lines {
				if strings.Contains(line, "GPU Load") {
					parts := strings.Split(line, ":")
					if len(parts) > 1 {
						cleanStr := strings.TrimSpace(strings.ReplaceAll(parts[1], "%", ""))
						if val, err := strconv.ParseFloat(cleanStr, 64); err == nil {
							m.UsagePercent = val
							flags.GPUUsageSupported = true
							break
						}
					}
				}
			}
		}
	}

	// 2. VRAM used bytes
	usedBytes, err := os.ReadFile(filepath.Join(deviceDir, "mem_info_vram_used"))
	usedOk := err == nil
	if usedOk {
		if val, err := strconv.ParseUint(strings.TrimSpace(string(usedBytes)), 10, 64); err == nil {
			m.VramUsedBytes = val
		} else {
			usedOk = false
		}
	}

	// 3. VRAM total bytes
	totalBytes, err := os.ReadFile(filepath.Join(deviceDir, "mem_info_vram_total"))
	totalOk := err == nil
	if totalOk {
		if val, err := strconv.ParseUint(strings.TrimSpace(string(totalBytes)), 10, 64); err == nil {
			m.VramTotalBytes = val
		} else {
			totalOk = false
		}
	}
	flags.GPUVramSupported = usedOk && totalOk

	// 4. GPU Temperature
	hwmonGlob := filepath.Join(deviceDir, "hwmon", "hwmon*", "temp1_input")
	matches, err := filepath.Glob(hwmonGlob)
	if err == nil && len(matches) > 0 {
		tempBytes, err := os.ReadFile(matches[0])
		if err == nil {
			if val, err := strconv.ParseFloat(strings.TrimSpace(string(tempBytes)), 64); err == nil {
				m.TempCelsius = val / 1000.0
				flags.GPUTempSupported = true
			}
		}
	}

	// 4.5. GPU Power Draw
	powerGlob := filepath.Join(deviceDir, "hwmon", "hwmon*", "power1_average")
	if powerMatches, err := filepath.Glob(powerGlob); err == nil && len(powerMatches) > 0 {
		if powerBytes, err := os.ReadFile(powerMatches[0]); err == nil {
			if val, err := strconv.ParseFloat(strings.TrimSpace(string(powerBytes)), 64); err == nil {
				m.PowerWatts = math.Round((val/1000000.0)*100) / 100
				flags.GPUPowerSupported = true
			}
		}
	} else {
		// Fallback to power1_input
		powerGlobInput := filepath.Join(deviceDir, "hwmon", "hwmon*", "power1_input")
		if powerMatchesInput, err := filepath.Glob(powerGlobInput); err == nil && len(powerMatchesInput) > 0 {
			if powerBytes, err := os.ReadFile(powerMatchesInput[0]); err == nil {
				if val, err := strconv.ParseFloat(strings.TrimSpace(string(powerBytes)), 64); err == nil {
					m.PowerWatts = math.Round((val/1000000.0)*100) / 100
					flags.GPUPowerSupported = true
				}
			}
		}
	}
	// 5. GPU Frequency
	var freqSupported bool
	m.FreqMHz, freqSupported = readSysfsGPUFrequency()
	flags.GPUFreqSupported = freqSupported

	// 6. VRAM Temperature
	var vramTempSupported bool
	m.VramTempCelsius, vramTempSupported = readSysfsVRAMTemperature(deviceDir)
	flags.GPUVramTempSupported = vramTempSupported

	// 7. VRAM Frequency
	var vramFreqSupported bool
	m.VramFreqMHz, vramFreqSupported = readSysfsVRAMFrequency(deviceDir)
	flags.GPUVramFreqSupported = vramFreqSupported

	return m, nil, flags
}

// readSysfsGPUFrequency reads graphics engine active clock frequency in MHz in order of preference.
func readSysfsGPUFrequency() (float64, bool) {
	// 1. Intel Active Frequency: /sys/class/drm/card*/device/gt_act_freq_mhz or /sys/class/drm/card*/gt_act_freq_mhz
	intelPaths := []string{
		"/sys/class/drm/card*/device/gt_act_freq_mhz",
		"/sys/class/drm/card*/gt_act_freq_mhz",
	}
	for _, pattern := range intelPaths {
		files, err := filepath.Glob(pattern)
		if err == nil && len(files) > 0 {
			for _, file := range files {
				freqBytes, err := os.ReadFile(file)
				if err != nil {
					continue
				}
				if val, err := strconv.ParseFloat(strings.TrimSpace(string(freqBytes)), 64); err == nil {
					return val, true
				}
			}
		}
	}

	// 2. AMD DPM System Clock: /sys/class/drm/card*/device/pp_dpm_sclk
	amdFiles, err := filepath.Glob("/sys/class/drm/card*/device/pp_dpm_sclk")
	if err == nil && len(amdFiles) > 0 {
		for _, file := range amdFiles {
			sclkBytes, err := os.ReadFile(file)
			if err != nil {
				continue
			}
			lines := strings.Split(string(sclkBytes), "\n")
			for _, line := range lines {
				if strings.Contains(line, "*") {
					s := strings.ToLower(line)
					s = strings.ReplaceAll(s, "*", "")
					s = strings.ReplaceAll(s, "mhz", "")
					fields := strings.Fields(s)
					for _, f := range fields {
						f = strings.Trim(f, ": \t")
						if val, err := strconv.ParseFloat(f, 64); err == nil {
							return val, true
						}
					}
				}
			}
		}
	}

	// 3. Hwmon Frequency Input: /sys/class/drm/card*/device/hwmon/hwmon*/freq1_input (Hz to MHz)
	hwmonFiles, err := filepath.Glob("/sys/class/drm/card*/device/hwmon/hwmon*/freq1_input")
	if err == nil && len(hwmonFiles) > 0 {
		for _, file := range hwmonFiles {
			freqBytes, err := os.ReadFile(file)
			if err != nil {
				continue
			}
			if val, err := strconv.ParseFloat(strings.TrimSpace(string(freqBytes)), 64); err == nil {
				return val / 1000000.0, true
			}
		}
	}

	return 0.0, false
}

// readSysfsVRAMTemperature reads VRAM/memory temperature from sysfs / hwmon.
func readSysfsVRAMTemperature(deviceDir string) (float64, bool) {
	hwmonGlob := filepath.Join(deviceDir, "hwmon", "hwmon*", "temp*_input")
	matches, err := filepath.Glob(hwmonGlob)
	if err != nil || len(matches) == 0 {
		return 0.0, false
	}

	// 1. Check labels for "mem", "vram", "junction"
	for _, match := range matches {
		labelPath := strings.Replace(match, "_input", "_label", 1)
		if _, err := os.Stat(labelPath); err == nil {
			lblBytes, err := os.ReadFile(labelPath)
			if err == nil {
				lbl := strings.ToLower(strings.TrimSpace(string(lblBytes)))
				if strings.Contains(lbl, "mem") || strings.Contains(lbl, "vram") || strings.Contains(lbl, "junction") {
					tempBytes, err := os.ReadFile(match)
					if err == nil {
						if val, err := strconv.ParseFloat(strings.TrimSpace(string(tempBytes)), 64); err == nil {
							return val / 1000.0, true
						}
					}
				}
			}
		}
	}

	// 2. Fallback to temp2_input or temp3_input
	fallbackPaths := []string{
		filepath.Join(filepath.Dir(matches[0]), "temp2_input"),
		filepath.Join(filepath.Dir(matches[0]), "temp3_input"),
	}
	for _, path := range fallbackPaths {
		if _, err := os.Stat(path); err == nil {
			tempBytes, err := os.ReadFile(path)
			if err == nil {
				if val, err := strconv.ParseFloat(strings.TrimSpace(string(tempBytes)), 64); err == nil {
					return val / 1000.0, true
				}
			}
		}
	}

	return 0.0, false
}

// readSysfsVRAMFrequency reads VRAM/memory clock frequency in MHz.
func readSysfsVRAMFrequency(deviceDir string) (float64, bool) {
	// 1. AMD DPM Memory Clock: /sys/class/drm/card*/device/pp_dpm_mclk
	mclkPath := filepath.Join(deviceDir, "pp_dpm_mclk")
	if _, err := os.Stat(mclkPath); err == nil {
		sclkBytes, err := os.ReadFile(mclkPath)
		if err == nil {
			lines := strings.Split(string(sclkBytes), "\n")
			for _, line := range lines {
				if strings.Contains(line, "*") {
					s := strings.ToLower(line)
					s = strings.ReplaceAll(s, "*", "")
					s = strings.ReplaceAll(s, "mhz", "")
					fields := strings.Fields(s)
					for _, f := range fields {
						f = strings.Trim(f, ": \t")
						if val, err := strconv.ParseFloat(f, 64); err == nil {
							return val, true
						}
					}
				}
			}
		}
	}

	// 2. Hwmon Frequency Input: /sys/class/drm/card*/device/hwmon/hwmon*/freq2_input (Hz to MHz)
	hwmonGlob := filepath.Join(deviceDir, "hwmon", "hwmon*", "freq2_input")
	hwmonFiles, err := filepath.Glob(hwmonGlob)
	if err == nil && len(hwmonFiles) > 0 {
		for _, file := range hwmonFiles {
			freqBytes, err := os.ReadFile(file)
			if err != nil {
				continue
			}
			if val, err := strconv.ParseFloat(strings.TrimSpace(string(freqBytes)), 64); err == nil {
				return val / 1000000.0, true
			}
		}
	}

	return 0.0, false
}
