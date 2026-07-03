package agent

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestStateRoundtrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "agent.json")
	st := &State{
		ServerURL:  "https://logos.example.com",
		NodeID:     "0d9e4f3a-0000-4000-8000-000000000001",
		NodeToken:  "tok",
		PrivateKey: "aa",
	}
	if err := SaveState(path, st); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Errorf("state file mode = %o, want 600 (contains the node token)", perm)
		}
	}
	got, err := LoadState(path)
	if err != nil {
		t.Fatal(err)
	}
	if *got != *st {
		t.Errorf("LoadState = %+v, want %+v", got, st)
	}

	if err := WipeState(path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("state file still exists after wipe")
	}
	if err := WipeState(path); err != nil {
		t.Errorf("wiping a missing state errored: %v", err)
	}
}

func TestLoadStateIncomplete(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.json")
	if err := os.WriteFile(path, []byte(`{"server_url":"https://x"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadState(path); err == nil {
		t.Error("incomplete state loaded without error")
	}
}

func TestCollectMetrics(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux-only")
	}
	m := CollectMetrics()
	if m.UptimeSec <= 0 {
		t.Error("uptime not collected")
	}
	if m.MemTotalKB == 0 {
		t.Error("meminfo not collected")
	}
	if m.Kernel == "" {
		t.Error("kernel version not collected")
	}
}
