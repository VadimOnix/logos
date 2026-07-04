package adopt

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"net/netip"
	"os"
	"sort"
	"strings"
	"sync"
)

// F12 v1: fleet adoption. Adopt many routers in one run from a CSV inventory
// or an IP range, driving each locally over SSH with bounded concurrency.
// One unreachable or incompatible device never blocks the rest.

// Target is one router to adopt, with its own optional credential overrides.
type Target struct {
	Router   string
	User     string // "" → fleet default
	Password string // "" → fleet default
	KeyFile  string // "" → fleet default
}

// FleetResult records the outcome for one target.
type FleetResult struct {
	Router string
	Err    error
}

// ParseCSV reads a fleet inventory. The first non-empty line may be a header
// (recognized when its first field is "router"/"host"/"address"); columns are
// router,user,password,key with only router required. Blank cells inherit the
// fleet defaults. '#' comment lines are ignored.
func ParseCSV(r io.Reader) ([]Target, error) {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1 // ragged rows: trailing columns are optional
	cr.TrimLeadingSpace = true
	rows, err := cr.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("parse CSV: %w", err)
	}
	var targets []Target
	for i, row := range rows {
		if len(row) == 0 || strings.TrimSpace(row[0]) == "" || strings.HasPrefix(strings.TrimSpace(row[0]), "#") {
			continue
		}
		first := strings.ToLower(strings.TrimSpace(row[0]))
		if i == 0 && (first == "router" || first == "host" || first == "address") {
			continue // header
		}
		t := Target{Router: strings.TrimSpace(row[0])}
		if len(row) > 1 {
			t.User = strings.TrimSpace(row[1])
		}
		if len(row) > 2 {
			t.Password = strings.TrimSpace(row[2])
		}
		if len(row) > 3 {
			t.KeyFile = strings.TrimSpace(row[3])
		}
		targets = append(targets, t)
	}
	if len(targets) == 0 {
		return nil, fmt.Errorf("no routers found in the CSV")
	}
	return targets, nil
}

// ParseRange expands an IPv4 CIDR into per-host targets (network and
// broadcast addresses excluded for prefixes shorter than /31).
func ParseRange(cidr string) ([]Target, error) {
	p, err := netip.ParsePrefix(strings.TrimSpace(cidr))
	if err != nil {
		return nil, fmt.Errorf("invalid --range %q: %w", cidr, err)
	}
	if !p.Addr().Is4() {
		return nil, fmt.Errorf("--range must be IPv4")
	}
	p = p.Masked()
	if p.Bits() < 20 {
		return nil, fmt.Errorf("--range %s is too large (%d hosts); narrow it to /20 or smaller", p, 1<<(32-p.Bits()))
	}
	var targets []Target
	first, last := p.Addr().Next(), broadcast(p)
	if p.Bits() >= 31 { // /31 and /32: use every address
		first, last = p.Addr(), p.Addr().Next()
		if p.Bits() == 32 {
			last = first
		}
	}
	for a := first; p.Contains(a); a = a.Next() {
		if p.Bits() < 31 && a == broadcast(p) {
			break
		}
		targets = append(targets, Target{Router: a.String()})
		if a == last {
			break
		}
	}
	return targets, nil
}

func broadcast(p netip.Prefix) netip.Addr {
	b := p.Addr().As4()
	for i := p.Bits(); i < 32; i++ {
		b[i/8] |= 1 << (7 - i%8)
	}
	return netip.AddrFrom4(b)
}

// FleetOptions is the shared configuration for a fleet run. Per-target
// credentials from the CSV override User/Password/KeyFile.
type FleetOptions struct {
	Targets  []Target
	User     string // default ssh user
	Password string // default ssh password
	KeyFile  string // default ssh key
	Server   string
	// MintCode supplies a fresh single-use claim code per router, called
	// lazily during adoption (only after the device passes its checks) so a
	// scan never wastes codes on unreachable hosts.
	MintCode    func(ctx context.Context, router string) (string, error)
	AgentBinary string
	Force       bool
	Concurrency int
}

// adoptFn is the per-target adoption call, overridable in tests.
var adoptFn = Adopt

// AdoptFleet adopts every target with bounded concurrency and returns a
// per-target result. Adoption of one device never aborts the others; the
// caller decides how to treat partial failure.
func AdoptFleet(ctx context.Context, opts FleetOptions, out io.Writer) ([]FleetResult, error) {
	if len(opts.Targets) == 0 {
		return nil, fmt.Errorf("no targets")
	}
	if opts.MintCode == nil {
		return nil, fmt.Errorf("MintCode is required (each router needs its own single-use claim code)")
	}
	conc := opts.Concurrency
	if conc < 1 {
		conc = 4
	}

	results := make([]FleetResult, len(opts.Targets))
	var mu sync.Mutex
	sem := make(chan struct{}, conc)
	var wg sync.WaitGroup

	for i, t := range opts.Targets {
		wg.Add(1)
		go func(i int, t Target) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				results[i] = FleetResult{Router: t.Router, Err: ctx.Err()}
				return
			}
			defer func() { <-sem }()

			one := Options{
				RouterAddr:  t.Router,
				User:        orDefault(t.User, opts.User),
				Password:    orDefault(t.Password, opts.Password),
				KeyFile:     orDefault(t.KeyFile, opts.KeyFile),
				Server:      opts.Server,
				CodeFunc:    func(c context.Context) (string, error) { return opts.MintCode(c, t.Router) },
				AgentBinary: opts.AgentBinary,
				Force:       opts.Force,
			}
			var buf strings.Builder
			err := adoptFn(ctx, one, &buf)

			mu.Lock()
			results[i] = FleetResult{Router: t.Router, Err: err}
			status := "OK"
			if err != nil {
				status = "FAILED: " + err.Error()
			}
			fmt.Fprintf(out, "[%s] %s\n", t.Router, status)
			mu.Unlock()
		}(i, t)
	}
	wg.Wait()

	sort.SliceStable(results, func(a, b int) bool { return results[a].Router < results[b].Router })
	ok, failed := 0, 0
	for _, r := range results {
		if r.Err == nil {
			ok++
		} else {
			failed++
		}
	}
	fmt.Fprintf(out, "\nfleet adoption complete: %d succeeded, %d failed\n", ok, failed)
	if failed > 0 {
		return results, fmt.Errorf("%d of %d routers failed", failed, len(results))
	}
	return results, nil
}

func orDefault(v, def string) string {
	if v != "" {
		return v
	}
	return def
}

// LoadCSVFile is a convenience wrapper for the CLI.
func LoadCSVFile(path string) ([]Target, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return ParseCSV(f)
}
