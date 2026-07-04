package adopt

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
)

func routers(ts []Target) []string {
	out := make([]string, len(ts))
	for i, t := range ts {
		out[i] = t.Router
	}
	return out
}

func TestParseCSV(t *testing.T) {
	in := `router,user,password,key
192.168.1.1,root,secret,
192.168.1.2,,,~/.ssh/id_ed25519
# a comment line
10.0.0.5
`
	got, err := ParseCSV(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("want 3 targets, got %d: %+v", len(got), got)
	}
	if got[0] != (Target{Router: "192.168.1.1", User: "root", Password: "secret"}) {
		t.Errorf("row 0: %+v", got[0])
	}
	if got[1].KeyFile != "~/.ssh/id_ed25519" || got[1].User != "" {
		t.Errorf("row 1: %+v", got[1])
	}
	if got[2].Router != "10.0.0.5" {
		t.Errorf("row 2: %+v", got[2])
	}

	if _, err := ParseCSV(strings.NewReader("# only comments\n\n")); err == nil {
		t.Error("empty CSV accepted")
	}
}

func TestParseRange(t *testing.T) {
	got, err := ParseRange("192.168.1.0/29") // hosts .1–.6
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"192.168.1.1", "192.168.1.2", "192.168.1.3", "192.168.1.4", "192.168.1.5", "192.168.1.6"}
	if strings.Join(routers(got), ",") != strings.Join(want, ",") {
		t.Errorf("range hosts:\n got %v\nwant %v", routers(got), want)
	}

	single, err := ParseRange("10.0.0.7/32")
	if err != nil || len(single) != 1 || single[0].Router != "10.0.0.7" {
		t.Errorf("/32 = %v, %v", routers(single), err)
	}

	if _, err := ParseRange("10.0.0.0/8"); err == nil {
		t.Error("oversized range accepted")
	}
	if _, err := ParseRange("nonsense"); err == nil {
		t.Error("garbage range accepted")
	}
}

func TestAdoptFleet(t *testing.T) {
	// Swap the per-target adoption for a stub: .3 fails, others succeed.
	orig := adoptFn
	defer func() { adoptFn = orig }()
	var mintedMu sync.Mutex
	minted := map[string]bool{}
	adoptFn = func(ctx context.Context, opts Options, out io.Writer) error {
		// Exercise lazy code minting: only reached for "adoptable" hosts.
		if _, err := opts.CodeFunc(ctx); err != nil {
			return err
		}
		if strings.HasSuffix(opts.RouterAddr, ".3") {
			return fmt.Errorf("compatibility check failed")
		}
		return nil
	}

	targets := []Target{{Router: "10.0.0.1"}, {Router: "10.0.0.2"}, {Router: "10.0.0.3"}}
	var out strings.Builder
	res, err := AdoptFleet(context.Background(), FleetOptions{
		Targets:     targets,
		Server:      "http://cp:8080",
		Concurrency: 2,
		MintCode: func(_ context.Context, router string) (string, error) {
			mintedMu.Lock()
			minted[router] = true
			mintedMu.Unlock()
			return "LG-" + router, nil
		},
	}, &out)
	if err == nil {
		t.Error("fleet with a failing host should return an error")
	}
	if len(res) != 3 {
		t.Fatalf("want 3 results, got %d", len(res))
	}
	// Results are sorted by router.
	if res[2].Router != "10.0.0.3" || res[2].Err == nil {
		t.Errorf(".3 should have failed: %+v", res[2])
	}
	if res[0].Err != nil || res[1].Err != nil {
		t.Errorf("healthy hosts failed: %+v", res)
	}
	if !strings.Contains(out.String(), "2 succeeded, 1 failed") {
		t.Errorf("summary missing:\n%s", out.String())
	}
	if len(minted) != 3 {
		t.Errorf("expected a code minted per host, got %v", minted)
	}
}

func TestAdoptFleetRequiresMinter(t *testing.T) {
	_, err := AdoptFleet(context.Background(),
		FleetOptions{Targets: []Target{{Router: "x"}}}, &strings.Builder{})
	if err == nil || !strings.Contains(err.Error(), "MintCode is required") {
		t.Errorf("err = %v", err)
	}
}
