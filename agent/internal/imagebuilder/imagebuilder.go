// Package imagebuilder wraps the official OpenWrt Image Builder (F14): it
// bakes logos-agent, its procd service, and an optional enrollment preseed
// into a sysupgrade image, so a router flashed with the result either
// enrolls itself on first boot (preseed) or comes up serving the setup
// portal (F2).
package imagebuilder

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/VadimOnix/logos/agent/internal/initscript"
)

type Config struct {
	Release string // OpenWrt release, e.g. "24.10.1"
	Target  string // e.g. "ath79/generic"
	Profile string // device profile, e.g. "tplink_archer-c7-v5"

	AgentBinary string // path to a logos-agent built for the target arch
	InitScript  string // path to the procd init script ("" = embedded copy)

	// Preseed: when both are set, the image auto-enrolls on first boot.
	Server string
	Code   string

	Packages  []string // extra packages, e.g. wireguard-tools
	OutputDir string
	WorkDir   string // where the Image Builder is unpacked (kept for reuse)

	// DownloadBase overrides https://downloads.openwrt.org (tests, mirrors).
	DownloadBase string
	// Make overrides the make binary (tests).
	Make string

	Out io.Writer // progress messages; defaults to os.Stderr
}

func (c *Config) out() io.Writer {
	if c.Out != nil {
		return c.Out
	}
	return os.Stderr
}

func (c *Config) validate() error {
	switch {
	case c.Release == "":
		return fmt.Errorf("--release is required (e.g. 24.10.1)")
	case !strings.Contains(c.Target, "/"):
		return fmt.Errorf("--target must be <target>/<subtarget>, e.g. ath79/generic")
	case c.Profile == "":
		return fmt.Errorf("--profile is required (see `make info` in the Image Builder, or the OpenWrt firmware selector)")
	case c.AgentBinary == "":
		return fmt.Errorf("--agent-binary is required (build with `make agent` for the device arch)")
	case (c.Server == "") != (c.Code == ""):
		return fmt.Errorf("--server and --code go together (both for auto-enrollment, neither for portal-only)")
	}
	return nil
}

// TarballURL builds the download URL for the release's Image Builder.
// OpenWrt switched the archive format from .tar.xz to .tar.zst in 24.10.
func TarballURL(base, release, target string) string {
	if base == "" {
		base = "https://downloads.openwrt.org"
	}
	ext := "tar.zst"
	if major(release) < 24 {
		ext = "tar.xz"
	}
	return fmt.Sprintf("%s/releases/%s/targets/%s/openwrt-imagebuilder-%s-%s.Linux-x86_64.%s",
		base, release, target, release, strings.ReplaceAll(target, "/", "-"), ext)
}

func major(release string) int {
	head, _, _ := strings.Cut(release, ".")
	n, err := strconv.Atoi(head)
	if err != nil {
		return 0
	}
	return n
}

// Build downloads (or reuses) the Image Builder, stages the files overlay,
// and produces sysupgrade images in cfg.OutputDir.
func Build(ctx context.Context, cfg *Config) error {
	if err := cfg.validate(); err != nil {
		return err
	}
	if cfg.WorkDir == "" {
		cfg.WorkDir = filepath.Join(os.TempDir(), "logos-imagebuilder")
	}
	if cfg.OutputDir == "" {
		cfg.OutputDir = "."
	}

	ibDir, err := ensureImageBuilder(ctx, cfg)
	if err != nil {
		return err
	}
	filesDir := filepath.Join(ibDir, "logos-files")
	if err := StageFiles(filesDir, cfg); err != nil {
		return err
	}
	if err := runMake(ctx, cfg, ibDir, filesDir); err != nil {
		return err
	}
	return collectImages(cfg, ibDir)
}

// ensureImageBuilder returns the unpacked Image Builder directory,
// downloading the tarball on first use.
func ensureImageBuilder(ctx context.Context, cfg *Config) (string, error) {
	url := TarballURL(cfg.DownloadBase, cfg.Release, cfg.Target)
	dirName := strings.TrimSuffix(strings.TrimSuffix(filepath.Base(url), ".tar.zst"), ".tar.xz")
	ibDir := filepath.Join(cfg.WorkDir, dirName)
	if _, err := os.Stat(filepath.Join(ibDir, "Makefile")); err == nil {
		fmt.Fprintf(cfg.out(), "using cached Image Builder at %s\n", ibDir)
		return ibDir, nil
	}

	if err := os.MkdirAll(cfg.WorkDir, 0o755); err != nil {
		return "", err
	}
	tarPath := filepath.Join(cfg.WorkDir, filepath.Base(url))
	fmt.Fprintf(cfg.out(), "downloading %s\n", url)
	if err := download(ctx, url, tarPath); err != nil {
		return "", err
	}
	fmt.Fprintf(cfg.out(), "unpacking %s\n", filepath.Base(tarPath))
	// GNU tar picks the right decompressor from the extension.
	if out, err := exec.CommandContext(ctx, "tar", "-xaf", tarPath, "-C", cfg.WorkDir).CombinedOutput(); err != nil {
		return "", fmt.Errorf("unpack image builder: %v: %s", err, out)
	}
	os.Remove(tarPath)
	if _, err := os.Stat(filepath.Join(ibDir, "Makefile")); err != nil {
		return "", fmt.Errorf("unexpected Image Builder layout: no Makefile in %s", ibDir)
	}
	return ibDir, nil
}

