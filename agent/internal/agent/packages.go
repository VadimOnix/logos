package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

// Package management (F5) via the node's native manager: opkg on OpenWrt
// ≤23.05, apk on 24.10+/Alpine. Commands run directly (no shell), package
// names are validated, and output is captured for the panel.

type pkgManager struct {
	name string // "opkg" | "apk"
	bin  string
}

// detectPkgManager prefers opkg (present only on opkg-based OpenWrt);
// otherwise apk. LOGOS_PKG_MANAGER overrides for testing.
func detectPkgManager() (*pkgManager, error) {
	if forced := os.Getenv("LOGOS_PKG_MANAGER"); forced != "" {
		bin, err := exec.LookPath(forced)
		if err != nil {
			return nil, fmt.Errorf("LOGOS_PKG_MANAGER=%s: %w", forced, err)
		}
		return &pkgManager{name: forced, bin: bin}, nil
	}
	for _, name := range []string{"opkg", "apk"} {
		if bin, err := exec.LookPath(name); err == nil {
			return &pkgManager{name: name, bin: bin}, nil
		}
	}
	return nil, fmt.Errorf("no supported package manager found (opkg/apk)")
}

var pkgNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]*$`)

type pkgInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type pkgListResult struct {
	Manager  string    `json:"manager"`
	Packages []pkgInfo `json:"packages"`
}

type pkgActionResult struct {
	Manager  string `json:"manager"`
	ExitCode int    `json:"exit_code"`
	Output   string `json:"output"`
}

func handlePackagesList(ctx context.Context, _ json.RawMessage) (any, error) {
	pm, err := detectPkgManager()
	if err != nil {
		return nil, err
	}
	var out []byte
	switch pm.name {
	case "opkg":
		out, err = exec.CommandContext(ctx, pm.bin, "list-installed").Output()
	default: // apk
		out, err = exec.CommandContext(ctx, pm.bin, "list", "--installed").Output()
	}
	if err != nil {
		return nil, fmt.Errorf("%s list: %w", pm.name, err)
	}
	res := pkgListResult{Manager: pm.name, Packages: []pkgInfo{}}
	for _, line := range strings.Split(string(out), "\n") {
		if p, ok := parsePkgLine(pm.name, line); ok {
			res.Packages = append(res.Packages, p)
		}
	}
	return res, nil
}

// apkLineRe captures "name-version-rN" from apk list output; versions may
// themselves contain dashes, so anchor on the "-rN" release suffix.
var apkLineRe = regexp.MustCompile(`^(.+)-([^-]+-r\d+)$`)

func parsePkgLine(manager, line string) (pkgInfo, bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "WARNING") {
		return pkgInfo{}, false
	}
	switch manager {
	case "opkg":
		// "name - version" (description may follow after another " - ")
		parts := strings.SplitN(line, " - ", 3)
		if len(parts) < 2 {
			return pkgInfo{}, false
		}
		return pkgInfo{Name: parts[0], Version: parts[1]}, true
	default: // apk: "name-1.2.3-r0 arch {origin} (license) [installed]"
		first, _, _ := strings.Cut(line, " ")
		if m := apkLineRe.FindStringSubmatch(first); m != nil {
			return pkgInfo{Name: m[1], Version: m[2]}, true
		}
		return pkgInfo{}, false
	}
}

type pkgNameParams struct {
	Name string `json:"name"`
}

func pkgAction(ctx context.Context, params json.RawMessage, opkgVerb string, apkVerb string, needName bool) (any, error) {
	pm, err := detectPkgManager()
	if err != nil {
		return nil, err
	}
	args := []string{}
	switch pm.name {
	case "opkg":
		args = append(args, opkgVerb)
	default:
		args = append(args, apkVerb)
	}
	if needName {
		var p pkgNameParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, fmt.Errorf("invalid params: %w", err)
		}
		if !pkgNameRe.MatchString(p.Name) {
			return nil, fmt.Errorf("invalid package name %q", p.Name)
		}
		args = append(args, p.Name)
	}

	cmd := exec.CommandContext(ctx, pm.bin, args...)
	out, err := cmd.CombinedOutput()
	res := pkgActionResult{Manager: pm.name, Output: truncate(string(out), 64*1024)}
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			res.ExitCode = exitErr.ExitCode()
			// Non-zero exit is a result, not a transport error: the panel
			// shows the manager's own output.
			return res, nil
		}
		return nil, fmt.Errorf("%s %s: %w", pm.name, strings.Join(args, " "), err)
	}
	return res, nil
}

func handlePackagesInstall(ctx context.Context, params json.RawMessage) (any, error) {
	return pkgAction(ctx, params, "install", "add", true)
}

func handlePackagesRemove(ctx context.Context, params json.RawMessage) (any, error) {
	return pkgAction(ctx, params, "remove", "del", true)
}

func handlePackagesUpdate(ctx context.Context, params json.RawMessage) (any, error) {
	return pkgAction(ctx, params, "update", "update", false)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "\n…(truncated)"
}
