// logos-imagebuilder wraps the official OpenWrt Image Builder (PRD F14):
// it produces a sysupgrade image with logos-agent baked in — optionally
// pre-seeded with a control plane URL and claim code, so the router enrolls
// itself on first boot. Runs on the operator's Linux machine (the Image
// Builder itself requires Linux x86_64).
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/VadimOnix/logos/agent/internal/imagebuilder"
)

const usage = `logos-imagebuilder — bake logos-agent into an OpenWrt image

Usage:
  logos-imagebuilder --release 24.10.1 --target ath79/generic \
      --profile tplink_archer-c7-v5 --agent-binary ./logos-agent-linux-mips \
      [--server https://logos.example.com --code LG-XXXXX-XXXXX] \
      [--packages "wireguard-tools kmod-wireguard"] [--output ./images]

With --server/--code the flashed router enrolls itself on first boot
(the claim code is single-use — mint one per image). Without them it
boots into the local setup portal (http://<router>:8484).
`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	fs := flag.NewFlagSet("logos-imagebuilder", flag.ExitOnError)
	fs.Usage = func() { fmt.Fprint(os.Stderr, usage); fs.PrintDefaults() }
	cfg := &imagebuilder.Config{}
	fs.StringVar(&cfg.Release, "release", "", "OpenWrt release, e.g. 24.10.1")
	fs.StringVar(&cfg.Target, "target", "", "target/subtarget, e.g. ath79/generic")
	fs.StringVar(&cfg.Profile, "profile", "", "device profile, e.g. tplink_archer-c7-v5")
	fs.StringVar(&cfg.AgentBinary, "agent-binary", "", "logos-agent built for the device architecture")
	fs.StringVar(&cfg.InitScript, "init-script", "", "procd init script (default: embedded copy)")
	fs.StringVar(&cfg.Server, "server", "", "control plane URL to pre-seed for first-boot auto-enrollment")
	fs.StringVar(&cfg.Code, "code", "", "claim code to pre-seed (single-use; mint one per image)")
	packages := fs.String("packages", "", "extra packages, space-separated (e.g. \"wireguard-tools\")")
	fs.StringVar(&cfg.OutputDir, "output", ".", "where to put the built images")
	fs.StringVar(&cfg.WorkDir, "workdir", "", "Image Builder cache directory (default: $TMPDIR/logos-imagebuilder)")
	fs.StringVar(&cfg.DownloadBase, "download-base", "", "mirror base URL (default: https://downloads.openwrt.org)")
	fs.Parse(os.Args[1:])
	if fields := strings.Fields(*packages); len(fields) > 0 {
		cfg.Packages = fields
	}

	if err := imagebuilder.Build(ctx, cfg); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
