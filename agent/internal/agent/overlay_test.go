package agent

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// RFC 7748 §6.1 test vector: Alice's X25519 keypair. WireGuard keys are
// plain X25519, so the derivation must match.
func TestWGPublicKey(t *testing.T) {
	priv, _ := hex.DecodeString("77076d0a7318a57d3c16c17251b26645df4c2f87ebc0992ab177fba51db92c2a")
	wantPub, _ := hex.DecodeString("8520f0098930a754748b7ddcb43ef75a0dbf3a0d26381af4eba4a98eaa9b4e6a")
	got, err := wgPublicKey(base64.StdEncoding.EncodeToString(priv))
	if err != nil {
		t.Fatal(err)
	}
	if got != base64.StdEncoding.EncodeToString(wantPub) {
		t.Errorf("derived %s, want %s", got, base64.StdEncoding.EncodeToString(wantPub))
	}
}

func TestGenerateWGKey(t *testing.T) {
	priv, pub, err := generateWGKey()
	if err != nil {
		t.Fatal(err)
	}
	if !validWGKey(priv) || !validWGKey(pub) {
		t.Fatalf("invalid key material: %q %q", priv, pub)
	}
	raw, _ := base64.StdEncoding.DecodeString(priv)
	if raw[0]&7 != 0 || raw[31]&128 != 0 || raw[31]&64 == 0 {
		t.Error("private key is not clamped")
	}
	got, err := wgPublicKey(priv)
	if err != nil || got != pub {
		t.Errorf("pub does not re-derive: %s vs %s (%v)", got, pub, err)
	}
}

func TestOverlaySpecValidate(t *testing.T) {
	pk := base64.StdEncoding.EncodeToString(make([]byte, 32))
	good := overlaySpec{Iface: "logos1", Address: "100.90.0.1/24", ListenPort: 51821,
		Peers: []overlayPeer{{PublicKey: pk, EndpointHost: "203.0.113.7", EndpointPort: 51821,
			AllowedIPs: []string{"100.90.0.2/32", "192.168.5.0/24"}, Keepalive: 25}}}
	if err := good.validate(); err != nil {
		t.Errorf("good spec rejected: %v", err)
	}
	bads := []overlaySpec{
		{Iface: "wan", Address: "100.90.0.1/24", ListenPort: 51821},
		{Iface: "logos1", Address: "not-an-ip", ListenPort: 51821},
		{Iface: "logos1", Address: "100.90.0.1/24", ListenPort: 0},
		{Iface: "logos1", Address: "100.90.0.1/24", ListenPort: 51821,
			Peers: []overlayPeer{{PublicKey: "short", AllowedIPs: []string{"100.90.0.2/32"}}}},
		{Iface: "logos1", Address: "100.90.0.1/24", ListenPort: 51821,
			Peers: []overlayPeer{{PublicKey: pk, AllowedIPs: []string{"bogus"}}}},
		{Iface: "logos1", Address: "100.90.0.1/24", ListenPort: 51821,
			Peers: []overlayPeer{{PublicKey: pk, AllowedIPs: []string{"100.90.0.2/32"}, EndpointHost: "evil host"}}},
	}
	for i, b := range bads {
		if err := b.validate(); err == nil {
			t.Errorf("bad spec %d accepted", i)
		}
	}
}

func TestPlanOverlayApply(t *testing.T) {
	pk := base64.StdEncoding.EncodeToString(make([]byte, 32))
	spec := &overlaySpec{Iface: "logos7", Address: "100.90.0.1/24", ListenPort: 51827,
		Peers: []overlayPeer{{PublicKey: pk, EndpointHost: "203.0.113.7", EndpointPort: 51827,
			AllowedIPs: []string{"100.90.0.2/32", "192.168.5.0/24"}, Keepalive: 25}}}
	var lines []string
	for _, c := range planOverlayApply(spec, "PRIV") {
		lines = append(lines, strings.Join(c.args, " "))
	}
	joined := strings.Join(lines, "\n")
	for _, want := range []string{
		"set network.logos7=interface",
		"set network.logos7.proto=wireguard",
		"set network.logos7.private_key=PRIV",
		"set network.logos7.listen_port=51827",
		"add_list network.logos7.addresses=100.90.0.1/24",
		"set network.logos7_p0=wireguard_logos7",
		"set network.logos7_p0.public_key=" + pk,
		"set network.logos7_p0.endpoint_host=203.0.113.7",
		"set network.logos7_p0.endpoint_port=51827",
		"set network.logos7_p0.persistent_keepalive=25",
		"add_list network.logos7_p0.allowed_ips=100.90.0.2/32",
		"add_list network.logos7_p0.allowed_ips=192.168.5.0/24",
		"set firewall.logos=zone",
		"add_list firewall.logos.network=logos7",
		"set firewall.logos_rule_logos7.dest_port=51827",
		"commit network",
		"commit firewall",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("plan missing %q:\n%s", want, joined)
		}
	}
}

