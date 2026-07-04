package agent

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/netip"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/crypto/curve25519"
)

// F7: WireGuard overlay networks. The server is the coordinator — it decides
// membership, addresses and peers — but the private key is generated here and
// never leaves the device; only the public key goes back in the RPC result.
//
// The overlay materializes as a uci wireguard interface (proto 'wireguard' is
// handled by netifd once wireguard-tools + the kernel module are installed)
// plus a self-contained "logos" firewall zone: overlay traffic is allowed,
// forwarded to/from lan, and the listen port is opened on wan. Everything we
// create is a *named* uci section, so a sync is a deterministic rewrite and
// removal cannot touch anything the user configured.

type overlayPeer struct {
	PublicKey    string   `json:"public_key"`
	EndpointHost string   `json:"endpoint_host,omitempty"`
	EndpointPort int      `json:"endpoint_port,omitempty"`
	AllowedIPs   []string `json:"allowed_ips"`
	Keepalive    int      `json:"keepalive,omitempty"`
}

type overlaySpec struct {
	Iface      string        `json:"iface"`
	Address    string        `json:"address"`
	ListenPort int           `json:"listen_port"`
	Peers      []overlayPeer `json:"peers"`
}

var (
	overlayIfaceRe    = regexp.MustCompile(`^logos\d{1,9}$`)
	overlayEndpointRe = regexp.MustCompile(`^[A-Za-z0-9._:\[\]-]+$`)
)

const maxOverlayPeers = 250

// overlayMu serializes overlay mutations: sync/reconcile/remove all rewrite
// uci state and must not interleave.
var overlayMu sync.Mutex

// overlayKeys caches generated private keys per interface for the lifetime
// of the process, so repeated syncs stay stable even before the first uci
// commit persisted the key.
var overlayKeys = struct {
	sync.Mutex
	m map[string]string
}{m: map[string]string{}}

// generateWGKey creates a WireGuard (Curve25519) keypair, base64-encoded.
func generateWGKey() (priv, pub string, err error) {
	var k [32]byte
	if _, err := rand.Read(k[:]); err != nil {
		return "", "", err
	}
	k[0] &= 248
	k[31] &= 127
	k[31] |= 64
	pubKey, err := wgPublicKey(base64.StdEncoding.EncodeToString(k[:]))
	if err != nil {
		return "", "", err
	}
	return base64.StdEncoding.EncodeToString(k[:]), pubKey, nil
}

// wgPublicKey derives the public key from a base64 private key.
func wgPublicKey(privB64 string) (string, error) {
	priv, err := base64.StdEncoding.DecodeString(privB64)
	if err != nil || len(priv) != 32 {
		return "", fmt.Errorf("invalid wireguard private key")
	}
	pub, err := curve25519.X25519(priv, curve25519.Basepoint)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(pub), nil
}

func validWGKey(b64 string) bool {
	b, err := base64.StdEncoding.DecodeString(b64)
	return err == nil && len(b) == 32
}

func (s *overlaySpec) validate() error {
	if !overlayIfaceRe.MatchString(s.Iface) {
		return fmt.Errorf("invalid overlay interface %q", s.Iface)
	}
	if p, err := netip.ParsePrefix(s.Address); err != nil || !p.Addr().Is4() {
		return fmt.Errorf("invalid overlay address %q", s.Address)
	}
	if s.ListenPort < 1 || s.ListenPort > 65535 {
		return fmt.Errorf("invalid listen port %d", s.ListenPort)
	}
	if len(s.Peers) > maxOverlayPeers {
		return fmt.Errorf("too many peers (%d)", len(s.Peers))
	}
	for i, p := range s.Peers {
		if !validWGKey(p.PublicKey) {
			return fmt.Errorf("peer %d: invalid public key", i)
		}
		if p.EndpointHost != "" && !overlayEndpointRe.MatchString(p.EndpointHost) {
			return fmt.Errorf("peer %d: invalid endpoint host %q", i, p.EndpointHost)
		}
		if p.EndpointPort < 0 || p.EndpointPort > 65535 || p.Keepalive < 0 || p.Keepalive > 3600 {
			return fmt.Errorf("peer %d: invalid endpoint port or keepalive", i)
		}
		if len(p.AllowedIPs) == 0 {
			return fmt.Errorf("peer %d: allowed_ips required", i)
		}
		for _, a := range p.AllowedIPs {
			if _, err := netip.ParsePrefix(a); err != nil {
				return fmt.Errorf("peer %d: invalid allowed ip %q", i, a)
			}
		}
	}
	return nil
}

