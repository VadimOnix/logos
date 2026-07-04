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
target on each PR, so the real on-flash number is always visible. The two
paths that can actually meet a sub-2 MB target are:

- **`upx --lzma`** on the shipped binary (self-extracting, ~1.3–2 MB on
  flash) — costs a one-time decompress into RAM at start, which matters on
  4/32 MB devices but not on the 8/64 MB class this MVP targets.
- **TinyGo** — not currently viable: the agent relies on `net/http`,
  `crypto/tls`, and `html/template`, which TinyGo does not fully support.

Whether to pack the shipped image with UPX, or to revise the PRD budget to
the measured ~1.5–2 MB compressed reality, is a product decision tracked at
the end of the MVP roadmap; treat the 1 MB figure as an aspiration, not a
guarantee.