func TestPlanOverlayRemove(t *testing.T) {
	var last []string
	for _, c := range planOverlayRemove("logos7", true) {
		last = append(last, strings.Join(c.args, " "))
	}
	for _, want := range []string{
		"-q delete network.logos7",
		"-q del_list firewall.logos.network=logos7",
		"-q delete firewall.logos_rule_logos7",
		"-q delete firewall.logos",
		"commit network",
	} {
		if !slices.Contains(last, want) {
			t.Errorf("remove plan missing %q:\n%s", want, strings.Join(last, "\n"))
		}
	}
	var notLast []string
	for _, c := range planOverlayRemove("logos7", false) {
		notLast = append(notLast, strings.Join(c.args, " "))
	}
	if slices.Contains(notLast, "-q delete firewall.logos") {
		t.Error("zone removed while other overlays remain")
	}
}

func TestParseLogosIfaces(t *testing.T) {
	show := `network.lan=interface
network.lan.proto='static'
network.logos1=interface
network.logos1.proto='wireguard'
network.logos12=interface
network.logosx=interface
network.@wireguard_logos1[0]=wireguard_logos1
`
	got := parseLogosIfaces(show)
	if !slices.Equal(got, []string{"logos1", "logos12"}) {
		t.Errorf("got %v", got)
	}
}

// TestHandleOverlaySyncStub drives the full sync handler against stub
// uci/wg binaries and checks the reported public key is stable across syncs
// (the generated private key is cached until uci persists it).
func TestHandleOverlaySyncStub(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "calls.log")
	// get/delete report absence; everything else succeeds.
	stub := `#!/bin/sh
echo "uci $@" >> "$UCI_LOG"
for a in "$@"; do case "$a" in get|delete|del_list) exit 1 ;; show) echo ""; exit 0 ;; esac; done
exit 0
`
	if err := os.WriteFile(filepath.Join(dir, "uci"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "wg"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("UCI_LOG", logPath)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	params, _ := json.Marshal(overlaySpec{Iface: "logos9", Address: "100.90.0.1/24", ListenPort: 51829})
	res1, err := handleOverlaySync(context.Background(), params)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	pub1 := res1.(map[string]string)["public_key"]
	if !validWGKey(pub1) {
		t.Fatalf("bad public key %q", pub1)
	}
	res2, err := handleOverlaySync(context.Background(), params)
	if err != nil || res2.(map[string]string)["public_key"] != pub1 {
		t.Errorf("public key not stable across syncs: %v %v", res2, err)
	}
	log, _ := os.ReadFile(logPath)
	for _, want := range []string{"set network.logos9=interface", "commit network", "commit firewall"} {
		if !strings.Contains(string(log), want) {
			t.Errorf("uci call log missing %q", want)
		}
	}
}

func TestHandleOverlayReconcileEmptyWithoutWG(t *testing.T) {
	// A node outside any overlay must not fail reconcile just because
	// wireguard-tools is absent.
	dir := t.TempDir()
	t.Setenv("PATH", dir) // nothing on PATH
	res, err := handleOverlayReconcile(context.Background(), json.RawMessage(`{"overlays":[]}`))
	if err != nil {
		t.Fatalf("empty reconcile failed: %v", err)
	}
	if _, ok := res.(map[string]any); !ok {
		t.Errorf("unexpected result %v", res)
	}
}
