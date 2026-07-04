package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadIfaceStats(t *testing.T) {
	sample := `Inter-|   Receive                                                |  Transmit
 face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets errs drop fifo colls carrier compressed
    lo:  541380    5806    0    0    0     0          0         0   541380    5806    0    0    0     0       0          0
  eth0: 1234567     890    0    0    0     0          0         0   7654321     432    0    0    0     0       0          0
br-lan:     100       1    0    0    0     0          0         0       200       2    0    0    0     0       0          0
`
	path := filepath.Join(t.TempDir(), "dev")
	if err := os.WriteFile(path, []byte(sample), 0o644); err != nil {
		t.Fatal(err)
	}
	stats := readIfaceStats(path)
	if len(stats) != 2 {
		t.Fatalf("got %d interfaces (lo must be skipped), want 2: %+v", len(stats), stats)
	}
	if stats[0].Name != "eth0" || stats[0].RxBytes != 1234567 || stats[0].TxBytes != 7654321 ||
		stats[0].RxPackets != 890 || stats[0].TxPackets != 432 {
		t.Errorf("eth0 = %+v", stats[0])
	}
	if stats[1].Name != "br-lan" || stats[1].RxBytes != 100 || stats[1].TxBytes != 200 {
		t.Errorf("br-lan = %+v", stats[1])
	}
	if got := readIfaceStats(filepath.Join(t.TempDir(), "missing")); got != nil {
		t.Errorf("missing file: got %+v, want nil", got)
	}
}

func TestReadDHCPLeases(t *testing.T) {
	sample := `1751600000 aa:bb:cc:dd:ee:ff 192.168.1.100 laptop 01:aa:bb:cc:dd:ee:ff
1751600100 11:22:33:44:55:66 192.168.1.101 * *
garbage line
`
	path := filepath.Join(t.TempDir(), "leases")
	if err := os.WriteFile(path, []byte(sample), 0o644); err != nil {
		t.Fatal(err)
	}
	leases := readDHCPLeases(path)
	if len(leases) != 2 {
		t.Fatalf("got %d leases, want 2: %+v", len(leases), leases)
	}
	if leases[0].Hostname != "laptop" || leases[0].IP != "192.168.1.100" ||
		leases[0].MAC != "aa:bb:cc:dd:ee:ff" || leases[0].Expires != 1751600000 {
		t.Errorf("lease[0] = %+v", leases[0])
	}
	if leases[1].Hostname != "" {
		t.Errorf(`"*" hostname should map to empty, got %q`, leases[1].Hostname)
	}
}
