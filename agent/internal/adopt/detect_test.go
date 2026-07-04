package adopt

import "testing"

func TestGoArchFor(t *testing.T) {
	cases := []struct {
		machine string
		le      bool
		want    string
	}{
		{"x86_64", true, "amd64"},
		{"aarch64", true, "arm64"},
		{"armv7l", true, "arm"},
		{"armv6l", true, "arm"},
		{"i686", true, "386"},
		{"riscv64", true, "riscv64"},
		{"mips", false, "mips"},  // ath79 (big-endian)
		{"mips", true, "mipsle"}, // mt7621
		{"mips64", false, "mips64"},
		{"mips64", true, "mips64le"},
	}
	for _, c := range cases {
		got, err := goArchFor(c.machine, c.le)
		if err != nil || got != c.want {
			t.Errorf("goArchFor(%q, le=%v) = %q, %v; want %q", c.machine, c.le, got, err, c.want)
		}
	}
	if _, err := goArchFor("vax", true); err == nil {
		t.Error("unknown machine accepted")
	}
}

func TestCheckCompatibility(t *testing.T) {
	good := &DeviceInfo{IsOpenWrt: true, PkgManager: "opkg", MemTotalKB: 128 * 1024, FlashFreeKB: 16 * 1024}
	if err := good.CheckCompatibility(false); err != nil {
		t.Errorf("good device rejected: %v", err)
	}

	bad := &DeviceInfo{IsOpenWrt: false, OSPretty: "Debian", PkgManager: "", MemTotalKB: 16 * 1024, FlashFreeKB: 1024}
	if err := bad.CheckCompatibility(false); err == nil {
		t.Error("bad device accepted")
	}
	if err := bad.CheckCompatibility(true); err != nil {
		t.Errorf("--force did not override: %v", err)
	}
}