// uciCmd is one uci invocation of an overlay plan. Optional commands clean
// up keys that may not exist — their failure is fine.
type uciCmd struct {
	args     []string
	optional bool
}

func set(key, value string) uciCmd { return uciCmd{args: []string{"set", key + "=" + value}} }
func addList(key, v string) uciCmd { return uciCmd{args: []string{"add_list", key + "=" + v}} }
func delOpt(key string) uciCmd     { return uciCmd{args: []string{"-q", "delete", key}, optional: true} }
func delListOpt(key, v string) uciCmd {
	return uciCmd{args: []string{"-q", "del_list", key + "=" + v}, optional: true}
}

// planOverlayApply renders the desired state of one overlay interface as a
// deterministic uci command sequence (peers of the interface are wiped by
// the caller first — see wipeOverlayPeers).
func planOverlayApply(spec *overlaySpec, privKey string) []uciCmd {
	iface := spec.Iface
	cmds := []uciCmd{
		set("network."+iface, "interface"),
		set("network."+iface+".proto", "wireguard"),
		set("network."+iface+".private_key", privKey),
		set("network."+iface+".listen_port", strconv.Itoa(spec.ListenPort)),
		delOpt("network." + iface + ".addresses"),
		addList("network."+iface+".addresses", spec.Address),
	}
	for k, p := range spec.Peers {
		sec := fmt.Sprintf("network.%s_p%d", iface, k)
		cmds = append(cmds,
			set(sec, "wireguard_"+iface),
			set(sec+".public_key", p.PublicKey),
			set(sec+".route_allowed_ips", "1"),
		)
		if p.EndpointHost != "" {
			cmds = append(cmds,
				set(sec+".endpoint_host", p.EndpointHost),
				set(sec+".endpoint_port", strconv.Itoa(p.EndpointPort)),
			)
		}
		if p.Keepalive > 0 {
			cmds = append(cmds, set(sec+".persistent_keepalive", strconv.Itoa(p.Keepalive)))
		}
		for _, a := range p.AllowedIPs {
			cmds = append(cmds, addList(sec+".allowed_ips", a))
		}
	}
	// Self-contained firewall integration: a "logos" zone holding every
	// overlay interface, two-way forwarding with lan, and the listen port
	// open on wan so peers can dial in directly.
	cmds = append(cmds,
		set("firewall.logos", "zone"),
		set("firewall.logos.name", "logos"),
		set("firewall.logos.input", "ACCEPT"),
		set("firewall.logos.output", "ACCEPT"),
		set("firewall.logos.forward", "ACCEPT"),
		delListOpt("firewall.logos.network", iface),
		addList("firewall.logos.network", iface),
		set("firewall.logos_fwd_in", "forwarding"),
		set("firewall.logos_fwd_in.src", "logos"),
		set("firewall.logos_fwd_in.dest", "lan"),
		set("firewall.logos_fwd_out", "forwarding"),
		set("firewall.logos_fwd_out.src", "lan"),
		set("firewall.logos_fwd_out.dest", "logos"),
		set("firewall.logos_rule_"+iface, "rule"),
		set("firewall.logos_rule_"+iface+".name", "Allow-Logos-"+iface),
		set("firewall.logos_rule_"+iface+".src", "wan"),
		set("firewall.logos_rule_"+iface+".proto", "udp"),
		set("firewall.logos_rule_"+iface+".dest_port", strconv.Itoa(spec.ListenPort)),
		set("firewall.logos_rule_"+iface+".target", "ACCEPT"),
		uciCmd{args: []string{"commit", "network"}},
		uciCmd{args: []string{"commit", "firewall"}},
	)
	return cmds
}

