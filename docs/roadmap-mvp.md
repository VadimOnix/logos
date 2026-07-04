# Logos MVP Roadmap (Tier 1)

> **Status:** Living document · **Date:** 2026-07-04 · Companion to [PRD.md](./PRD.md) §5.1
> Tier 1 = the MVP (v0.x) functional requirements F1–F14: "manage and mesh 10 routers".

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

### M1 — A real OpenWrt node (✅ code-complete; size budget open)

- ✅ RPC over the management channel: correlated request/response, bounded concurrency on the agent (foundation for F4/F5/F10).
- ✅ F5: package management per node — opkg/apk autodetect, list/install/remove/update via RPC + REST + panel.
- ✅ F6 (partial): interface traffic counters (/proc/net/dev) and DHCP client list (dnsmasq leases) in the heartbeat. `ubus` wireless associations still open.
- ✅ F4 step 1: read-only `uci export` snapshot via RPC + REST. Write path (set/commit, versioned server-side, rollback with auto-revert watchdog) still open.
- ✅ F1 (packaging skeleton): OpenWrt feed Makefile + procd init script. Size budget ≤ 1 MB is currently exceeded by Go binaries (~4–5 MB stripped) — mitigation tracked in agent/openwrt/README.md.
- ✅ F4 write path: `uci set/delete/commit` through the channel — every push is a versioned `config_changes` row with pre-change snapshots; the agent arms an **auto-revert watchdog** (crash/reboot-safe via a persisted pending file) and the server confirms only over a live channel, so a change that breaks connectivity reverts itself; rollback endpoint restores stored snapshots through the same machinery.
- ✅ mTLS for the agent channel: internal CA on the control plane, per-node client certs issued from a CSR at enrollment (key never leaves the device), dedicated TLS listener with `RequireAndVerifyClientCert`, identity = cert CN (node UUID) re-checked against node status per request (left nodes rejected without a CRL), rotation via `/agent/renew` inside a 30-day window. Token channel remains for pre-cert nodes.
- ✅ F6: wireless associations via `ubus call iwinfo` in the heartbeat.
- ✅ CI job cross-building the agent for common OpenWrt targets (mips/mipsle/arm/arm64/x86_64) with a size report vs the ≤1 MB budget.

**M1 exit note:** the agent binary size budget (≤1 MB) is still exceeded (~4–5 MB stripped Go); tracked in agent/openwrt/README.md — candidates: upx, TinyGo, or revising the PRD budget to ~2 MB compressed.

### M2 — Adoption & offboarding done right (in progress)

- ✅ F12: `logos-adopt` CLI — drives the router locally over SSH (credentials never leave the operator's machine): compatibility check (OpenWrt, arch incl. MIPS endianness, RAM/flash), **pre-adoption snapshot** (package list + `uci export`) stored on the device, agent binary fetched from the control plane (`/api/v1/agent-binary/{arch}`, `LOGOS_AGENT_BINARIES_DIR`) or a local file, procd service install, enroll, fail-safe rollback of everything uploaded on mid-install failure.
- ✅ F13 (full): `logos-agent leave --cleanup` / `logos-adopt remove --cleanup` — removes packages added since adoption (diff vs snapshot, per-item confirm/skip semantics via plan + `--yes`), reverts UCI to the snapshot, wipes identity; works without headend connectivity.
- ⬜ F12: fleet adoption (CSV/IP-range) — v1 per PRD.
- ⬜ File checksums in the snapshot (config-file-level conflict detection).
- ⬜ F2 (full): first-run local setup page with captive redirect for pre-flashed devices.
- ⬜ F14: `logos-imagebuilder` wrapper (bake agent + headend + enrollment key into a sysupgrade image).

### M3 — Operate a small fleet (in progress)

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
- ✅ F11: node-offline alerts — a watcher compares node liveness (live
  channel wins over a stale heartbeat) against a threshold
  (`LOGOS_ALERT_OFFLINE_AFTER`, default 3m) and notifies a JSON webhook
  and/or SMTP recipients on offline **and recovery**; alert state persists in
  the registry, so restarts neither repeat nor lose alerts.
- ⬜ F6: metric history (short retention).
- ⬜ F10: remote terminal via the management channel (audited).

## Cross-cutting rules (from PRD §6–7, enforced from M0)

- Agent initiates everything; only outbound 443. No inbound ports on the node.
- Control plane is never in the data path; its outage degrades management, never connectivity.
- One server binary (Go) + Postgres. `docker compose up` < 5 minutes is a hard requirement.
- Agent: Go, single static binary; target ≤ 1 MB flash / ≤ 10 MB RSS (checked in CI from M1).
- Security hygiene is never optional: enrollment codes single-use + expiring, secrets hashed/encrypted at rest, rate-limited enrollment endpoint.

## Explicitly deferred (not MVP)

Multi-tenancy/RBAC, templates & drift, firmware orchestration, overlay ACLs/DNS,
OIDC/SSO, hosted image builder, GUI adoption app, cloud billing — all v1.0+ (PRD §5.2).

---

*Related: [PRD.md](./PRD.md) · [market-analysis.md](./market-analysis.md) · [pricing.md](./pricing.md)*
