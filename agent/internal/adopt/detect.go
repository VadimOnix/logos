package adopt

import (
	"fmt"
	"strconv"
	"strings"
)

// DeviceInfo is what the compatibility check learns about the router
// (PRD §4.1 step 4: verify arch, flash/RAM, OpenWrt version before touching
// anything).
type DeviceInfo struct {
	Hostname    string
	OSPretty    string
	IsOpenWrt   bool
	GoArch      string
	PkgManager  string // opkg | apk | ""
	HasProcd    bool
	MemTotalKB  uint64
	FlashFreeKB uint64
}

const (
	minMemKB       = 32 * 1024
	minFlashFreeKB = 6 * 1024 // stripped agent binary is ~4–5 MB
)

func Detect(r *Router) (*DeviceInfo, error) {
	info := &DeviceInfo{}

	if out, err := r.Run("cat /etc/os-release 2>/dev/null; true"); err == nil {
		for _, line := range strings.Split(out, "\n") {
			if v, ok := strings.CutPrefix(line, "PRETTY_NAME="); ok {
				info.OSPretty = strings.Trim(strings.TrimSpace(v), `"`)
			}
			if v, ok := strings.CutPrefix(line, "ID="); ok {
				info.IsOpenWrt = strings.Trim(strings.TrimSpace(v), `"`) == "openwrt"
			}
		}
	}
	if out, err := r.Run("uname -n"); err == nil {
		info.Hostname = strings.TrimSpace(out)
	}

	machine, err := r.Run("uname -m")
	if err != nil {
		return nil, fmt.Errorf("detect architecture: %w", err)
	}
	// MIPS needs the byte order, which uname does not reveal; read the ELF
	// endianness flag (byte 5) of /bin/sh.
	littleEndian := true
	if strings.HasPrefix(strings.TrimSpace(machine), "mips") {
		out, err := r.Run("od -An -j5 -N1 -tu1 /bin/sh")
		if err != nil {
			return nil, fmt.Errorf("detect endianness: %w", err)
		}
		littleEndian = strings.TrimSpace(out) == "1"
	}
	info.GoArch, err = goArchFor(strings.TrimSpace(machine), littleEndian)
	if err != nil {
		return nil, err
	}

	if out, err := r.Run("grep MemTotal /proc/meminfo"); err == nil {
		if f := strings.Fields(out); len(f) >= 2 {
			info.MemTotalKB, _ = strconv.ParseUint(f[1], 10, 64)
		}
	}
	// Free space where the binary lands: /overlay on OpenWrt, / elsewhere.
	if out, err := r.Run("df -k /overlay 2>/dev/null || df -k /"); err == nil {
		lines := strings.Split(strings.TrimSpace(out), "\n")
		if f := strings.Fields(lines[len(lines)-1]); len(f) >= 4 {
			info.FlashFreeKB, _ = strconv.ParseUint(f[3], 10, 64)
		}
	}

	if _, err := r.Run("command -v opkg"); err == nil {
		info.PkgManager = "opkg"
	} else if _, err := r.Run("command -v apk"); err == nil {
		info.PkgManager = "apk"
	}
	if _, err := r.Run("test -x /sbin/procd"); err == nil {
		info.HasProcd = true
	}
	return info, nil
}

// goArchFor maps `uname -m` output to a Go architecture.
func goArchFor(machine string, littleEndian bool) (string, error) {
	switch {
	case machine == "x86_64":
		return "amd64", nil
	case machine == "aarch64" || machine == "arm64":
		return "arm64", nil
	case strings.HasPrefix(machine, "armv") || machine == "arm":
		return "arm", nil
	case machine == "i386" || machine == "i486" || machine == "i586" || machine == "i686":
		return "386", nil
	case machine == "riscv64":
		return "riscv64", nil
	case strings.HasPrefix(machine, "mips64"):
		if littleEndian {
			return "mips64le", nil
		}
		return "mips64", nil
	case strings.HasPrefix(machine, "mips"):
		if littleEndian {
			return "mipsle", nil
		}
		return "mips", nil
	}
	return "", fmt.Errorf("unsupported architecture %q", machine)
}

// CheckCompatibility fails safely before anything is installed (PRD §4.1:
// incompatible device → clear message, nothing touched).
func (d *DeviceInfo) CheckCompatibility(force bool) error {
	var problems []string
	if !d.IsOpenWrt {
		problems = append(problems, fmt.Sprintf("device does not look like OpenWrt (%s)", d.OSPretty))
	}
	if d.PkgManager == "" {
		problems = append(problems, "no opkg/apk package manager found")
	}
	if d.MemTotalKB > 0 && d.MemTotalKB < minMemKB {
		problems = append(problems, fmt.Sprintf("only %d MB RAM (need ≥ %d MB)", d.MemTotalKB/1024, minMemKB/1024))
	}
	if d.FlashFreeKB > 0 && d.FlashFreeKB < minFlashFreeKB {
		problems = append(problems, fmt.Sprintf("only %d KB free flash (need ≥ %d KB for the agent)", d.FlashFreeKB, minFlashFreeKB))
	}
	if len(problems) == 0 {
		return nil
	}
	if force {
		return nil // operator explicitly accepted the risk
	}
	return fmt.Errorf("device failed the compatibility check:\n  - %s\n(use --force to adopt anyway)",
		strings.Join(problems, "\n  - "))
}
