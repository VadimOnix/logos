# logos-agent OpenWrt package

Package feed skeleton for building `logos-agent` into an ipk/apk with the
OpenWrt buildroot or SDK.

## Build

```sh
# inside an OpenWrt buildroot/SDK checkout (23.05 or 24.10):
mkdir -p package/logos-agent
cp -r /path/to/logos/agent/openwrt/* package/logos-agent/
make defconfig
make package/logos-agent/compile V=s
```

The Go toolchain comes from the `packages` feed (`lang/golang`); the package
uses the standard `golang-package.mk` helpers, so all OpenWrt Go targets are
supported (mips is built with softfloat automatically).

## What gets installed

- `/usr/bin/logos-agent` — static binary
- `/etc/init.d/logos-agent` — procd service (respawn on crash, starts only
  when the node is enrolled)
- `/etc/logos/agent.json` — node identity, marked as a conffile (survives
  sysupgrade)

After installing:

```sh
logos-agent enroll --server https://logos.example.com --code LG-XXXXX-XXXXX
/etc/init.d/logos-agent enable && /etc/init.d/logos-agent start
```

## Size budget status (PRD §6: ≤ 1 MB flash)

As the agent grew to the full MVP feature set (enrollment, mTLS, WSS channel,
package/UCI management, WireGuard overlays, remote terminal, setup portal),
its footprint grew with it. A stripped Go binary for `mips_24kc` now measures:

| representation | size | notes |
|---|---|---|
| raw stripped (`-s -w -trimpath`) | ~9.8 MB | what CGO_ENABLED=0 emits |
| gzip | ~3.4 MB | roughly what an ipk/apk stores |
| xz / upx `--lzma` | ~2.3 MB | on-flash if self-extracting |

All three are **over the 1 MB PRD budget**, and the gap is structural, not
incidental: the binary is dominated by the Go runtime (~1.7 MB) and the
mandatory stdlib crypto (TLS + the Go 1.24+ FIPS-140 module, ~1 MB) and
`net/http` (~0.6 MB) — none of which are optional for an mTLS + WSS agent.
Only ~0.1 MB is the agent's own code, so dependency trimming cannot close
the gap.

The `agent-openwrt` CI job reports raw / gzip / `upx --lzma` sizes for every
target on each PR, so the real on-flash number is always visible. TinyGo — the one toolchain that could
approach a sub-2 MB target — is not viable: the agent relies on `net/http`,
`crypto/tls`, and `html/template`, which TinyGo does not fully support.

**Resolution.** The ≤1 MB target was retired as the wrong constraint (the
agent runs comfortably on the 16 MB-flash devices the MVP targets), and the
PRD §6 Footprint budget was revised to the measured ~2.5 MB compressed
reality. For flash-constrained builds, `logos-imagebuilder --compress` packs
the agent with `upx --lzma` into a self-extracting ~2.3 MB binary (one-time
RAM decompress at start); it is **off by default**, so the shipped binary is
unchanged unless you ask for it.
