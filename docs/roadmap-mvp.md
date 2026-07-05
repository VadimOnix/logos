# Logos MVP Roadmap (Tier 1)

> **Status: Tier 1 MVP complete** (all milestones ✅) · **Date:** 2026-07-05 · Companion to [PRD.md](./PRD.md) §5.1
> Tier 1 = the MVP (v0.x) functional requirements F1–F14: "manage and mesh 10 routers".
> Current work: early v1.0 slices (PRD §5.2) — see "Post-MVP progress" at the end.

This document breaks the MVP requirement table into buildable milestones and tracks
what exists in the repository. Priorities and requirement IDs (F1–F14) come from
PRD §5.1 and are not redefined here.

## Milestones

### M0 — Skeleton that speaks (✅ shipped, PR #2)

The thinnest end-to-end slice: a self-hostable control plane a router agent can
enroll into and stay connected to. Everything later builds on this channel.

| PRD req | Slice delivered in M0 | Status |
|---|---|---|
| F3 Control plane: device registry, node detail, online/offline | Registry in Postgres, nodes REST API, online/offline via channel liveness, minimal built-in panel | ✅ started |
| F2 Enrollment (claim-code flow) | Single-use expiring claim codes; `POST /api/v1/enroll` exchanges code + agent keypair for node identity + token. First-run captive portal page is **M2** | ✅ started |
| F8 Single-org auth | Bootstrap admin (email+password), session cookies, API tokens for automation | ✅ started |
| F1 Agent: outbound persistent channel | Go agent: `enroll` / `run` / `leave`; outbound WSS with backoff+jitter, heartbeat, reconnect. OpenWrt packaging (<1 MB, procd init) is **M1** | ✅ started |
| F6 Basic monitoring | Heartbeat carries system metrics (uptime, load, mem, fs); stored as latest-state. Interface traffic + client list are **M1**, history is **M3** | ✅ started |
| F13 Clean offboarding | `logos-agent leave` wipes local identity without needing the headend; panel-side remove marks node `left`. Snapshot-based full cleanup is **M2** (with F12 adoption tool) | ✅ started |
| F9 Self-hosted deployment | `docker compose up` = server + Postgres; single static server binary | ✅ started |

### M1 — A real OpenWrt node (✅ complete; size budget resolved)

- ✅ RPC over the management channel: correlated request/response, bounded concurrency on the agent (foundation for F4/F5/F10).
- ✅ F5: package management per node — opkg/apk autodetect, list/install/remove/update via RPC + REST + panel.
- ✅ F6 (partial): interface traffic counters (/proc/net/dev) and DHCP client list (dnsmasq leases) in the heartbeat. `ubus` wireless associations still open.
- ✅ F4 step 1: read-only `uci export` snapshot via RPC + REST. Write path (set/commit, versioned server-side, rollback with auto-revert watchdog) still open.
- ✅ F1 (packaging skeleton): OpenWrt feed Makefile + procd init script. The original ≤ 1 MB size budget was later measured, found structurally unreachable, and formally revised — see the M1 exit note below.
- ✅ F4 write path: `uci set/delete/commit` through the channel — every push is a versioned `config_changes` row with pre-change snapshots; the agent arms an **auto-revert watchdog** (crash/reboot-safe via a persisted pending file) and the server confirms only over a live channel, so a change that breaks connectivity reverts itself; rollback endpoint restores stored snapshots through the same machinery.
- ✅ mTLS for the agent channel: internal CA on the control plane, per-node client certs issued from a CSR at enrollment (key never leaves the device), dedicated TLS listener with `RequireAndVerifyClientCert`, identity = cert CN (node UUID) re-checked against node status per request (left nodes rejected without a CRL), rotation via `/agent/renew` inside a 30-day window. Token channel remains for pre-cert nodes.
- ✅ F6: wireless associations via `ubus call iwinfo` in the heartbeat.
- ✅ CI job cross-building the agent for common OpenWrt targets (mips/mipsle/arm/arm64/x86_64) with a size report vs the ≤1 MB budget.

