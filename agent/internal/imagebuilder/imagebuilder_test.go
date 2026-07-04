package imagebuilder

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTarballURL(t *testing.T) {
	got := TarballURL("", "24.10.1", "ath79/generic")
	want := "https://downloads.openwrt.org/releases/24.10.1/targets/ath79/generic/openwrt-imagebuilder-24.10.1-ath79-generic.Linux-x86_64.tar.zst"
	if got != want {
		t.Errorf("24.10 URL:\n got %s\nwant %s", got, want)
	}
	if got := TarballURL("", "23.05.5", "ramips/mt7621"); !strings.HasSuffix(got, ".tar.xz") {
		t.Errorf("pre-24.10 should use .tar.xz: %s", got)
	}
	if got := TarballURL("http://mirror.local", "24.10.1", "x86/64"); !strings.HasPrefix(got, "http://mirror.local/releases/") {
		t.Errorf("mirror base not honored: %s", got)
	}
}

func TestStageFiles(t *testing.T) {
	dir := t.TempDir()
	agentBin := filepath.Join(dir, "agent.bin")
	os.WriteFile(agentBin, []byte("ELF-ish"), 0o644)

	filesDir := filepath.Join(dir, "files")
	cfg := &Config{AgentBinary: agentBin, Server: "https://cp.example.com", Code: "LG-AAAAA-BBBBB"}
	if err := StageFiles(filesDir, cfg); err != nil {
		t.Fatal(err)
	}

	bin, err := os.ReadFile(filepath.Join(filesDir, "usr/bin/logos-agent"))
	if err != nil || string(bin) != "ELF-ish" {
		t.Errorf("agent binary not staged: %v", err)
	}
	initData, err := os.ReadFile(filepath.Join(filesDir, "etc/init.d/logos-agent"))
	if err != nil || !strings.Contains(string(initData), "procd_open_instance") {
		t.Errorf("init script not staged: %v", err)
	}
	link, err := os.Readlink(filepath.Join(filesDir, "etc/rc.d/S95logos-agent"))
	if err != nil || link != "../init.d/logos-agent" {
		t.Errorf("rc.d symlink: %q, %v", link, err)
	}
	var ps map[string]string
	data, err := os.ReadFile(filepath.Join(filesDir, "etc/logos/preseed.json"))
	if err != nil || json.Unmarshal(data, &ps) != nil ||
		ps["server"] != "https://cp.example.com" || ps["code"] != "LG-AAAAA-BBBBB" {
		t.Errorf("preseed: %s, %v", data, err)
	}

	// Portal-only image: no preseed file.
	if err := StageFiles(filesDir, &Config{AgentBinary: agentBin}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(filesDir, "etc/logos/preseed.json")); !os.IsNotExist(err) {
		t.Error("preseed staged without --server/--code")
	}
}

func TestValidate(t *testing.T) {
	ok := Config{Release: "24.10.1", Target: "ath79/generic", Profile: "p", AgentBinary: "a"}
	if err := ok.validate(); err != nil {
		t.Errorf("valid config rejected: %v", err)
	}
	for name, bad := range map[string]Config{
		"no release":     {Target: "a/b", Profile: "p", AgentBinary: "a"},
		"bad target":     {Release: "24.10.1", Target: "ath79", Profile: "p", AgentBinary: "a"},
		"code no server": {Release: "24.10.1", Target: "a/b", Profile: "p", AgentBinary: "a", Code: "LG-X"},
	} {
		if err := bad.validate(); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

// TestBuildAgainstFakeImageBuilder runs the whole pipeline with a pre-seeded
// WorkDir containing a fake Image Builder whose Makefile writes a fake
// sysupgrade image — verifying make invocation, FILES staging and image
// collection without touching the network.
func TestBuildAgainstFakeImageBuilder(t *testing.T) {
	work := t.TempDir()
	ibDir := filepath.Join(work, "openwrt-imagebuilder-24.10.1-ath79-generic.Linux-x86_64")
	os.MkdirAll(ibDir, 0o755)
	makefile := `image:
	@test -n "$(PROFILE)" && test -d "$(FILES)"
	@test -x "$(FILES)/usr/bin/logos-agent"
	@mkdir -p bin/targets/ath79/generic
	@echo FAKEIMG > bin/targets/ath79/generic/openwrt-24.10.1-ath79-generic-$(PROFILE)-squashfs-sysupgrade.bin
`
	if err := os.WriteFile(filepath.Join(ibDir, "Makefile"), []byte(makefile), 0o644); err != nil {
		t.Fatal(err)
	}
	agentBin := filepath.Join(work, "agent.bin")
	os.WriteFile(agentBin, []byte("ELF"), 0o755)

	out := filepath.Join(work, "out")
	var log strings.Builder
	cfg := &Config{
		Release: "24.10.1", Target: "ath79/generic", Profile: "tplink_archer-c7-v5",
		AgentBinary: agentBin, Server: "http://cp:8080", Code: "LG-1",
		Packages: []string{"wireguard-tools"},
		WorkDir:  work, OutputDir: out, Out: &log,
	}
	if err := Build(context.Background(), cfg); err != nil {
		t.Fatalf("build: %v\n%s", err, log.String())
	}
	img := filepath.Join(out, "openwrt-24.10.1-ath79-generic-tplink_archer-c7-v5-squashfs-sysupgrade.bin")
	if data, err := os.ReadFile(img); err != nil || !strings.Contains(string(data), "FAKEIMG") {
		t.Errorf("image not collected: %v\n%s", err, log.String())
	}
	if !strings.Contains(log.String(), "PACKAGES=wireguard-tools") {
		t.Errorf("PACKAGES not passed to make:\n%s", log.String())
	}
}
