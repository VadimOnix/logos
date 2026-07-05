package api

import "testing"

func TestConfigDrift(t *testing.T) {
	base := "aaa111"
	metricsWith := func(h string) []byte {
		if h == "" {
			return []byte(`{"load1":0.1}`)
		}
		return []byte(`{"load1":0.1,"config_hash":"` + h + `"}`)
	}

	if configDrift(nil, metricsWith("bbb222")) {
		t.Error("drift reported with no baseline")
	}
	if configDrift(&base, metricsWith("")) {
		t.Error("drift reported with no live hash (non-UCI node)")
	}
	if configDrift(&base, nil) {
		t.Error("drift reported with no metrics at all")
	}
	if configDrift(&base, metricsWith("aaa111")) {
		t.Error("drift reported when hashes match")
	}
	if !configDrift(&base, metricsWith("bbb222")) {
		t.Error("real drift not detected")
	}
}
