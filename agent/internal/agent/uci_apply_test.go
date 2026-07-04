package agent

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// installStubUCI puts a fake `uci` on PATH that logs every invocation and
// answers `export` with canned content.
func installStubUCI(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	logPath := filepath.Join(dir, "uci.log")
	script := `#!/bin/sh
echo "$@" >> "$UCI_LOG"
case "$1" in
  export) echo "package $2"; echo "config stub 'section'" ;;
esac
exit 0
`
	if err := os.WriteFile(filepath.Join(dir, "uci"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("UCI_LOG", logPath)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return logPath
}

func uciLog(t *testing.T, logPath string) string {
	t.Helper()
	data, _ := os.ReadFile(logPath)
	return string(data)
}

func setupPending(t *testing.T) {
	t.Helper()
	SetStateDir(t.TempDir(), slog.New(slog.DiscardHandler))
	t.Cleanup(func() {
		pendingGuard.Lock()
		clearPendingLocked()
		pendingGuard.Unlock()
	})
}

func TestValidateChanges(t *testing.T) {
	configs, err := validateChanges([]uciChange{
		{Op: "set", Key: "network.lan.ipaddr", Value: "192.168.2.1"},
		{Op: "delete", Key: "firewall.@rule[3]"},
		{Op: "set", Key: "network.lan.netmask", Value: "255.255.255.0"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(configs) != 2 || configs[0] != "network" || configs[1] != "firewall" {
		t.Errorf("configs = %v", configs)
	}

	bad := [][]uciChange{
		{},
		{{Op: "set", Key: "no_dot", Value: "x"}},
		{{Op: "set", Key: "a.b;rm.c", Value: "x"}},
		{{Op: "exec", Key: "a.b.c"}},
		{{Op: "delete", Key: "a.b.c", Value: "unexpected"}},
		{{Op: "set", Key: "a.b.c", Value: "line1\nline2"}},
	}
	for i, changes := range bad {
		if _, err := validateChanges(changes); err == nil {
			t.Errorf("bad case %d accepted: %+v", i, changes)
		}
	}
}

func TestClampRevertTimeout(t *testing.T) {
	if d := clampRevertTimeout(0); d != defaultRevertTimeout {
		t.Errorf("zero → %v", d)
	}
	if d := clampRevertTimeout(1); d != minRevertTimeout {
		t.Errorf("tiny → %v", d)
	}
	if d := clampRevertTimeout(100000); d != maxRevertTimeout {
		t.Errorf("huge → %v", d)
	}
	if d := clampRevertTimeout(120); d != 120*time.Second {
		t.Errorf("in-range → %v", d)
	}
}

func TestApplyConfirmCycle(t *testing.T) {
	logPath := installStubUCI(t)
	setupPending(t)

	params, _ := json.Marshal(uciApplyParams{
		ApplyID:          "42",
		Changes:          []uciChange{{Op: "set", Key: "network.lan.ipaddr", Value: "10.0.0.1"}},
		RevertTimeoutSec: 60,
	})
	res, err := handleUCIApply(context.Background(), params)
	if err != nil {
		t.Fatal(err)
	}
	out := res.(uciApplyResult)
	if out.ApplyID != "42" || len(out.Configs) != 1 || out.Configs[0] != "network" {
		t.Errorf("result = %+v", out)
	}
	if !strings.Contains(out.Snapshots["network"], "package network") {
		t.Errorf("snapshot missing: %q", out.Snapshots["network"])
	}
	log := uciLog(t, logPath)
	for _, want := range []string{"export network", "set network.lan.ipaddr=10.0.0.1", "commit network"} {
		if !strings.Contains(log, want) {
			t.Errorf("uci log missing %q:\n%s", want, log)
		}
	}
	if PendingApplyID() != "42" {
		t.Errorf("PendingApplyID = %q, want 42", PendingApplyID())
	}

	// A second apply while one is pending must be rejected.
	if _, err := handleUCIApply(context.Background(), params); err == nil {
		t.Error("concurrent apply accepted while another is pending")
	}

	// Confirm clears the pending state.
	confirmParams, _ := json.Marshal(uciConfirmParams{ApplyID: "42"})
	if _, err := handleUCIConfirm(context.Background(), confirmParams); err != nil {
		t.Fatal(err)
	}
	if PendingApplyID() != "" {
		t.Error("pending state survived confirm")
	}
	// Confirming again must fail (single-shot).
	if _, err := handleUCIConfirm(context.Background(), confirmParams); err == nil {
		t.Error("double confirm accepted")
	}
}

func TestRevertPendingOnStart(t *testing.T) {
	logPath := installStubUCI(t)
	setupPending(t)

	// Simulate a crash after apply: pending file exists, agent restarts.
	pendingGuard.Lock()
	err := writePendingLocked(&pendingRevert{
		ApplyID:   "13",
		Deadline:  time.Now().Add(time.Minute),
		Snapshots: map[string]string{"network": "package network\nconfig old 'state'\n"},
	})
	pendingGuard.Unlock()
	if err != nil {
		t.Fatal(err)
	}

	if err := RevertPendingOnStart(); err != nil {
		t.Fatal(err)
	}
	log := uciLog(t, logPath)
	if !strings.Contains(log, "import network") || !strings.Contains(log, "commit network") {
		t.Errorf("revert did not import+commit:\n%s", log)
	}
	if PendingApplyID() != "" {
		t.Error("pending state survived revert")
	}
	// No pending file → no-op.
	if err := RevertPendingOnStart(); err != nil {
		t.Errorf("idempotent revert errored: %v", err)
	}
}

func TestApplyFailureRevertsImmediately(t *testing.T) {
	// Stub uci that fails on `set`.
	dir := t.TempDir()
	logPath := filepath.Join(dir, "uci.log")
	script := `#!/bin/sh
echo "$@" >> "$UCI_LOG"
case "$1" in
  export) echo "package $2" ;;
  set) echo "uci: Invalid argument" >&2; exit 1 ;;
esac
exit 0
`
	if err := os.WriteFile(filepath.Join(dir, "uci"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("UCI_LOG", logPath)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	setupPending(t)

	params, _ := json.Marshal(uciApplyParams{
		ApplyID: "7",
		Changes: []uciChange{{Op: "set", Key: "network.lan.ipaddr", Value: "10.0.0.1"}},
	})
	if _, err := handleUCIApply(context.Background(), params); err == nil {
		t.Fatal("failing apply reported success")
	}
	if PendingApplyID() != "" {
		t.Error("pending state left behind after failed apply")
	}
	if log := uciLog(t, logPath); !strings.Contains(log, "import network") {
		t.Errorf("failed apply did not restore snapshot:\n%s", log)
	}
}
