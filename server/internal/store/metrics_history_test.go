package store

import (
	"encoding/json"
	"testing"
)

// TestDeriveSample checks the heartbeat→sample derivation (percentages and
// aggregate traffic) without a database.
func TestDeriveSample(t *testing.T) {
	raw := []byte(`{
		"load1": 0.42,
		"mem_total_kb": 128000, "mem_available_kb": 32000,
		"rootfs_total_kb": 20000, "rootfs_free_kb": 5000,
		"interfaces": [
			{"rx_bytes": 100, "tx_bytes": 200},
			{"rx_bytes": 50, "tx_bytes": 25}
		],
		"dhcp_clients": [{"mac":"a"},{"mac":"b"},{"mac":"c"}]
	}`)
	var hm heartbeatMetrics
	if err := json.Unmarshal(raw, &hm); err != nil {
		t.Fatal(err)
	}
	if hm.Load1 != 0.42 {
		t.Errorf("load1 = %v", hm.Load1)
	}
	memUsed := 100 * (hm.MemTotalKB - hm.MemAvailableKB) / hm.MemTotalKB
	if memUsed != 75 {
		t.Errorf("mem used pct = %v, want 75", memUsed)
	}
	fsUsed := 100 * (hm.RootFSTotalKB - hm.RootFSFreeKB) / hm.RootFSTotalKB
	if fsUsed != 75 {
		t.Errorf("rootfs used pct = %v, want 75", fsUsed)
	}
	var rx, tx int64
	for _, i := range hm.Interfaces {
		rx += i.RxBytes
		tx += i.TxBytes
	}
	if rx != 150 || tx != 225 {
		t.Errorf("aggregate traffic rx=%d tx=%d", rx, tx)
	}
	if len(hm.DHCPClients) != 3 {
		t.Errorf("dhcp clients = %d", len(hm.DHCPClients))
	}
}

func TestRootFSUsedPct(t *testing.T) {
	if pct, ok := RootFSUsedPct([]byte(`{"rootfs_total_kb":20000,"rootfs_free_kb":5000}`)); !ok || pct != 75 {
		t.Errorf("pct=%v ok=%v, want 75 true", pct, ok)
	}
	// No rootfs size reported → unknown, not 0%.
	if _, ok := RootFSUsedPct([]byte(`{"load1":0.1}`)); ok {
		t.Error("missing rootfs total should yield ok=false")
	}
	if _, ok := RootFSUsedPct(nil); ok {
		t.Error("nil payload should yield ok=false")
	}
	if _, ok := RootFSUsedPct([]byte(`not json`)); ok {
		t.Error("unparseable payload should yield ok=false")
	}
}

// TestDeriveSampleZeroTotals ensures divide-by-zero is avoided when a node
// reports no memory/fs totals (fields omitted).
func TestDeriveSampleZeroTotals(t *testing.T) {
	var hm heartbeatMetrics
	if err := json.Unmarshal([]byte(`{"load1":0.1}`), &hm); err != nil {
		t.Fatal(err)
	}
	if hm.MemTotalKB != 0 || hm.RootFSTotalKB != 0 {
		t.Errorf("expected zero totals, got %+v", hm)
	}
	// InsertMetricSample guards on >0; nothing to divide here.
}

func TestConfigHashFromMetrics(t *testing.T) {
	if h := ConfigHashFromMetrics([]byte(`{"load1":1,"config_hash":"abc"}`)); h != "abc" {
		t.Errorf("got %q, want abc", h)
	}
	for _, raw := range [][]byte{nil, []byte(`{}`), []byte(`not json`)} {
		if h := ConfigHashFromMetrics(raw); h != "" {
			t.Errorf("%q: got %q, want empty", raw, h)
		}
	}
}
