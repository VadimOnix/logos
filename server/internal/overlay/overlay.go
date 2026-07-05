// Package overlay holds the coordinator logic for WireGuard overlay networks
// (F7): address assignment inside the overlay CIDR and per-node sync specs.
// Pure functions — persistence and RPC live in store/ and api/.
package overlay

import (
	"fmt"
	"net/netip"
	"strings"

	"github.com/VadimOnix/logos/server/internal/store"
)

// PeerSpec is one WireGuard peer as pushed to an agent. AllowedIPs carries
// the peer's overlay address plus any LAN subnets it advertises
// (subnet-router mode); the agent installs routes for all of them.
type PeerSpec struct {
	PublicKey    string   `json:"public_key"`
	EndpointHost string   `json:"endpoint_host,omitempty"`
	EndpointPort int      `json:"endpoint_port,omitempty"`
	AllowedIPs   []string `json:"allowed_ips"`
	Keepalive    int      `json:"keepalive,omitempty"`
}

// SyncParams is the desired state of one overlay interface on one node
// (params of the overlay.sync / overlay.reconcile agent RPCs).
type SyncParams struct {
	Iface      string     `json:"iface"`
	Address    string     `json:"address"` // overlay IP with the overlay prefix, e.g. 100.90.0.2/24
	ListenPort int        `json:"listen_port"`
	Peers      []PeerSpec `json:"peers"`
	// Hosts are overlay-DNS names (v1.0): every member, including the node
	// itself, resolvable as <node>.<overlay>.logos on the device.
	Hosts []HostEntry `json:"hosts,omitempty"`
}

// HostEntry is one overlay-DNS name pushed with the spec.
type HostEntry struct {
	Name string `json:"name"`
	IP   string `json:"ip"`
}

// keepalive keeps NAT bindings open so two NATed peers can hold a session
// once one of them punched through. Relay fallback for hard NATs is the next
// F7 slice.
const keepalive = 25

// IfaceName is the uci interface an overlay materializes as on every member.
func IfaceName(overlayID int64) string { return fmt.Sprintf("logos%d", overlayID) }

// ListenPort picks the UDP port for an overlay. One port per overlay (not
// per node): members are distinct machines, so collisions only matter for a
// node in many overlays — the offset keeps them apart.
func ListenPort(overlayID int64) int { return 51820 + int(overlayID%10000) }

// ParseCIDR validates an overlay network: IPv4, a real network address (no
// host bits set), and room for at least two hosts.
func ParseCIDR(s string) (netip.Prefix, error) {
	p, err := netip.ParsePrefix(s)
	if err != nil {
		return netip.Prefix{}, fmt.Errorf("invalid CIDR %q: %w", s, err)
	}
	if !p.Addr().Is4() {
		return netip.Prefix{}, fmt.Errorf("overlay CIDR must be IPv4 (got %q)", s)
	}
	if p.Masked() != p {
		return netip.Prefix{}, fmt.Errorf("%q has host bits set (use %s)", s, p.Masked())
	}
	if p.Bits() > 30 {
		return netip.Prefix{}, fmt.Errorf("%q is too small for an overlay (need /30 or larger)", s)
	}
	return p, nil
}

// FindOverlap returns the first existing overlay whose CIDR shares addresses
// with prefix (PRD §5.2 "overlap detection"): a node joined to two
// overlapping overlays would get ambiguous routes, so creation is refused
// up front. Stored CIDRs that fail to parse are skipped — they were
// validated on the way in, and a scan should not break on one bad row.
func FindOverlap(prefix netip.Prefix, existing []*store.Overlay) *store.Overlay {
	for _, o := range existing {
		if p, err := netip.ParsePrefix(o.CIDR); err == nil && p.Overlaps(prefix) {
			return o
		}
	}
	return nil
}