// planOverlayRemove tears one overlay down; when it was the last one, the
// shared zone and forwardings go too.
func planOverlayRemove(iface string, last bool) []uciCmd {
	cmds := []uciCmd{
		delOpt("network." + iface),
		delListOpt("firewall.logos.network", iface),
		delOpt("firewall.logos_rule_" + iface),
	}
	if last {
		cmds = append(cmds,
			delOpt("firewall.logos"),
			delOpt("firewall.logos_fwd_in"),
			delOpt("firewall.logos_fwd_out"),
		)
	}
	cmds = append(cmds,
		uciCmd{args: []string{"commit", "network"}},
		uciCmd{args: []string{"commit", "firewall"}},
	)
	return cmds
}

func runUciPlan(ctx context.Context, uciBin string, cmds []uciCmd) error {
	for _, c := range cmds {
		out, err := exec.CommandContext(ctx, uciBin, c.args...).CombinedOutput()
		if err != nil && !c.optional {
			return fmt.Errorf("uci %s: %v: %s", strings.Join(c.args, " "), err, out)
		}
	}
	return nil
}

// wipeOverlayPeers deletes every peer section of the interface (they are all
// of type wireguard_<iface>, including our named ones).
func wipeOverlayPeers(ctx context.Context, uciBin, iface string) {
	for i := 0; i < maxOverlayPeers*2; i++ {
		if err := exec.CommandContext(ctx, uciBin, "-q", "delete",
			fmt.Sprintf("network.@wireguard_%s[0]", iface)).Run(); err != nil {
			return
		}
	}
}

// parseLogosIfaces extracts overlay interfaces from `uci show network`.
func parseLogosIfaces(uciShow string) []string {
	var out []string
	for _, line := range strings.Split(uciShow, "\n") {
		rest, ok := strings.CutPrefix(strings.TrimSpace(line), "network.")
		if !ok {
			continue
		}
		name, typ, ok := strings.Cut(rest, "=")
		if ok && strings.Trim(typ, "'\"") == "interface" && overlayIfaceRe.MatchString(name) {
			out = append(out, name)
		}
	}
	return out
}

func listOverlayIfaces(ctx context.Context, uciBin string) ([]string, error) {
	out, err := exec.CommandContext(ctx, uciBin, "-q", "show", "network").Output()
	if err != nil {
		return nil, fmt.Errorf("uci show network: %w", err)
	}
	return parseLogosIfaces(string(out)), nil
}

// overlayPrereqs checks the node can actually run WireGuard before any
// config is touched.
func overlayPrereqs() (uciBin string, err error) {
	uciBin, err = exec.LookPath("uci")
	if err != nil {
		return "", fmt.Errorf("uci is not available on this node (not OpenWrt?)")
	}
	if _, err := exec.LookPath("wg"); err != nil {
		return "", fmt.Errorf("wireguard is not installed — install the wireguard-tools package (panel → node → Packages)")
	}
	return uciBin, nil
}

// currentPrivateKey returns the interface's existing key from uci, the
// process cache, or a fresh keypair (cached until a commit persists it).
func currentPrivateKey(ctx context.Context, uciBin, iface string) (string, error) {
	out, _ := exec.CommandContext(ctx, uciBin, "-q", "get", "network."+iface+".private_key").Output()
	if k := strings.TrimSpace(string(out)); validWGKey(k) {
		return k, nil
	}
	overlayKeys.Lock()
	defer overlayKeys.Unlock()
	if k, ok := overlayKeys.m[iface]; ok {
		return k, nil
	}
	priv, _, err := generateWGKey()
	if err != nil {
		return "", fmt.Errorf("generate wireguard key: %w", err)
	}
	overlayKeys.m[iface] = priv
	return priv, nil
}

// applyOverlay brings one interface to the desired state and returns the
// node's public key for it.
func applyOverlay(ctx context.Context, uciBin string, spec *overlaySpec) (string, error) {
	priv, err := currentPrivateKey(ctx, uciBin, spec.Iface)
	if err != nil {
		return "", err
	}
	pub, err := wgPublicKey(priv)
	if err != nil {
		return "", err
	}
	wipeOverlayPeers(ctx, uciBin, spec.Iface)
	if err := runUciPlan(ctx, uciBin, planOverlayApply(spec, priv)); err != nil {
		return "", err
	}
	return pub, nil
}

