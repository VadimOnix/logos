package adopt

import "testing"

func TestParseChecksums(t *testing.T) {
	out := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855  /etc/config/network\n" +
		"5891b5b522d5df086d0ff0b110fbd9d21bb4fc7163af34d08286a2e846f6be03  /etc/config/firewall\n" +
		"sha256sum: /etc/config/missing: No such file or directory\n"
	sums := parseChecksums(out)
	if len(sums) != 2 {
		t.Fatalf("want 2 entries, got %v", sums)
	}
	if sums["/etc/config/network"] != "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" {
		t.Errorf("network sum: %q", sums["/etc/config/network"])
	}
	if parseChecksums("no checksums here\n") != nil {
		t.Error("expected nil for checksum-free output")
	}
}
