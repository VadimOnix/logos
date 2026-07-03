package agent

import (
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"
)

// Metrics is the heartbeat payload: cheap, dependency-free readings from
// /proc so the agent stays tiny (PRD §6 Footprint). Fields that cannot be
// read are simply omitted.
type Metrics struct {
	UptimeSec      float64 `json:"uptime_sec,omitempty"`
	Load1          float64 `json:"load1,omitempty"`
	Load5          float64 `json:"load5,omitempty"`
	Load15         float64 `json:"load15,omitempty"`
	MemTotalKB     uint64  `json:"mem_total_kb,omitempty"`
	MemAvailableKB uint64  `json:"mem_available_kb,omitempty"`
	RootFSTotalKB  uint64  `json:"rootfs_total_kb,omitempty"`
	RootFSFreeKB   uint64  `json:"rootfs_free_kb,omitempty"`
	Kernel         string  `json:"kernel,omitempty"`
}

func CollectMetrics() Metrics {
	var m Metrics

	if data, err := os.ReadFile("/proc/uptime"); err == nil {
		if f := strings.Fields(string(data)); len(f) > 0 {
			m.UptimeSec, _ = strconv.ParseFloat(f[0], 64)
		}
	}
	if data, err := os.ReadFile("/proc/loadavg"); err == nil {
		if f := strings.Fields(string(data)); len(f) >= 3 {
			m.Load1, _ = strconv.ParseFloat(f[0], 64)
			m.Load5, _ = strconv.ParseFloat(f[1], 64)
			m.Load15, _ = strconv.ParseFloat(f[2], 64)
		}
	}
	if data, err := os.ReadFile("/proc/meminfo"); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			f := strings.Fields(line)
			if len(f) < 2 {
				continue
			}
			switch f[0] {
			case "MemTotal:":
				m.MemTotalKB, _ = strconv.ParseUint(f[1], 10, 64)
			case "MemAvailable:":
				m.MemAvailableKB, _ = strconv.ParseUint(f[1], 10, 64)
			}
		}
	}
	var fs syscall.Statfs_t
	if err := syscall.Statfs("/", &fs); err == nil && fs.Bsize > 0 {
		m.RootFSTotalKB = fs.Blocks * uint64(fs.Bsize) / 1024
		m.RootFSFreeKB = fs.Bavail * uint64(fs.Bsize) / 1024
	}
	var uts syscall.Utsname
	if err := syscall.Uname(&uts); err == nil {
		m.Kernel = utsString(uts.Release)
	}
	return m
}

// OSVersion returns a human identifier for the node's OS: on OpenWrt the
// pretty name from /etc/os-release, otherwise GOOS.
func OSVersion() string {
	if data, err := os.ReadFile("/etc/os-release"); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if v, ok := strings.CutPrefix(line, "PRETTY_NAME="); ok {
				return strings.Trim(v, `"`)
			}
		}
	}
	return runtime.GOOS
}

func utsString(f [65]int8) string {
	b := make([]byte, 0, len(f))
	for _, c := range f {
		if c == 0 {
			break
		}
		b = append(b, byte(c))
	}
	return string(b)
}