func removeOverlay(ctx context.Context, uciBin, iface string) error {
	wipeOverlayPeers(ctx, uciBin, iface)
	remaining, err := listOverlayIfaces(ctx, uciBin)
	if err != nil {
		return err
	}
	last := true
	for _, r := range remaining {
		if r != iface {
			last = false
			break
		}
	}
	return runUciPlan(ctx, uciBin, planOverlayRemove(iface, last))
}

// handleOverlaySync applies the desired state of a single overlay.
func handleOverlaySync(ctx context.Context, params json.RawMessage) (any, error) {
	var spec overlaySpec
	if err := json.Unmarshal(params, &spec); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	if err := spec.validate(); err != nil {
		return nil, err
	}
	uciBin, err := overlayPrereqs()
	if err != nil {
		return nil, err
	}
	overlayMu.Lock()
	defer overlayMu.Unlock()
	pub, err := applyOverlay(ctx, uciBin, &spec)
	if err != nil {
		return nil, err
	}
	reloadServices()
	return map[string]string{"public_key": pub}, nil
}

type overlayReconcileParams struct {
	Overlays []overlaySpec `json:"overlays"`
}

// handleOverlayReconcile applies the full desired overlay set and prunes any
// logosN interface the server no longer knows about — the agent may have
// been offline while overlays were changed or deleted.
func handleOverlayReconcile(ctx context.Context, params json.RawMessage) (any, error) {
	var p overlayReconcileParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	desired := map[string]bool{}
	for i := range p.Overlays {
		if err := p.Overlays[i].validate(); err != nil {
			return nil, err
		}
		desired[p.Overlays[i].Iface] = true
	}
	// A node with no overlays and no wireguard installed has nothing to
	// reconcile — don't fail the RPC over a missing prerequisite.
	uciBin, err := overlayPrereqs()
	if err != nil {
		if len(p.Overlays) == 0 {
			return map[string]any{"public_keys": map[string]string{}, "removed": []string{}}, nil
		}
		return nil, err
	}

	overlayMu.Lock()
	defer overlayMu.Unlock()
	existing, err := listOverlayIfaces(ctx, uciBin)
	if err != nil {
		return nil, err
	}
	removed := []string{}
	for _, iface := range existing {
		if !desired[iface] {
			if err := removeOverlay(ctx, uciBin, iface); err != nil {
				return nil, err
			}
			removed = append(removed, iface)
		}
	}
	keys := map[string]string{}
	for i := range p.Overlays {
		pub, err := applyOverlay(ctx, uciBin, &p.Overlays[i])
		if err != nil {
			return nil, fmt.Errorf("%s: %w", p.Overlays[i].Iface, err)
		}
		keys[p.Overlays[i].Iface] = pub
	}
	if len(removed) > 0 || len(p.Overlays) > 0 {
		reloadServices()
	}
	return map[string]any{"public_keys": keys, "removed": removed}, nil
}

type overlayRemoveParams struct {
	Iface string `json:"iface"`
}

func handleOverlayRemove(ctx context.Context, params json.RawMessage) (any, error) {
	var p overlayRemoveParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	if !overlayIfaceRe.MatchString(p.Iface) {
		return nil, fmt.Errorf("invalid overlay interface %q", p.Iface)
	}
	uciBin, err := exec.LookPath("uci")
	if err != nil {
		return nil, fmt.Errorf("uci is not available on this node (not OpenWrt?)")
	}
	overlayMu.Lock()
	defer overlayMu.Unlock()
	if err := removeOverlay(ctx, uciBin, p.Iface); err != nil {
		return nil, err
	}
	overlayKeys.Lock()
	delete(overlayKeys.m, p.Iface)
	overlayKeys.Unlock()
	reloadServices()
	return map[string]string{"removed": p.Iface}, nil
}
