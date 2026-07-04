package agent

import "testing"

func TestParsePkgLineOpkg(t *testing.T) {
	p, ok := parsePkgLine("opkg", "dnsmasq - 2.90-4")
	if !ok || p.Name != "dnsmasq" || p.Version != "2.90-4" {
		t.Errorf("got %+v ok=%v", p, ok)
	}
	// opkg lines can carry a description after a second " - "
	p, ok = parsePkgLine("opkg", "uhttpd - 2023-06-25-34a8a74d-2 - uHTTPd web server")
	if !ok || p.Name != "uhttpd" || p.Version != "2023-06-25-34a8a74d-2" {
		t.Errorf("got %+v ok=%v", p, ok)
	}
	if _, ok := parsePkgLine("opkg", ""); ok {
		t.Error("empty line parsed")
	}
}

func TestParsePkgLineApk(t *testing.T) {
	p, ok := parsePkgLine("apk", "busybox-1.36.1-r5 x86_64 {busybox} (GPL-2.0-only) [installed]")
	if !ok || p.Name != "busybox" || p.Version != "1.36.1-r5" {
		t.Errorf("got %+v ok=%v", p, ok)
	}
	// version containing dashes
	p, ok = parsePkgLine("apk", "libncursesw-6.4_p20230506-r0 x86_64 {ncurses} (MIT) [installed]")
	if !ok || p.Name != "libncursesw" || p.Version != "6.4_p20230506-r0" {
		t.Errorf("got %+v ok=%v", p, ok)
	}
	if _, ok := parsePkgLine("apk", "WARNING: opening from cache"); ok {
		t.Error("warning line parsed as a package")
	}
}

func TestPkgNameValidation(t *testing.T) {
	valid := []string{"dnsmasq", "libustream-openssl", "kmod-wireguard", "luci-app-firewall", "zlib1g"}
	for _, n := range valid {
		if !pkgNameRe.MatchString(n) {
			t.Errorf("%q rejected", n)
		}
	}
	invalid := []string{"", "-flag", "a b", "pkg;rm", "../etc", "$(x)"}
	for _, n := range invalid {
		if pkgNameRe.MatchString(n) {
			t.Errorf("%q accepted", n)
		}
	}
}
