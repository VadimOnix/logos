package agent

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestDiffPackages(t *testing.T) {
	got := diffPackages(
		[]string{"base", "logos-dep", "htop", "base"},
		[]string{"base"},
	)
	if !slices.Equal(got, []string{"htop", "logos-dep"}) {
		t.Errorf("got %v", got)
	}
	if got := diffPackages([]string{"a"}, []string{"a"}); len(got) != 0 {
		t.Errorf("no-diff case: %v", got)
	}
}

// TestCleanupToSnapshot drives the full cleanup against stub opkg/uci: the
// package added after adoption is removed and the uci snapshot re-imported.
func TestCleanupToSnapshot(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "calls.log")
	stub := `#!/bin/sh
echo "$(basename "$0") $@" >> "$UCI_LOG"
case "$(basename "$0")" in
  opkg) [ "$1" = list-installed ] && printf 'base - 1.0\nhtop - 3.2\n' ;;
esac
exit 0
`
	for _, name := range []string{"opkg", "uci"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(stub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("UCI_LOG", logPath)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	stateDir := t.TempDir()
	statePath := filepath.Join(stateDir, "agent.json")
	snap, _ := json.Marshal(map[string]any{
		"pkg_manager": "opkg",
		"packages":    []string{"base"},
		"uci_export":  "package network\nconfig interface 'lan'\n",
	})
	if err := os.WriteFile(snapshotFile(statePath), snap, 0o600); err != nil {
		t.Fatal(err)
	}

	var out strings.Builder
	if err := CleanupToSnapshot(context.Background(), statePath, true, strings.NewReader(""), &out); err != nil {
		t.Fatalf("cleanup: %v (output: %s)", err, out.String())
	}
	log, _ := os.ReadFile(logPath)
	for _, want := range []string{"opkg remove htop", "uci -f", "import", "uci commit"} {
		if !strings.Contains(string(log), want) {
			t.Errorf("call log missing %q:\n%s", want, log)
		}
	}
	if strings.Contains(string(log), "remove base") {
		t.Error("cleanup removed a package that predates adoption")
	}
	if _, err := os.Stat(snapshotFile(statePath)); !os.IsNotExist(err) {
		t.Error("snapshot file survived cleanup")
	}
}

func TestCleanupDeclined(t *testing.T) {
	stateDir := t.TempDir()
	statePath := filepath.Join(stateDir, "agent.json")
	snap, _ := json.Marshal(map[string]any{"pkg_manager": "opkg", "packages": []string{}, "uci_export": "x"})
	os.WriteFile(snapshotFile(statePath), snap, 0o600)

	// Needs a pkg manager on PATH to reach the prompt; reuse system PATH if
	// opkg/apk absent → the error must be about the manager, not a crash.
	var out strings.Builder
	err := CleanupToSnapshot(context.Background(), statePath, false, strings.NewReader("n\n"), &out)
	if err == nil {
		t.Fatal("declined cleanup reported success")
	}
}

func TestCleanupWithoutSnapshot(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "agent.json")
	err := CleanupToSnapshot(context.Background(), statePath, true, strings.NewReader(""), &strings.Builder{})
	if err == nil || !strings.Contains(err.Error(), "no pre-adoption snapshot") {
		t.Errorf("err = %v", err)
	}
}

func sha256hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return fmt.Sprintf("%x", sum)
}

func TestChangedConfigFiles(t *testing.T) {
	dir := t.TempDir()
	same := filepath.Join(dir, "network")
	os.WriteFile(same, []byte("config interface\n"), 0o644)
	edited := filepath.Join(dir, "firewall")
	os.WriteFile(edited, []byte("edited after adoption\n"), 0o644)

	baseline := map[string]string{
		same:                       sha256hex("config interface\n"),  // unchanged
		edited:                     sha256hex("original firewall\n"), // differs now
		filepath.Join(dir, "gone"): sha256hex("was here\n"),          // missing now
	}
	changed := changedConfigFiles(baseline)
	if len(changed) != 2 {
		t.Fatalf("want 2 changed, got %v", changed)
	}
	if !slices.Contains(changed, edited) {
		t.Errorf("edited file not flagged: %v", changed)
	}
	foundGone := false
	for _, c := range changed {
		if strings.HasSuffix(c, "gone (removed)") {
			foundGone = true
		}
	}
	if !foundGone {
		t.Errorf("removed file not flagged: %v", changed)
	}
	if changedConfigFiles(nil) != nil {
		t.Error("nil baseline should yield no changes")
	}
}