**M1 exit note (resolved):** the agent binary size budget is **closed**. The
≤1 MB flash target proved structurally unreachable: measured for `mips_24kc`
at the full MVP feature set, ~9.8 MB raw stripped, ~3.4 MB gzip, ~2.3 MB
xz/`upx --lzma`. The binary is dominated by the Go runtime (~1.7 MB) and
mandatory stdlib crypto (TLS + FIPS-140, ~1 MB) + `net/http` (~0.6 MB); only
~0.1 MB is the agent's own code, so trimming cannot close the gap (TinyGo is
not viable — `net/http` + `crypto/tls` + `html/template` unsupported). The
`agent-openwrt` CI job reports raw / gzip / `upx --lzma` sizes on every PR.

Resolution (both shipped):
- **PRD budget revised** (§6 Footprint) to the measured ~2.5 MB compressed
  reality; the agent runs comfortably on the 16 MB-flash devices the MVP
  targets, so the original 1 MB figure was the wrong constraint, not a bug.
- **Opt-in `upx --lzma` packing** for flash-constrained operators:
  `logos-imagebuilder --compress` bakes a self-extracting ~2.3 MB binary
  (one-time RAM decompress at start). Off by default — no runtime change.

### M2 — Adoption & offboarding done right (in progress)

- ✅ F12: `logos-adopt` CLI — drives the router locally over SSH (credentials never leave the operator's machine): compatibility check (OpenWrt, arch incl. MIPS endianness, RAM/flash), **pre-adoption snapshot** (package list + `uci export`) stored on the device, agent binary fetched from the control plane (`/api/v1/agent-binary/{arch}`, `LOGOS_AGENT_BINARIES_DIR`) or a local file, procd service install, enroll, fail-safe rollback of everything uploaded on mid-install failure.
- ✅ F13 (full): `logos-agent leave --cleanup` / `logos-adopt remove --cleanup` — removes packages added since adoption (diff vs snapshot, per-item confirm/skip semantics via plan + `--yes`), reverts UCI to the snapshot, wipes identity; works without headend connectivity.
- ✅ F12: fleet adoption — `logos-adopt fleet` adopts many routers at once
  from a CSV inventory (`router,user,password,key`; blanks inherit the flag
  defaults) or an IPv4 `--range`, with bounded `--concurrency`. Each router
  gets its own fresh single-use claim code, minted lazily via an API token
  **only after it passes its checks**, so a scan never burns codes on
  unreachable hosts. One failure never blocks the rest; the process exits
  non-zero if any router failed.
- ✅ File checksums in the snapshot — the pre-adoption snapshot records the
  sha256 of every `/etc/config` file, so `--cleanup` detects which config
  files diverged since adoption and warns that the revert will overwrite
  them (config-file-level conflict detection).
- ✅ F2 (full): first-run local setup page — an unenrolled agent serves
  `http://<router>:8484` (any other path redirects there, so captive-portal
  probes land on it) with a claim-code form; enrollment closes the portal and
  opens the management channel, and a node that later *leaves* falls back to
  the portal. The procd service now starts in both states. DNS-hijack-based
  captive redirect is left to the image builder (F14).
- ✅ F14: `logos-imagebuilder` — wraps the official OpenWrt Image Builder:
  downloads/caches the per-target tarball, stages a FILES overlay (agent
  binary, enabled procd service, optional `preseed.json` with control-plane
  URL + claim code), runs `make image`, and collects the sysupgrade images.
  A flashed router **auto-enrolls on first boot** from the preseed (retrying
  until WAN is up, file removed after success) with the F2 portal running in
  parallel as fallback; without a preseed it boots straight into the portal.
  `--compress` optionally `upx --lzma`-packs the agent binary into the image
  (~2.3 MB self-extracting vs ~9.8 MB) for flash-constrained devices.

### M3 — Operate a small fleet (✅ feature-complete)

- ✅ F7: overlay networks v1 — WireGuard full mesh with the control plane as
  coordinator: overlays with a CIDR each, IPAM (lowest free host address),
  per-node keys **generated on the device** (only the public key is reported),
  peer/endpoint distribution over the management channel, uci/netifd interface
  plus a self-contained `logos` firewall zone (two-way forwarding with lan,
  listen port opened on wan), subnet-router mode via advertised LAN subnets
  (`allowed_ips` + `route_allowed_ips`), and offline convergence — agents
  reconcile their full overlay set (including pruning) on every reconnect.
  Endpoints come from the channel's source address with persistent keepalive
  for NAT. **Open (next slice):** relay fallback for peers that cannot reach
  each other directly, richer endpoint discovery (STUN/ICE-style).
- ✅ F11: node-offline **and low-flash** alerts — a watcher compares node
  liveness (live channel wins over a stale heartbeat) against a threshold
  (`LOGOS_ALERT_OFFLINE_AFTER`, default 3m) and each online node's
  root-filesystem usage against `LOGOS_ALERT_DISK_PCT` (default 90%, PRD
  §4.3 "flash > 90%"; hysteresis on the clear side avoids flapping), then
  notifies a JSON webhook, a Telegram chat (`LOGOS_ALERT_TELEGRAM_TOKEN` +
  `LOGOS_ALERT_TELEGRAM_CHAT`, an early slice of v1.0 "Telegram delivery"),
  and/or SMTP recipients on the problem **and its recovery**. Both alert
  states persist per-node in the registry, so restarts neither repeat nor
  lose alerts.
- ✅ F6 (history): the server derives a compact sample (load, memory %,
  rootfs %, aggregate rx/tx, client count) from each heartbeat into
  `node_metrics_history`, kept for 24h by the janitor and served at
  `/nodes/{id}/metrics/history?since=…`. The panel renders dependency-free
  inline-SVG sparklines (traffic shown as per-interval rates).
- ✅ F10: remote terminal — an interactive shell multiplexed over the
  management channel (`term_open`/`term_data`/`term_close`). The agent runs
  the system shell in a pty (bounded to 2 concurrent sessions, 30-min idle
  reaper, all sessions die with the channel); the control plane bridges a
  browser WebSocket to it and records an audit row (who/when/why-ended,
  content not stored). Panel has a built-in terminal (no external JS).

## Cross-cutting rules (from PRD §6–7, enforced from M0)

- Agent initiates everything; only outbound 443. No inbound ports on the node.
- Control plane is never in the data path; its outage degrades management, never connectivity.
- One server binary (Go) + Postgres. `docker compose up` < 5 minutes is a hard requirement.
- Agent: Go, single static binary; ~3.4 MB gzip / ~2.3 MB with opt-in upx packing (budget revised 2026-07, PRD §6), ≤ 10 MB RSS — sizes reported in CI from M1.
- Security hygiene is never optional: enrollment codes single-use + expiring, secrets hashed/encrypted at rest, rate-limited enrollment endpoint.

## Post-MVP progress (early v1.0 slices, PRD §5.2)

Shipped after MVP completion, in small increments:

- **Fleet summary** — `GET /api/v1/stats` (node/overlay/alert counters) and a
  panel summary strip with alert badges.
- **Telegram alert delivery** — Bot API sink next to webhook/SMTP
  (`LOGOS_ALERT_TELEGRAM_TOKEN` + `LOGOS_ALERT_TELEGRAM_CHAT`).
- **Overlay overlap detection** — creating an overlay whose CIDR overlaps an
  existing one is refused with 409 (ambiguous routes otherwise).
- **Audit log (CE-basic)** — who/what/when for admin actions (login, tokens,
  claim codes, nodes, packages, config, overlays, terminal, 2FA), served at
  `GET /api/v1/audit`, 90-day retention, collapsible panel viewer.
- **TOTP 2FA** — RFC 6238 second factor on stdlib only: possession-proof
  enrollment, code-gated disable, progressive login field in the panel.
- **Config drift detection** — the agent fingerprints `uci export` in every
  heartbeat; the server compares it against a per-node accepted baseline
  (first contact / confirmed Logos change / explicit accept) and the panel
  flags "⚠ drift" with an accept action.
- **Bulk package operations** — `POST /api/v1/nodes/packages/bulk` fans
  install/remove/update out to many nodes (bounded concurrency, per-node
  verdicts); panel buttons apply to all online nodes.
- **Ops hardening** — `/readyz` readiness probe (DB ping) wired into the
  compose healthcheck, and a production Caddy overlay with automatic HTTPS.

## Explicitly deferred (not MVP)

Multi-tenancy/RBAC, templates & drift, firmware orchestration, overlay ACLs/DNS,
OIDC/SSO, hosted image builder, GUI adoption app, cloud billing — all v1.0+ (PRD §5.2).

---

*Related: [PRD.md](./PRD.md) · [market-analysis.md](./market-analysis.md) · [pricing.md](./pricing.md)*
