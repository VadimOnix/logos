package adopt

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/VadimOnix/logos/agent/internal/pkgparse"
)

// SnapshotPath is where the pre-adoption snapshot lives on the router. The
// agent's `leave --cleanup` (PRD §4.4 level 2) reads it to restore the
// device to its pre-adoption state.
const SnapshotPath = "/etc/logos/pre-adoption-snapshot.json"

// Snapshot captures the device state before anything is installed
// (PRD §4.1 step 4: installed package list + uci export).
type Snapshot struct {
	TakenAt    time.Time `json:"taken_at"`
	PkgManager string    `json:"pkg_manager"`
	Packages   []string  `json:"packages"` // package names only
	UCIExport  string    `json:"uci_export"`
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
	return snap, nil
}

func (s *Snapshot) JSON() ([]byte, error) {
	return json.MarshalIndent(s, "", "  ")
}
