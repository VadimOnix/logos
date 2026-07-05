package agent

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Overlay DNS (v1.0): the server sends a hosts list with each overlay spec
// and the agent materializes it as a file in /tmp/hosts — a directory
// OpenWrt's dnsmasq already reads additional hosts from — so overlay peers
// resolve by name (e.g. office.mesh.logos) without touching the dnsmasq
// config. Best-effort by design: name resolution failing must never break
// the tunnel itself.

// overlayHostsDir is a var so tests can point it at a temp dir.
var overlayHostsDir = "/tmp/hosts"

type overlayHost struct {
	Name string `json:"name"`
	IP   string `json:"ip"`
}

// writeOverlayHosts writes (or removes, when the list is empty) the hosts
// file for one overlay interface and pokes dnsmasq to re-read it.
func writeOverlayHosts(iface string, hosts []overlayHost) error {
	path := filepath.Join(overlayHostsDir, iface)
	if len(hosts) == 0 {
		if err := os.Remove(path); err != nil {
			if os.IsNotExist(err) {
				return nil // nothing was published; don't poke dnsmasq
			}
			return err
		}
		hupDnsmasq()
		return nil
	}
	var b strings.Builder
	b.WriteString("# managed by logos-agent: overlay peer names\n")
	for _, h := range hosts {
		fmt.Fprintf(&b, "%s %s\n", h.IP, h.Name)
	}
	if err := os.MkdirAll(overlayHostsDir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		return err
	}
	hupDnsmasq()
	return nil
}

// hupDnsmasq asks dnsmasq to re-read its hosts files. Best-effort: absent
// dnsmasq (non-router hosts) is not an error. Overridden in tests.
var hupDnsmasq = func() {
	exec.Command("killall", "-HUP", "dnsmasq").Run()
}
