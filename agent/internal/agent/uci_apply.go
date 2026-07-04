package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// F4 write path: apply UCI changes with an auto-revert watchdog.
//
// Safety model (PRD §6 Resilience, §9 "never brick a device"):
//  1. Snapshot every affected config (`uci export`).
//  2. Persist the snapshot + deadline to a pending-revert file BEFORE
//     mutating anything — a crash or reboot mid-apply reverts on next start.
//  3. Apply set/delete, commit, reload services.
//  4. If the server does not confirm within the window (its confirmation
//     proves the management channel survived the change), restore the
//     snapshot. Confirmation deletes the pending file and stops the timer.

type uciChange struct {
	Op    string `json:"op"` // "set" | "delete"
	Key   string `json:"key"`
	Value string `json:"value,omitempty"`
}

type uciApplyParams struct {
	ApplyID          string      `json:"apply_id"`
	Changes          []uciChange `json:"changes"`
	RevertTimeoutSec int         `json:"revert_timeout_sec"`
}

type uciRestoreParams struct {
	ApplyID          string            `json:"apply_id"`
	Snapshots        map[string]string `json:"snapshots"` // config → uci export text
	RevertTimeoutSec int               `json:"revert_timeout_sec"`
}

type uciApplyResult struct {
	ApplyID   string            `json:"apply_id"`
	Configs   []string          `json:"configs"`
	Snapshots map[string]string `json:"snapshots"`
}

// pendingRevert is what survives a crash: enough to restore the snapshot.
type pendingRevert struct {
	ApplyID   string            `json:"apply_id"`
	Deadline  time.Time         `json:"deadline"`
	Snapshots map[string]string `json:"snapshots"`
}

// uciKeyRe matches "config.section" or "config.section.option"; sections may
// be named or positional (@type[0]).
var uciKeyRe = regexp.MustCompile(`^[a-z0-9_-]+\.(@[a-z0-9_-]+\[-?\d+\]|[A-Za-z0-9_-]+)(\.[A-Za-z0-9_-]+)?$`)

const (
	defaultRevertTimeout = 90 * time.Second
	minRevertTimeout     = 15 * time.Second
	maxRevertTimeout     = 10 * time.Minute
	maxChangesPerApply   = 200
	maxValueLen          = 4096
)

// pendingGuard serializes apply/confirm/restore and owns the watchdog timer.
var pendingGuard struct {
	sync.Mutex
	path  string // pending-revert file location, set by SetStateDir
	timer *time.Timer
	log   *slog.Logger
}

// SetStateDir tells the UCI machinery where to keep the pending-revert file
// (next to the agent state). Called by Run before connecting.
func SetStateDir(dir string, log *slog.Logger) {
	pendingGuard.Lock()
	defer pendingGuard.Unlock()
	pendingGuard.path = filepath.Join(dir, "pending-revert.json")
	pendingGuard.log = log
}

// RevertPendingOnStart restores a leftover pending snapshot. Called at agent
// startup: a pending file can only exist if a previous apply was never
// confirmed (crash, reboot, or lost channel) — so the change must be undone.
func RevertPendingOnStart() error {
	pendingGuard.Lock()
	defer pendingGuard.Unlock()
	p, err := readPendingLocked()
	if err != nil || p == nil {
		return err
	}
	logw("unconfirmed config change found at startup; reverting", "apply_id", p.ApplyID)
	return revertLocked(p)
}

func logw(msg string, args ...any) {
	if pendingGuard.log != nil {
		pendingGuard.log.Warn(msg, args...)
	}
}

func readPendingLocked() (*pendingRevert, error) {
	if pendingGuard.path == "" {
		return nil, fmt.Errorf("state dir not configured")
	}
	data, err := os.ReadFile(pendingGuard.path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var p pendingRevert
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("corrupt pending-revert file: %w", err)
	}
	return &p, nil
}

func writePendingLocked(p *pendingRevert) error {
	data, err := json.Marshal(p)
	if err != nil {
		return err
	}
	tmp := pendingGuard.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, pendingGuard.path)
}

func clearPendingLocked() {
	if pendingGuard.timer != nil {
		pendingGuard.timer.Stop()
		pendingGuard.timer = nil
	}
	os.Remove(pendingGuard.path)
}

