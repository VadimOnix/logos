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

A stripped Go binary for `mips_24kc` currently lands at ~4–5 MB (~1.5 MB in
the compressed ipk) — **over the PRD budget**. This is tracked as an M1 task:
options are `upx --lzma` (≈1.3 MB), building with TinyGo, or trimming
dependencies further (the agent already uses only stdlib + one WS library).
The budget check will be wired into CI once the OpenWrt CI job exists;
until then treat the 1 MB figure as a target, not a guarantee.
