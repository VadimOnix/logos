package agent

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/VadimOnix/logos/agent/internal/pkgparse"
)

// Full-cleanup offboarding (PRD §4.4 level 2): restore the device to the
// pre-adoption snapshot taken by logos-adopt — remove packages installed
// since adoption and revert UCI configuration. The plan is shown and
// confirmed before anything is destroyed.

// preAdoptionSnapshot mirrors adopt.Snapshot (kept as a separate type so the
// agent binary does not link the ssh-bearing adopt package).
type preAdoptionSnapshot struct {
	PkgManager      string            `json:"pkg_manager"`
	Packages        []string          `json:"packages"`
	UCIExport       string            `json:"uci_export"`
	ConfigChecksums map[string]string `json:"config_checksums,omitempty"`
}

// snapshotFile lives next to the agent state (written by logos-adopt).
func snapshotFile(statePath string) string {
	return filepath.Join(filepath.Dir(statePath), "pre-adoption-snapshot.json")
}

// CleanupToSnapshot computes and applies the restore plan. Interactive
// confirmation unless yes; reads answers from in, reports to out.
func CleanupToSnapshot(ctx context.Context, statePath string, yes bool, in io.Reader, out io.Writer) error {
	data, err := os.ReadFile(snapshotFile(statePath))
	if os.IsNotExist(err) {
		return fmt.Errorf("no pre-adoption snapshot at %s — this device was not adopted with logos-adopt, nothing to clean up", snapshotFile(statePath))
	}
	if err != nil {
		return err
	}
	var snap preAdoptionSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return fmt.Errorf("corrupt snapshot: %w", err)
	}

	pm, err := detectPkgManager()
	if err != nil {
		return err
	}
	var listOut []byte
	switch pm.name {
	case "opkg":
		listOut, err = exec.CommandContext(ctx, pm.bin, "list-installed").Output()
	default:
		listOut, err = exec.CommandContext(ctx, pm.bin, "list", "--installed").Output()
	}
	if err != nil {
		return fmt.Errorf("%s list: %w", pm.name, err)
	}
	toRemove := diffPackages(pkgparse.Names(pm.name, string(listOut)), snap.Packages)

	fmt.Fprintln(out, "cleanup plan:")
	if len(toRemove) == 0 {
		fmt.Fprintln(out, "  - no packages were added since adoption")
	} else {
		fmt.Fprintf(out, "  - remove %d package(s) added since adoption: %s\n", len(toRemove), strings.Join(toRemove, ", "))
	}
	fmt.Fprintln(out, "  - revert UCI configuration to the pre-adoption snapshot")

	// File-level conflict detection: warn about /etc/config files that
	// diverged since adoption, so the operator knows the revert will
	// overwrite changes made after the device was adopted.
	if changed := changedConfigFiles(snap.ConfigChecksums); len(changed) > 0 {
		fmt.Fprintf(out, "  ! %d config file(s) changed since adoption and will be overwritten: %s\n",
			len(changed), strings.Join(changed, ", "))
	}
	if !yes {
		fmt.Fprint(out, "proceed? [y/N]: ")
		line, _ := bufio.NewReader(in).ReadString('\n')
		if answer := strings.ToLower(strings.TrimSpace(line)); answer != "y" && answer != "yes" {
			return fmt.Errorf("cleanup declined")
		}
	}

	// Remove packages first (needs working opkg state), then revert UCI.
	// Failures are reported per item and do not abort the rest — the PRD
	// calls for confirm/skip, not silent all-or-nothing destruction.
	var problems []string
	for _, name := range toRemove {
		verb := "remove"
		if pm.name == "apk" {
			verb = "del"
		}
		if outB, err := exec.CommandContext(ctx, pm.bin, verb, name).CombinedOutput(); err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v: %s", name, err, strings.TrimSpace(string(outB))))
		} else {
			fmt.Fprintf(out, "removed %s\n", name)
		}
	}

	uciBin, err := exec.LookPath("uci")
	if err != nil {
		return fmt.Errorf("uci not available; configuration not reverted")
	}
	tmp, err := os.CreateTemp("", "logos-snapshot-*.conf")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(snap.UCIExport); err != nil {
		tmp.Close()
		return err
	}
	tmp.Close()
	if outB, err := exec.CommandContext(ctx, uciBin, "-f", tmp.Name(), "import").CombinedOutput(); err != nil {
		return fmt.Errorf("uci import: %v: %s", err, outB)
	}
	if outB, err := exec.CommandContext(ctx, uciBin, "commit").CombinedOutput(); err != nil {
		return fmt.Errorf("uci commit: %v: %s", err, outB)
	}
	reloadServices()
	fmt.Fprintln(out, "UCI configuration reverted to the pre-adoption snapshot")

	os.Remove(snapshotFile(statePath))
	if len(problems) > 0 {
		return fmt.Errorf("cleanup finished with skipped items:\n  %s", strings.Join(problems, "\n  "))
	}
	return nil
}

// changedConfigFiles reports which snapshotted /etc/config files differ from
// their pre-adoption checksum now (sorted). Files that vanished are reported
// too; files unreadable now are skipped (nothing to overwrite).
func changedConfigFiles(baseline map[string]string) []string {
	var changed []string
	for path, want := range baseline {
		data, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			changed = append(changed, path+" (removed)")
			continue
		}
		if err != nil {
			continue
		}
		if fmt.Sprintf("%x", sha256.Sum256(data)) != want {
			changed = append(changed, path)
		}
	}
	slices.Sort(changed)
	return changed
}

// diffPackages returns names in current that are absent from baseline.
func diffPackages(current, baseline []string) []string {
	base := make(map[string]bool, len(baseline))
	for _, n := range baseline {
		base[n] = true
	}
	var added []string
	for _, n := range current {
		if !base[n] {
			added = append(added, n)
		}
	}
	slices.Sort(added)
	return slices.Compact(added)
}
