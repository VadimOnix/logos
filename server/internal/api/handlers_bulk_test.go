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

func TestSplitCanary(t *testing.T) {
	ids := []string{"a", "b", "c", "d"}
	for _, tc := range []struct {
		canary              int
		wantFirst, wantRest int
	}{
		{0, 4, 0}, // staging off → one batch
		{1, 1, 3}, // classic canary
		{3, 3, 1},
		{4, 4, 0},  // canary covers everything → one batch
		{99, 4, 0}, // more than the fleet → one batch
	} {
		first, rest := splitCanary(ids, tc.canary)
		if len(first) != tc.wantFirst || len(rest) != tc.wantRest {
			t.Errorf("canary=%d: got %d/%d, want %d/%d",
				tc.canary, len(first), len(rest), tc.wantFirst, tc.wantRest)
		}
	}
	if first, rest := splitCanary(nil, 1); len(first) != 0 || rest != nil {
		t.Errorf("empty list: got %v/%v", first, rest)
	}
}
