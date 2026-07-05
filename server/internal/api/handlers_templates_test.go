package api

import "testing"

func TestSubstVars(t *testing.T) {
	vars := map[string]string{"ip": "192.168.5.1", "node.name": "office"}
	for _, tc := range []struct {
		in, want string
		wantErr  bool
	}{
		{"network.lan.ipaddr", "network.lan.ipaddr", false}, // no placeholders
		{"${ip}", "192.168.5.1", false},
		{"host-${node.name}", "host-office", false},
		{"${ip}/${node.name}", "192.168.5.1/office", false},
		{"${missing}", "", true}, // undefined must fail, not pass through
	} {
		got, err := substVars(tc.in, vars)
		if (err != nil) != tc.wantErr || got != tc.want {
			t.Errorf("substVars(%q) = %q, %v; want %q, err=%v", tc.in, got, err, tc.want, tc.wantErr)
		}
	}
}

func TestRenderTemplate(t *testing.T) {
	body := []uciChangeReq{
		{Op: "set", Key: "system.@system[0].hostname", Value: "${node.name}"},
		{Op: "set", Key: "network.lan.ipaddr", Value: "${lan_ip}"},
		{Op: "delete", Key: "firewall.guest_${node.id}"},
	}
	vars := map[string]string{"node.name": "office", "node.id": "abc123", "lan_ip": "192.168.7.1"}

	out, err := renderTemplate(body, vars)
	if err != nil {
		t.Fatal(err)
	}
	if out[0].Value != "office" || out[1].Value != "192.168.7.1" || out[2].Key != "firewall.guest_abc123" {
		t.Errorf("rendered: %+v", out)
	}
	// The template itself must stay untouched (render returns a copy).
	if body[0].Value != "${node.name}" {
		t.Error("render mutated the template body")
	}

	if _, err := renderTemplate(body, map[string]string{"node.name": "x", "node.id": "y"}); err == nil {
		t.Error("missing lan_ip accepted")
	}
}

func TestValidateTemplateBody(t *testing.T) {
	if err := validateTemplateBody(nil); err == nil {
		t.Error("empty body accepted")
	}
	if err := validateTemplateBody([]uciChangeReq{{Op: "frobnicate", Key: "a.b"}}); err == nil {
		t.Error("unknown op accepted")
	}
	if err := validateTemplateBody([]uciChangeReq{{Op: "set", Key: ""}}); err == nil {
		t.Error("empty key accepted")
	}
	if err := validateTemplateBody([]uciChangeReq{{Op: "set", Key: "a.b", Value: "${v}"}}); err != nil {
		t.Errorf("valid body rejected: %v", err)
	}
}
