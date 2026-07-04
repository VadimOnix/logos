# Logos MVP Roadmap (Tier 1)

> **Status:** Living document · **Date:** 2026-07-03 · Companion to [PRD.md](./PRD.md) §5.1
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

### M1 — A real OpenWrt node (in progress)

- ✅ RPC over the management channel: correlated request/response, bounded concurrency on the agent (foundation for F4/F5/F10).
- ✅ F5: package management per node — opkg/apk autodetect, list/install/remove/update via RPC + REST + panel.
- ✅ F6 (partial): interface traffic counters (/proc/net/dev) and DHCP client list (dnsmasq leases) in the heartbeat. `ubus` wireless associations still open.
- ✅ F4 step 1: read-only `uci export` snapshot via RPC + REST. Write path (set/commit, versioned server-side, rollback with auto-revert watchdog) still open.
- ✅ F1 (packaging skeleton): OpenWrt feed Makefile + procd init script. Size budget ≤ 1 MB is currently exceeded by Go binaries (~4–5 MB stripped) — mitigation tracked in agent/openwrt/README.md.
- ✅ F4 write path: `uci set/delete/commit` through the channel — every push is a versioned `config_changes` row with pre-change snapshots; the agent arms an **auto-revert watchdog** (crash/reboot-safe via a persisted pending file) and the server confirms only over a live channel, so a change that breaks connectivity reverts itself; rollback endpoint restores stored snapshots through the same machinery.
- ⬜ mTLS for the agent channel (per-node client certs issued at enrollment; token auth remains the bootstrap).
- ⬜ F6: wireless associations via `ubus`.
- ⬜ CI job building the agent for OpenWrt targets + size report.

### M2 — Adoption & offboarding done right

- F12: adoption tool (one-line script / CLI): local SSH/ubus install, **pre-adoption snapshot** (package list + `uci export` + checksums), enroll; credentials never leave the operator's machine.
- F13 (full): disconnect **+ optional cleanup** — remove platform-installed packages, revert to snapshot, conflict confirm/skip.
- F2 (full): first-run local setup page with captive redirect for pre-flashed devices.
- F14: `logos-imagebuilder` wrapper (bake agent + headend + enrollment key into a sysupgrade image).

### M3 — Operate a small fleet

- F6/F11: metric history (short retention), node-offline alerts (email/webhook).
- F10: remote terminal via the management channel (audited).
- F7: overlay networks v1 — WireGuard full mesh, control plane as coordinator (keys, IPAM, routes), subnet-router mode, relay fallback.

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
