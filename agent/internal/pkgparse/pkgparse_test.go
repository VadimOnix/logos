package pkgparse

import (
	"slices"
	"testing"
)

func TestNamesOpkg(t *testing.T) {
	out := "dnsmasq - 2.90-4\nuhttpd - 2023-06-25-2 - uHTTPd web server\n\n"
	got := Names("opkg", out)
	if !slices.Equal(got, []string{"dnsmasq", "uhttpd"}) {
		t.Errorf("got %v", got)
	}
}

func TestNamesApk(t *testing.T) {
	out := `WARNING: opening from cache
busybox-1.36.1-r5 x86_64 {busybox} (GPL-2.0-only) [installed]
libncursesw-6.4_p20230506-r0 x86_64 {ncurses} (MIT) [installed]
`
	got := Names("apk", out)
	if !slices.Equal(got, []string{"busybox", "libncursesw"}) {
		t.Errorf("got %v", got)
	}
}
