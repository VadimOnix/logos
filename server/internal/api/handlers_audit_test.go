package api

import "testing"

func TestAuditLimit(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want int
	}{
		{"", 100},     // default
		{"50", 50},    // explicit
		{"1", 1},      // lower bound
		{"500", 500},  // upper bound
		{"9999", 500}, // clamped
		{"0", 100},    // invalid → default
		{"-5", 100},
		{"lots", 100},
	} {
		if got := auditLimit(tc.in); got != tc.want {
			t.Errorf("auditLimit(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}