// NextFreeIP allocates the lowest unused host address in the overlay,
// skipping the network and broadcast addresses.
func NextFreeIP(prefix netip.Prefix, used []string) (netip.Addr, error) {
	taken := make(map[netip.Addr]bool, len(used))
	for _, s := range used {
		if a, err := netip.ParseAddr(s); err == nil {
			taken[a] = true
		}
	}
	bcast := broadcast(prefix)
	for a := prefix.Addr().Next(); prefix.Contains(a) && a != bcast; a = a.Next() {
		if !taken[a] {
			return a, nil
		}
	}
	return netip.Addr{}, fmt.Errorf("overlay %s is full (%d hosts)", prefix, len(used))
}

func broadcast(p netip.Prefix) netip.Addr {
	b := p.Addr().As4()
	for i := p.Bits(); i < 32; i++ {
		b[i/8] |= 1 << (7 - i%8)
	}
	return netip.AddrFrom4(b)
}

// BuildSync produces the desired interface state for one member: its own
// address inside the overlay and every *other* member with a known public
// key as a peer. Members that have not completed their first sync (no key
// yet) are omitted and picked up by the next pass.
func BuildSync(o *store.Overlay, members []*store.OverlayMember, self *store.OverlayMember) SyncParams {
	prefix, _ := netip.ParsePrefix(o.CIDR)
	sp := SyncParams{
		Iface:      IfaceName(o.ID),
		Address:    fmt.Sprintf("%s/%d", self.OverlayIP, prefix.Bits()),
		ListenPort: self.ListenPort,
		Peers:      []PeerSpec{},
	}
	for _, m := range members {
		if m.NodeID == self.NodeID || m.PublicKey == "" || m.NodeStatus != store.NodeStatusEnrolled {
			continue
		}
		peer := PeerSpec{
			PublicKey:  m.PublicKey,
			AllowedIPs: append([]string{m.OverlayIP + "/32"}, m.Subnets...),
			Keepalive:  keepalive,
		}
		if m.NodeAddr != "" {
			peer.EndpointHost = m.NodeAddr
			peer.EndpointPort = m.ListenPort
		}
		sp.Peers = append(sp.Peers, peer)
	}
	sp.Hosts = buildHosts(o, members)
	return sp
}

// buildHosts derives the overlay-DNS entries: one per enrolled member
// (including the receiving node itself), named <node>.<overlay>.logos.
// Name collisions after sanitizing are disambiguated with the node id
// prefix so every member stays resolvable.
func buildHosts(o *store.Overlay, members []*store.OverlayMember) []HostEntry {
	zone := dnsLabel(o.Name)
	if zone == "" {
		zone = fmt.Sprintf("overlay%d", o.ID)
	}
	hosts := []HostEntry{}
	seen := map[string]bool{}
	for _, m := range members {
		if m.NodeStatus != store.NodeStatusEnrolled || m.OverlayIP == "" {
			continue
		}
		label := dnsLabel(m.NodeName)
		if label == "" && len(m.NodeID) >= 8 {
			label = m.NodeID[:8]
		}
		if label == "" {
			continue
		}
		if seen[label] && len(m.NodeID) >= 8 {
			label += "-" + m.NodeID[:8]
		}
		seen[label] = true
		hosts = append(hosts, HostEntry{Name: label + "." + zone + ".logos", IP: m.OverlayIP})
	}
	return hosts
}

// dnsLabel reduces a free-form name to a valid lowercase hostname label:
// anything outside [a-z0-9-] becomes '-', runs collapse, edges trim. Empty
// when nothing survives.
func dnsLabel(s string) string {
	var b []byte
	lastDash := true // suppress a leading dash
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			b = append(b, byte(r))
			lastDash = false
		default:
			if !lastDash {
				b = append(b, '-')
				lastDash = true
			}
		}
	}
	out := strings.TrimRight(string(b), "-")
	if len(out) > 63 {
		out = strings.TrimRight(out[:63], "-")
	}
	return out
}
