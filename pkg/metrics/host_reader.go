package metrics

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
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
	logger       *slog.Logger
	mu           sync.Mutex
	lastCPUTimes cpu.TimesStat
	hasCPUTimes  bool
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
	if err == nil && len(times) > 0 {
		curr := times[0]
		if r.hasCPUTimes {
			m.UsagePercent = calculateCPUUsage(r.lastCPUTimes, curr)
		}
		r.lastCPUTimes = curr
		r.hasCPUTimes = true
	}

	// Retrieve CPU temperature from sysfs / hwmon
	m.TempCelsius = readCPUTemperature()
	return m, nil
}

// ReadRAM retrieves physical RAM information.
func (r *HostMetricsReader) ReadRAM() (RAMMetrics, error) {
	var m RAMMetrics
	v, err := mem.VirtualMemory()
	if err != nil {
		return m, err
	}
	m.TotalBytes = v.Total
	m.UsedBytes = v.Used
	m.Percentage = v.UsedPercent
	return m, nil
}

// ReadGPU retrieves graphics processor metrics.
func (r *HostMetricsReader) ReadGPU() (GPUMetrics, error) {
	// Attempt to query NVIDIA via nvidia-smi first
	if isNvidiaAvailable() {
		m, err := readNvidiaGPU()
		if err == nil {
			return m, nil
		}
		r.logger.Warn("Failed to read NVIDIA GPU via nvidia-smi fallback", "error", err)
	}

	// Attempt to query AMD/Intel via sysfs
	m, err := readSysfsGPU()
	if err == nil {
		return m, nil
	}

	// Return graceful empty metrics if no GPU is detected or readable
	return GPUMetrics{}, nil
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
func readCPUTemperature() float64 {
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
					return tempVal / 1000.0
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
							return tempVal / 1000.0
						}
					}
				}
			}
		}
	}

	return 0.0
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
		"--query-gpu=temperature.gpu,utilization.gpu,memory.used,memory.total",
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

	return m, nil
}

// readSysfsGPU queries open-source AMD / Intel sysfs nodes.
func readSysfsGPU() (GPUMetrics, error) {
	var m GPUMetrics
	deviceDir := "/sys/class/drm/card0/device"
	if _, err := os.Stat(deviceDir); os.IsNotExist(err) {
		return m, err
	}

	// 1. GPU Busy percent
	busyBytes, err := os.ReadFile(filepath.Join(deviceDir, "gpu_busy_percent"))
	if err == nil {
		if val, err := strconv.ParseFloat(strings.TrimSpace(string(busyBytes)), 64); err == nil {
			m.UsagePercent = val
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
							break
						}
					}
				}
			}
		}
	}

	// 2. VRAM used bytes
	usedBytes, err := os.ReadFile(filepath.Join(deviceDir, "mem_info_vram_used"))
	if err == nil {
		if val, err := strconv.ParseUint(strings.TrimSpace(string(usedBytes)), 10, 64); err == nil {
			m.VramUsedBytes = val
		}
	}

	// 3. VRAM total bytes
	totalBytes, err := os.ReadFile(filepath.Join(deviceDir, "mem_info_vram_total"))
	if err == nil {
		if val, err := strconv.ParseUint(strings.TrimSpace(string(totalBytes)), 10, 64); err == nil {
			m.VramTotalBytes = val
		}
	}

	// 4. GPU Temperature
	hwmonGlob := filepath.Join(deviceDir, "hwmon", "hwmon*", "temp1_input")
	matches, err := filepath.Glob(hwmonGlob)
	if err == nil && len(matches) > 0 {
		tempBytes, err := os.ReadFile(matches[0])
		if err == nil {
			if val, err := strconv.ParseFloat(strings.TrimSpace(string(tempBytes)), 64); err == nil {
				m.TempCelsius = val / 1000.0
			}
		}
	}

	return m, nil
}
