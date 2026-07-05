package overlay

import (
	"net/netip"
	"reflect"
	"testing"

	"github.com/VadimOnix/logos/server/internal/store"
)

func TestParseCIDR(t *testing.T) {
	if _, err := ParseCIDR("100.90.0.0/24"); err != nil {
		t.Errorf("valid CIDR rejected: %v", err)
	}
	for _, bad := range []string{"100.90.0.1/24", "fd00::/64", "10.0.0.0/31", "nope"} {
		if _, err := ParseCIDR(bad); err == nil {
			t.Errorf("ParseCIDR(%q) accepted", bad)
		}
	}
}

func TestNextFreeIP(t *testing.T) {
	p := netip.MustParsePrefix("100.90.0.0/29") // hosts .1–.6
	ip, err := NextFreeIP(p, nil)
	if err != nil || ip.String() != "100.90.0.1" {
		t.Fatalf("first alloc = %v, %v", ip, err)
	}
	ip, err = NextFreeIP(p, []string{"100.90.0.1", "100.90.0.3"})
	if err != nil || ip.String() != "100.90.0.2" {
		t.Fatalf("gap alloc = %v, %v", ip, err)
	}
	full := []string{"100.90.0.1", "100.90.0.2", "100.90.0.3", "100.90.0.4", "100.90.0.5", "100.90.0.6"}
	if _, err := NextFreeIP(p, full); err == nil {
		t.Error("full overlay still allocated (broadcast must be excluded)")
	}
}

func TestBuildSync(t *testing.T) {
	o := &store.Overlay{ID: 3, Name: "hq", CIDR: "100.90.0.0/24"}
	self := &store.OverlayMember{NodeID: "a", OverlayIP: "100.90.0.1", ListenPort: 51823, NodeStatus: store.NodeStatusEnrolled}
	members := []*store.OverlayMember{
		self,
		{NodeID: "b", OverlayIP: "100.90.0.2", ListenPort: 51823, PublicKey: "PKB",
			NodeStatus: store.NodeStatusEnrolled, NodeAddr: "203.0.113.7",
			Subnets: []string{"192.168.5.0/24"}},
		{NodeID: "c", OverlayIP: "100.90.0.3", ListenPort: 51823, PublicKey: "", // not yet synced
			NodeStatus: store.NodeStatusEnrolled},
		{NodeID: "d", OverlayIP: "100.90.0.4", ListenPort: 51823, PublicKey: "PKD",
			NodeStatus: store.NodeStatusLeft},
	}
	sp := BuildSync(o, members, self)
	if sp.Iface != "logos3" || sp.Address != "100.90.0.1/24" || sp.ListenPort != 51823 {
		t.Errorf("iface spec: %+v", sp)
	}
	if len(sp.Peers) != 1 {
		t.Fatalf("want exactly peer b, got %+v", sp.Peers)
	}
	p := sp.Peers[0]
	if p.PublicKey != "PKB" || p.EndpointHost != "203.0.113.7" || p.EndpointPort != 51823 || p.Keepalive != 25 {
		t.Errorf("peer: %+v", p)
	}
	if !reflect.DeepEqual(p.AllowedIPs, []string{"100.90.0.2/32", "192.168.5.0/24"}) {
		t.Errorf("allowed ips: %v", p.AllowedIPs)
	}
}

func TestFindOverlap(t *testing.T) {
	existing := []*store.Overlay{
		{ID: 1, Name: "mesh", CIDR: "100.90.0.0/24"},
		{ID: 2, Name: "lab", CIDR: "10.8.0.0/16"},
		{ID: 3, Name: "broken", CIDR: "not-a-cidr"}, // skipped, must not panic
	}
	for _, tc := range []struct {
		cidr string
		want string // clashing overlay name, "" = no overlap
	}{
		{"100.90.0.0/24", "mesh"},   // identical
		{"100.90.0.128/25", "mesh"}, // subset
		{"100.90.0.0/16", "mesh"},   // superset
		{"10.8.42.0/24", "lab"},     // inside the /16
		{"100.91.0.0/24", ""},       // disjoint
		{"192.168.1.0/24", ""},      // disjoint
	} {
		p := netip.MustParsePrefix(tc.cidr)
		got := FindOverlap(p, existing)
		switch {
		case tc.want == "" && got != nil:
			t.Errorf("%s: unexpected overlap with %q", tc.cidr, got.Name)
		case tc.want != "" && (got == nil || got.Name != tc.want):
			t.Errorf("%s: got %v, want %q", tc.cidr, got, tc.want)
		}
	}
	if FindOverlap(netip.MustParsePrefix("100.90.0.0/24"), nil) != nil {
		t.Error("overlap reported against no overlays")
	}
}

func TestDNSLabel(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"Office Router", "office-router"},
		{"кухня", ""}, // nothing survives → caller falls back
		{"router_2 (attic)", "router-2-attic"},
		{"--x--", "x"},
		{"UPPER", "upper"},
	} {
		if got := dnsLabel(tc.in); got != tc.want {
			t.Errorf("dnsLabel(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestBuildHosts(t *testing.T) {
	o := &store.Overlay{ID: 3, Name: "Home Mesh"}
	members := []*store.OverlayMember{
		{NodeID: "aaaaaaaa-1", NodeName: "Office Router", NodeStatus: store.NodeStatusEnrolled, OverlayIP: "100.90.0.2"},
		{NodeID: "bbbbbbbb-2", NodeName: "office router", NodeStatus: store.NodeStatusEnrolled, OverlayIP: "100.90.0.3"}, // collides after sanitizing
		{NodeID: "cccccccc-3", NodeName: "кухня", NodeStatus: store.NodeStatusEnrolled, OverlayIP: "100.90.0.4"},         // non-latin → id fallback
		{NodeID: "dddddddd-4", NodeName: "gone", NodeStatus: store.NodeStatusLeft, OverlayIP: "100.90.0.5"},              // left → skipped
	}
	hosts := buildHosts(o, members)
	if len(hosts) != 3 {
		t.Fatalf("got %d hosts: %+v", len(hosts), hosts)
	}
	if hosts[0].Name != "office-router.home-mesh.logos" || hosts[0].IP != "100.90.0.2" {
		t.Errorf("first: %+v", hosts[0])
	}
	if hosts[1].Name != "office-router-bbbbbbbb.home-mesh.logos" {
		t.Errorf("collision not disambiguated: %+v", hosts[1])
	}
	if hosts[2].Name != "cccccccc.home-mesh.logos" {
		t.Errorf("fallback label: %+v", hosts[2])
	}
}
