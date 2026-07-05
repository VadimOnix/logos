package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
)

// F4 step 1: read-only UCI access. `uci export` gives the full (or per-config)
// state snapshot the server will later diff against desired state. Write
// operations (set/commit with server-side versioning and rollback) are the
// next roadmap slice and intentionally absent here.

var uciConfigRe = regexp.MustCompile(`^[a-z0-9_-]+$`)

// configHash fingerprints the full `uci export` output (sha256 hex) — the
// drift signal reported in heartbeats and after confirmed changes, so the
// server can tell when config was modified outside Logos (e.g. local LuCI).
// Empty on non-UCI systems or on error.
func configHash(ctx context.Context) string {
	bin, err := exec.LookPath("uci")
	if err != nil {
		return ""
	}
	out, err := exec.CommandContext(ctx, bin, "export").Output()
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(out)
	return hex.EncodeToString(sum[:])
}

type uciExportParams struct {
	Config string `json:"config,omitempty"`
}

type uciExportResult struct {
	Config string `json:"config,omitempty"`
	Export string `json:"export"`
}

func handleUCIExport(ctx context.Context, params json.RawMessage) (any, error) {
	bin, err := exec.LookPath("uci")
	if err != nil {
		return nil, fmt.Errorf("uci is not available on this node (not OpenWrt?)")
	}
	var p uciExportParams
	if len(params) > 0 {
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, fmt.Errorf("invalid params: %w", err)
		}
	}
	args := []string{"export"}
	if p.Config != "" {
		if !uciConfigRe.MatchString(p.Config) {
			return nil, fmt.Errorf("invalid config name %q", p.Config)
		}
		args = append(args, p.Config)
	}
	out, err := exec.CommandContext(ctx, bin, args...).Output()
	if err != nil {
		return nil, fmt.Errorf("uci export: %w", err)
	}
	return uciExportResult{Config: p.Config, Export: truncate(string(out), 256*1024)}, nil
}
