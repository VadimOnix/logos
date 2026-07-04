package api

import "testing"

func TestParseResize(t *testing.T) {
	cols, rows, ok := parseResize([]byte(`{"resize":[120,40]}`))
	if !ok || cols != 120 || rows != 40 {
		t.Errorf("resize parse: %d %d %v", cols, rows, ok)
	}
	for _, notResize := range []string{`ls -la`, `{"resize":[1]}`, `{"other":1}`, ``, `{"resize":"x"}`} {
		if _, _, ok := parseResize([]byte(notResize)); ok {
			t.Errorf("%q parsed as resize", notResize)
		}
	}
}
