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
	UptimeSec      float64      `json:"uptime_sec,omitempty"`
	Load1          float64      `json:"load1,omitempty"`
	Load5          float64      `json:"load5,omitempty"`
	Load15         float64      `json:"load15,omitempty"`
	MemTotalKB     uint64       `json:"mem_total_kb,omitempty"`
	MemAvailableKB uint64       `json:"mem_available_kb,omitempty"`
	RootFSTotalKB  uint64       `json:"rootfs_total_kb,omitempty"`
	RootFSFreeKB   uint64       `json:"rootfs_free_kb,omitempty"`
	Kernel         string       `json:"kernel,omitempty"`
	Interfaces     []IfaceStats `json:"interfaces,omitempty"`
	DHCPClients    []DHCPClient `json:"dhcp_clients,omitempty"`
	WifiClients    []WifiClient `json:"wifi_clients,omitempty"`
}

// IfaceStats are cumulative counters from /proc/net/dev (F6: traffic per
// interface). Rates are computed server/panel-side from successive samples.
type IfaceStats struct {
	Name      string `json:"name"`
	RxBytes   uint64 `json:"rx_bytes"`
	TxBytes   uint64 `json:"tx_bytes"`
	RxPackets uint64 `json:"rx_packets"`
	TxPackets uint64 `json:"tx_packets"`
}

// DHCPClient is one dnsmasq lease (F6: connected clients). Only present on
// nodes running dnsmasq (i.e. real OpenWrt routers).
type DHCPClient struct {
	MAC      string `json:"mac"`
	IP       string `json:"ip"`
	Hostname string `json:"hostname,omitempty"`
	Expires  int64  `json:"expires,omitempty"` // unix epoch
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
	m.Interfaces = readIfaceStats("/proc/net/dev")
	m.DHCPClients = readDHCPLeases("/tmp/dhcp.leases")
	m.WifiClients = collectWirelessClients()
	return m
}

// readIfaceStats parses /proc/net/dev; the loopback interface is skipped.
func readIfaceStats(path string) []IfaceStats {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var out []IfaceStats
	for _, line := range strings.Split(string(data), "\n") {
		name, rest, ok := strings.Cut(line, ":")
		if !ok {
			continue // header lines
		}
		name = strings.TrimSpace(name)
		if name == "" || name == "lo" {
			continue
		}
		f := strings.Fields(rest)
		if len(f) < 16 {
			continue
		}
		var s IfaceStats
		s.Name = name
		s.RxBytes, _ = strconv.ParseUint(f[0], 10, 64)
		s.RxPackets, _ = strconv.ParseUint(f[1], 10, 64)
		s.TxBytes, _ = strconv.ParseUint(f[8], 10, 64)
		s.TxPackets, _ = strconv.ParseUint(f[9], 10, 64)
		out = append(out, s)
	}
	return out
}

// readDHCPLeases parses the dnsmasq lease file:
// "<expiry-epoch> <mac> <ip> <hostname|*> <client-id|*>".
func readDHCPLeases(path string) []DHCPClient {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var out []DHCPClient
	for _, line := range strings.Split(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) < 4 {
			continue
		}
		c := DHCPClient{MAC: f[1], IP: f[2]}
		c.Expires, _ = strconv.ParseInt(f[0], 10, 64)
		if f[3] != "*" {
			c.Hostname = f[3]
		}
		out = append(out, c)
	}
	return out
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

// utsString converts a Utsname field to a string. The element type of
// syscall.Utsname arrays is platform-dependent (int8 on amd64/mips, uint8 on
// arm), hence the type parameter.
func utsString[T int8 | uint8](f [65]T) string {
	b := make([]byte, 0, len(f))
	for _, c := range f {
		if c == 0 {
			break
		}
		b = append(b, byte(c))
	}
	return string(b)
}
