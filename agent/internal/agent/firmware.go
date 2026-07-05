package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// Firmware upgrade orchestration (v1.0, PRD §5.2 "sysupgrade"). The agent
// downloads the image, verifies its sha256 — nothing is ever flashed on a
// hash mismatch — replies to the RPC, and only then hands off to sysupgrade:
// the reply must leave before the device reboots. Config is preserved by
// default (`sysupgrade` without -n), so the node re-enrolls into nothing —
// it just reconnects with its existing identity after the flash.

// firmwareMaxBytes caps the download; OpenWrt images are tens of MB.
const firmwareMaxBytes = 256 << 20

// Vars (not consts) so tests can redirect the image path and shrink the
// reply-before-flash grace period.
var (
	firmwareImagePath = "/tmp/logos-sysupgrade.img"
	// firmwareFlashDelay is the grace between replying to the RPC and
	// executing sysupgrade, so the result reaches the server first.
	firmwareFlashDelay = 3 * time.Second
)

var firmwareSHARe = regexp.MustCompile(`^[0-9a-f]{64}$`)

type firmwareUpgradeParams struct {
	URL        string `json:"url"`
	SHA256     string `json:"sha256"`
	KeepConfig *bool  `json:"keep_config,omitempty"` // default true
}

// execSysupgrade is swapped out by tests; the default detaches sysupgrade
// from the agent process (sysupgrade kills userland on its way down).
var execSysupgrade = func(bin string, args []string) error {
	cmd := exec.Command(bin, args...)
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Start()
}

func handleFirmwareUpgrade(ctx context.Context, params json.RawMessage) (any, error) {
	var p firmwareUpgradeParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	if !strings.HasPrefix(p.URL, "http://") && !strings.HasPrefix(p.URL, "https://") {
		return nil, fmt.Errorf("url must be http(s)")
	}
	p.SHA256 = strings.ToLower(strings.TrimSpace(p.SHA256))
	if !firmwareSHARe.MatchString(p.SHA256) {
		return nil, fmt.Errorf("sha256 must be 64 hex characters")
	}
	bin, err := exec.LookPath("sysupgrade")
	if err != nil {
		return nil, fmt.Errorf("sysupgrade is not available on this node (not OpenWrt?)")
	}

	if err := downloadAndVerify(ctx, p.URL, p.SHA256, firmwareImagePath); err != nil {
		return nil, err
	}

	args := []string{}
	if p.KeepConfig != nil && !*p.KeepConfig {
		args = append(args, "-n")
	}
	args = append(args, firmwareImagePath)

	// Reply first, flash after a grace period: sysupgrade takes the device
	// down, and the "flashing" result must reach the server before that.
	go func() {
		time.Sleep(firmwareFlashDelay)
		slog.Warn("executing sysupgrade", "image", firmwareImagePath)
		if err := execSysupgrade(bin, args); err != nil {
			slog.Error("sysupgrade failed to start", "err", err)
		}
	}()
	return map[string]string{"status": "flashing", "sha256": p.SHA256}, nil
}

// downloadAndVerify streams the image to disk and checks its sha256; on any
// failure the partial file is removed so a bad image can never linger.
func downloadAndVerify(ctx context.Context, url, wantSHA, path string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download: server returned %s", resp.Status)
	}

	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, firmwareMaxBytes+1))
	cerr := f.Close()
	if err == nil {
		err = cerr
	}
	if err == nil && n > firmwareMaxBytes {
		err = fmt.Errorf("image exceeds %d MB limit", firmwareMaxBytes>>20)
	}
	if err == nil {
		if got := hex.EncodeToString(h.Sum(nil)); got != wantSHA {
			err = fmt.Errorf("sha256 mismatch: image is %s, expected %s — refusing to flash", got, wantSHA)
		}
	}
	if err != nil {
		os.Remove(path)
		return err
	}
	return nil
}
