package api

import "testing"

func TestPackageMethod(t *testing.T) {
	for _, tc := range []struct {
		action, name string
		want         string
		wantErr      bool
	}{
		{"install", "curl", "packages.install", false},
		{"remove", "curl", "packages.remove", false},
		{"update", "", "packages.update", false}, // update needs no name
		{"update", "curl", "packages.update", false},
		{"install", "", "", true}, // name required
		{"remove", "", "", true},
		{"upgrade", "curl", "", true}, // unknown action
		{"", "", "", true},
	} {
		got, err := packageMethod(tc.action, tc.name)
		if (err != nil) != tc.wantErr || got != tc.want {
			t.Errorf("packageMethod(%q, %q) = %q, %v; want %q, err=%v",
				tc.action, tc.name, got, err, tc.want, tc.wantErr)
		}
	}
}
