package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteOverlayHosts(t *testing.T) {
	dir := t.TempDir()
	oldDir, oldHup := overlayHostsDir, hupDnsmasq
	hups := 0
	overlayHostsDir, hupDnsmasq = dir, func() { hups++ }
	defer func() { overlayHostsDir, hupDnsmasq = oldDir, oldHup }()

	hosts := []overlayHost{
		{Name: "office.mesh.logos", IP: "100.90.0.2"},
		{Name: "attic.mesh.logos", IP: "100.90.0.3"},
	}
	if err := writeOverlayHosts("logos1", hosts); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "logos1"))
	if err != nil {
		t.Fatal(err)
	}
	want := "# managed by logos-agent: overlay peer names\n100.90.0.2 office.mesh.logos\n100.90.0.3 attic.mesh.logos\n"
	if string(data) != want {
		t.Errorf("file:\n%s\nwant:\n%s", data, want)
	}
	if hups != 1 {
		t.Errorf("dnsmasq poked %d times, want 1", hups)
	}

	// Empty list removes the file and pokes dnsmasq once more.
	if err := writeOverlayHosts("logos1", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "logos1")); !os.IsNotExist(err) {
		t.Error("hosts file not removed")
	}
	if hups != 2 {
		t.Errorf("dnsmasq poked %d times, want 2", hups)
	}

	// Removing again is a silent no-op — nothing was published.
	if err := writeOverlayHosts("logos1", nil); err != nil {
		t.Fatal(err)
	}
	if hups != 2 {
		t.Errorf("no-op removal poked dnsmasq")
	}
}

func TestOverlaySpecValidatesHosts(t *testing.T) {
	base := overlaySpec{Iface: "logos1", Address: "100.90.0.2/24", ListenPort: 51821}
	for _, tc := range []struct {
		host overlayHost
		ok   bool
	}{
		{overlayHost{Name: "office.mesh.logos", IP: "100.90.0.2"}, true},
		{overlayHost{Name: "UPPER.mesh.logos", IP: "100.90.0.2"}, false}, // uppercase
		{overlayHost{Name: "office.mesh.logos", IP: "not-an-ip"}, false},
		{overlayHost{Name: "-bad.logos", IP: "100.90.0.2"}, false},
		{overlayHost{Name: "", IP: "100.90.0.2"}, false},
	} {
		s := base
		s.Hosts = []overlayHost{tc.host}
		if err := s.validate(); (err == nil) != tc.ok {
			t.Errorf("host %+v: err=%v, want ok=%v", tc.host, err, tc.ok)
		}
	}
}