// revertLocked restores snapshots via `uci import` and clears pending state.
func revertLocked(p *pendingRevert) error {
	uciBin, err := exec.LookPath("uci")
	if err != nil {
		return fmt.Errorf("uci not available for revert: %w", err)
	}
	var firstErr error
	for config, export := range p.Snapshots {
		if err := uciImport(uciBin, config, export); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	clearPendingLocked()
	reloadServices()
	if firstErr != nil {
		return fmt.Errorf("revert incomplete: %w", firstErr)
	}
	logw("configuration reverted to pre-change snapshot", "apply_id", p.ApplyID)
	return nil
}

func uciImport(uciBin, config, export string) error {
	tmp, err := os.CreateTemp("", "logos-uci-*.conf")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(export); err != nil {
		tmp.Close()
		return err
	}
	tmp.Close()
	if out, err := exec.Command(uciBin, "-f", tmp.Name(), "import", config).CombinedOutput(); err != nil {
		return fmt.Errorf("uci import %s: %v: %s", config, err, out)
	}
	if out, err := exec.Command(uciBin, "commit", config).CombinedOutput(); err != nil {
		return fmt.Errorf("uci commit %s: %v: %s", config, err, out)
	}
	return nil
}

// reloadServices asks procd to reload services whose config changed.
// Best-effort: absent outside OpenWrt.
func reloadServices() {
	if bin, err := exec.LookPath("reload_config"); err == nil {
		if out, err := exec.Command(bin).CombinedOutput(); err != nil {
			logw("reload_config failed", "err", err, "out", string(out))
		}
	}
}

func validateChanges(changes []uciChange) ([]string, error) {
	if len(changes) == 0 {
		return nil, fmt.Errorf("no changes given")
	}
	if len(changes) > maxChangesPerApply {
		return nil, fmt.Errorf("too many changes in one apply (max %d)", maxChangesPerApply)
	}
	seen := map[string]bool{}
	var configs []string
	for i, ch := range changes {
		if !uciKeyRe.MatchString(ch.Key) {
			return nil, fmt.Errorf("change %d: invalid uci key %q", i, ch.Key)
		}
		switch ch.Op {
		case "set":
			if len(ch.Value) > maxValueLen || strings.ContainsAny(ch.Value, "\x00\n\r") {
				return nil, fmt.Errorf("change %d: invalid value for %s", i, ch.Key)
			}
		case "delete":
			if ch.Value != "" {
				return nil, fmt.Errorf("change %d: delete takes no value", i)
			}
		default:
			return nil, fmt.Errorf("change %d: op must be \"set\" or \"delete\"", i)
		}
		config, _, _ := strings.Cut(ch.Key, ".")
		if !seen[config] {
			seen[config] = true
			configs = append(configs, config)
		}
	}
	return configs, nil
}

func clampRevertTimeout(sec int) time.Duration {
	d := time.Duration(sec) * time.Second
	if sec == 0 {
		return defaultRevertTimeout
	}
	if d < minRevertTimeout {
		return minRevertTimeout
	}
	if d > maxRevertTimeout {
		return maxRevertTimeout
	}
	return d
}

func snapshotConfigs(ctx context.Context, uciBin string, configs []string) (map[string]string, error) {
	snaps := make(map[string]string, len(configs))
	for _, c := range configs {
		out, err := exec.CommandContext(ctx, uciBin, "export", c).Output()
		if err != nil {
			return nil, fmt.Errorf("uci export %s: %w", c, err)
		}
		snaps[c] = string(out)
	}
	return snaps, nil
}

// armWatchdogLocked persists the pending file and schedules the revert.
func armWatchdogLocked(p *pendingRevert) error {
	if err := writePendingLocked(p); err != nil {
		return fmt.Errorf("persist pending-revert state: %w", err)
	}
	pendingGuard.timer = time.AfterFunc(time.Until(p.Deadline), func() {
		pendingGuard.Lock()
		defer pendingGuard.Unlock()
		cur, err := readPendingLocked()
		if err != nil || cur == nil || cur.ApplyID != p.ApplyID {
			return // confirmed or superseded in the meantime
		}
		logw("config change not confirmed in time; auto-reverting", "apply_id", p.ApplyID)
		if err := revertLocked(cur); err != nil {
			logw("auto-revert failed", "apply_id", p.ApplyID, "err", err)
		}
	})
	return nil
}

func handleUCIApply(ctx context.Context, params json.RawMessage) (any, error) {
	var p uciApplyParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	if p.ApplyID == "" {
		return nil, fmt.Errorf("apply_id is required")
	}
	configs, err := validateChanges(p.Changes)
	if err != nil {
		return nil, err
	}
	uciBin, err := exec.LookPath("uci")
	if err != nil {
		return nil, fmt.Errorf("uci is not available on this node (not OpenWrt?)")
	}

	pendingGuard.Lock()
	defer pendingGuard.Unlock()
	if cur, err := readPendingLocked(); err != nil {
		return nil, err
	} else if cur != nil {
		return nil, fmt.Errorf("another config change (%s) is awaiting confirmation", cur.ApplyID)
	}

	snaps, err := snapshotConfigs(ctx, uciBin, configs)
	if err != nil {
		return nil, err
	}
	pending := &pendingRevert{
		ApplyID:   p.ApplyID,
		Deadline:  time.Now().Add(clampRevertTimeout(p.RevertTimeoutSec)),
		Snapshots: snaps,
	}
	if err := armWatchdogLocked(pending); err != nil {
		return nil, err
	}

	// Mutate. Any failure reverts immediately — an apply is all-or-nothing.
	for _, ch := range p.Changes {
		var out []byte
		switch ch.Op {
		case "set":
			out, err = exec.CommandContext(ctx, uciBin, "set", ch.Key+"="+ch.Value).CombinedOutput()
		case "delete":
			out, err = exec.CommandContext(ctx, uciBin, "delete", ch.Key).CombinedOutput()
		}
		if err != nil {
			revertLocked(pending)
			return nil, fmt.Errorf("uci %s %s: %v: %s", ch.Op, ch.Key, err, out)
		}
	}
	for _, c := range configs {
		if out, err := exec.CommandContext(ctx, uciBin, "commit", c).CombinedOutput(); err != nil {
			revertLocked(pending)
			return nil, fmt.Errorf("uci commit %s: %v: %s", c, err, out)
		}
	}
	reloadServices()

	return uciApplyResult{ApplyID: p.ApplyID, Configs: configs, Snapshots: snaps}, nil
}

type uciConfirmParams struct {
	ApplyID string `json:"apply_id"`
}

// handleUCIConfirm makes an applied change permanent: the server only calls
// this once it can still reach the node, which is the connectivity proof.
func handleUCIConfirm(_ context.Context, params json.RawMessage) (any, error) {
	var p uciConfirmParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	pendingGuard.Lock()
	defer pendingGuard.Unlock()
	cur, err := readPendingLocked()
	if err != nil {
		return nil, err
	}
	if cur == nil || cur.ApplyID != p.ApplyID {
		return nil, fmt.Errorf("no pending apply with id %q", p.ApplyID)
	}
	clearPendingLocked()
	return map[string]string{"confirmed": p.ApplyID}, nil
}

// handleUCIRestore applies stored snapshots (server-side rollback of a past
// change). Runs through the same pending/watchdog machinery as apply: a
// rollback that cuts connectivity un-rolls itself back.
func handleUCIRestore(ctx context.Context, params json.RawMessage) (any, error) {
	var p uciRestoreParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	if p.ApplyID == "" || len(p.Snapshots) == 0 {
		return nil, fmt.Errorf("apply_id and snapshots are required")
	}
	uciBin, err := exec.LookPath("uci")
	if err != nil {
		return nil, fmt.Errorf("uci is not available on this node (not OpenWrt?)")
	}
	configs := make([]string, 0, len(p.Snapshots))
	for c := range p.Snapshots {
		if !regexp.MustCompile(`^[a-z0-9_-]+$`).MatchString(c) {
			return nil, fmt.Errorf("invalid config name %q", c)
		}
		configs = append(configs, c)
	}

	pendingGuard.Lock()
	defer pendingGuard.Unlock()
	if cur, err := readPendingLocked(); err != nil {
		return nil, err
	} else if cur != nil {
		return nil, fmt.Errorf("another config change (%s) is awaiting confirmation", cur.ApplyID)
	}

	snaps, err := snapshotConfigs(ctx, uciBin, configs)
	if err != nil {
		return nil, err
	}
	pending := &pendingRevert{
		ApplyID:   p.ApplyID,
		Deadline:  time.Now().Add(clampRevertTimeout(p.RevertTimeoutSec)),
		Snapshots: snaps,
	}
	if err := armWatchdogLocked(pending); err != nil {
		return nil, err
	}

	for c, export := range p.Snapshots {
		if err := uciImport(uciBin, c, export); err != nil {
			revertLocked(pending)
			return nil, err
		}
	}
	reloadServices()

	return uciApplyResult{ApplyID: p.ApplyID, Configs: configs, Snapshots: snaps}, nil
}

// PendingApplyID reports an unconfirmed change, if any — sent in hello so a
// reconnect can confirm a change that survived a channel drop.
func PendingApplyID() string {
	pendingGuard.Lock()
	defer pendingGuard.Unlock()
	p, err := readPendingLocked()
	if err != nil || p == nil {
		return ""
	}
	return p.ApplyID
}
