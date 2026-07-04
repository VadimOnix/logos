package adopt

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/VadimOnix/logos/agent/internal/pkgparse"
)

// SnapshotPath is where the pre-adoption snapshot lives on the router. The
// agent's `leave --cleanup` (PRD §4.4 level 2) reads it to restore the
// device to its pre-adoption state.
const SnapshotPath = "/etc/logos/pre-adoption-snapshot.json"

// Snapshot captures the device state before anything is installed
// (PRD §4.1 step 4: installed package list + uci export). ConfigChecksums
// records the sha256 of each /etc/config file so cleanup can tell which
// configuration actually diverged since adoption (file-level conflict
// detection) instead of blindly overwriting operator edits.
type Snapshot struct {
	TakenAt         time.Time         `json:"taken_at"`
	PkgManager      string            `json:"pkg_manager"`
	Packages        []string          `json:"packages"` // package names only
	UCIExport       string            `json:"uci_export"`
	ConfigChecksums map[string]string `json:"config_checksums,omitempty"` // /etc/config/<name> → sha256 hex
}

func TakeSnapshot(r *Router, pkgManager string) (*Snapshot, error) {
	snap := &Snapshot{TakenAt: time.Now().UTC(), PkgManager: pkgManager}

	var listCmd string
	switch pkgManager {
	case "opkg":
		listCmd = "opkg list-installed"
	case "apk":
		listCmd = "apk list --installed 2>/dev/null"
	default:
		return nil, fmt.Errorf("unsupported package manager %q", pkgManager)
	}
	out, err := r.Run(listCmd)
	if err != nil {
		return nil, fmt.Errorf("snapshot package list: %w", err)
	}
	snap.Packages = pkgparse.Names(pkgManager, out)

	uciOut, err := r.Run("uci export")
	if err != nil {
		return nil, fmt.Errorf("snapshot uci export: %w", err)
	}
	snap.UCIExport = uciOut

	// Per-file checksums (best-effort: skipped if the device lacks
	// sha256sum, which does not affect package/uci restore).
	if out, err := r.Run("sha256sum /etc/config/* 2>/dev/null"); err == nil {
		snap.ConfigChecksums = parseChecksums(out)
	}
	return snap, nil
}

// parseChecksums parses `sha256sum` output ("<hex>  <path>" per line).
func parseChecksums(out string) map[string]string {
	sums := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && len(fields[0]) == 64 {
			sums[fields[1]] = fields[0]
		}
	}
	if len(sums) == 0 {
		return nil
	}
	return sums
}

func (s *Snapshot) JSON() ([]byte, error) {
	return json.MarshalIndent(s, "", "  ")
}