func download(ctx context.Context, url, dst string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: %s", url, resp.Status)
	}
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		os.Remove(dst)
		return err
	}
	return f.Close()
}

// StageFiles lays out the FILES overlay: the agent binary, its enabled init
// script, and (optionally) the enrollment preseed.
func StageFiles(filesDir string, cfg *Config) error {
	if err := os.RemoveAll(filesDir); err != nil {
		return err
	}
	agentBin, err := os.ReadFile(cfg.AgentBinary)
	if err != nil {
		return fmt.Errorf("agent binary: %w", err)
	}
	initData := []byte(initscript.Script)
	if cfg.InitScript != "" {
		if initData, err = os.ReadFile(cfg.InitScript); err != nil {
			return fmt.Errorf("init script: %w", err)
		}
	}

	writes := []struct {
		path string
		data []byte
		mode os.FileMode
	}{
		{"usr/bin/logos-agent", agentBin, 0o755},
		{"etc/init.d/logos-agent", initData, 0o755},
	}
	if cfg.Server != "" {
		preseed, err := json.Marshal(map[string]string{"server": cfg.Server, "code": cfg.Code})
		if err != nil {
			return err
		}
		// Single-use: the agent deletes it after a successful enrollment.
		writes = append(writes, struct {
			path string
			data []byte
			mode os.FileMode
		}{"etc/logos/preseed.json", preseed, 0o600})
	}
	for _, w := range writes {
		dst := filepath.Join(filesDir, w.path)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(dst, w.data, w.mode); err != nil {
			return err
		}
	}
	// FILES overlays bypass the package system, so enable the service the
	// way opkg would: an rc.d start symlink (START=95 in the init script).
	rcd := filepath.Join(filesDir, "etc/rc.d")
	if err := os.MkdirAll(rcd, 0o755); err != nil {
		return err
	}
	return os.Symlink("../init.d/logos-agent", filepath.Join(rcd, "S95logos-agent"))
}

func runMake(ctx context.Context, cfg *Config, ibDir, filesDir string) error {
	makeBin := cfg.Make
	if makeBin == "" {
		makeBin = "make"
	}
	args := []string{
		"image",
		"PROFILE=" + cfg.Profile,
		"FILES=" + filesDir,
	}
	if len(cfg.Packages) > 0 {
		args = append(args, "PACKAGES="+strings.Join(cfg.Packages, " "))
	}
	fmt.Fprintf(cfg.out(), "building image: %s %s\n", makeBin, strings.Join(args, " "))
	cmd := exec.CommandContext(ctx, makeBin, args...)
	cmd.Dir = ibDir
	cmd.Stdout = cfg.out()
	cmd.Stderr = cfg.out()
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("image build failed: %w", err)
	}
	return nil
}

// collectImages copies *-sysupgrade.bin (and .img.gz for x86-style targets)
// from the Image Builder output tree into cfg.OutputDir.
func collectImages(cfg *Config, ibDir string) error {
	root := filepath.Join(ibDir, "bin", "targets")
	var found []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.Type().IsRegular() &&
			(strings.Contains(name, "sysupgrade") || strings.HasSuffix(name, ".img.gz")) {
			found = append(found, path)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("scan build output: %w", err)
	}
	if len(found) == 0 {
		return fmt.Errorf("the build produced no sysupgrade image under %s", root)
	}
	if err := os.MkdirAll(cfg.OutputDir, 0o755); err != nil {
		return err
	}
	for _, src := range found {
		dst := filepath.Join(cfg.OutputDir, filepath.Base(src))
		data, err := os.ReadFile(src)
		if err != nil {
			return err
		}
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			return err
		}
		fmt.Fprintf(cfg.out(), "image: %s\n", dst)
	}
	return nil
}
