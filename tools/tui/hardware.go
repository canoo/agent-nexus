package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// --- GPU detection ---

type gpuInfo struct {
	Name     string
	MemoryMB int
	Platform string // "nvidia", "amd", "apple", "unknown"
}

func detectGPU() gpuInfo {
	// Linux: NVIDIA via nvidia-smi
	if out, err := exec.Command("nvidia-smi", "--query-gpu=name,memory.total",
		"--format=csv,noheader,nounits").Output(); err == nil {
		// Multi-GPU: take the first line only
		line := strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0])
		// Split by last ", " to handle GPU names containing commas
		if idx := strings.LastIndex(line, ", "); idx > 0 {
			mem := 0
			fmt.Sscanf(strings.TrimSpace(line[idx+2:]), "%d", &mem)
			return gpuInfo{Name: strings.TrimSpace(line[:idx]), MemoryMB: mem, Platform: "nvidia"}
		}
	}

	// macOS: Apple Silicon via system_profiler
	if out, err := exec.Command("system_profiler", "SPHardwareDataType").Output(); err == nil {
		text := string(out)
		// Intel Macs have "Processor Name:" instead of "Chip:" — skip them
		if strings.Contains(text, "Processor Name:") {
			return gpuInfo{Platform: "unknown"}
		}
		info := gpuInfo{Platform: "apple"}
		for _, line := range strings.Split(text, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "Chip:") {
				info.Name = strings.TrimSpace(strings.TrimPrefix(line, "Chip:"))
			}
			if strings.HasPrefix(line, "Memory:") {
				val := strings.TrimSpace(strings.TrimPrefix(line, "Memory:"))
				mem := 0
				if strings.Contains(val, "GB") {
					fmt.Sscanf(val, "%d", &mem)
					mem *= 1024
				}
				info.MemoryMB = mem
			}
		}
		if info.Name != "" {
			return info
		}
	}

	// Linux: AMD via sysfs
	matches, _ := filepath.Glob("/sys/class/drm/card*/device/mem_info_vram_total")
	if len(matches) > 0 {
		if data, err := os.ReadFile(matches[0]); err == nil {
			bytes := 0
			fmt.Sscanf(strings.TrimSpace(string(data)), "%d", &bytes)
			return gpuInfo{Name: "AMD GPU", MemoryMB: bytes / 1024 / 1024, Platform: "amd"}
		}
	}

	return gpuInfo{Platform: "unknown"}
}

func (g gpuInfo) String() string {
	if g.Platform == "unknown" {
		return "No GPU detected"
	}
	mem := g.MemoryMB / 1024
	unit := "GB"
	if g.Platform == "apple" {
		return fmt.Sprintf("%s — %d %s unified memory", g.Name, mem, unit)
	}
	return fmt.Sprintf("%s — %d %s VRAM", g.Name, mem, unit)
}

func (g gpuInfo) RecommendedModels() (supervisor, logic string) {
	mem := g.MemoryMB
	switch {
	case g.Platform == "apple" && mem <= 8*1024:
		return "qwen2.5-coder:3b", "llama3.2:3b"
	case mem <= 4*1024:
		return "qwen2.5-coder:1.5b", "llama3.2:3b"
	case mem <= 8*1024:
		return "qwen2.5-coder:7b", "llama3.1:8b"
	case mem <= 16*1024:
		return "qwen2.5-coder:7b", "qwen2.5:14b"
	case mem <= 18*1024:
		return "qwen2.5-coder:7b", "llama3.1:8b"
	case mem <= 24*1024:
		return "qwen2.5-coder:14b", "qwen2.5:32b"
	default:
		return "qwen2.5-coder:14b", "qwen2.5:32b"
	}
}
