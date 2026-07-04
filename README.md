# logos

> everywhere anywhere access

**Logos** is an open-source SaaS for administering fleets of OpenWrt routers (and, eventually, any Linux node that manages network traffic). A lightweight agent on the router dials out to a control plane — self-hosted or managed cloud — giving you a single admin panel for enrollment, configuration, package management, monitoring, and WireGuard-based virtual subnets that join routers (and the LANs behind them) into one network.

**Logos** — open-source платформа для централизованного администрирования роутеров на OpenWrt (а в перспективе — любых Linux-нод, управляющих сетевым трафиком). Лёгкий агент на роутере устанавливает исходящее соединение с головным сервером (self-hosted или облачным), давая единую панель для подключения устройств, управления конфигурацией и пакетами, мониторинга и объединения роутеров в виртуальные подсети на WireGuard.

## Quick start (self-hosted control plane)

```sh
cd deploy
cp .env.example .env   # set POSTGRES_PASSWORD and the admin credentials
docker compose up -d
```

Open http://localhost:8080, log in with the admin credentials from `.env`,
click **New claim code**, then adopt an existing OpenWrt router in one
command from your machine (admin credentials are used only for the local
SSH session and never sent to the server):

```sh
logos-adopt run --router 192.168.1.1 --server http://<control-plane-host>:8080 --code LG-XXXXX-XXXXX
```

The tool checks compatibility, takes a **pre-adoption snapshot**, installs
the agent, and enrolls the node — it appears in the panel as *online* with
live metrics. Manual enrollment also works (`logos-agent enroll … && logos-agent run`).

Leaving is just as easy and never requires the control plane to be
reachable:

```sh
logos-adopt remove --router 192.168.1.1 --cleanup   # revert to the pre-adoption snapshot
# or on the device itself: logos-agent leave [--cleanup]
```

### Building from source

```sh
make build       # bin/logos-server, bin/logos-agent (Go 1.25+)
make test
```

## Repository layout

| Path | Contents |
|---|---|
| `server/` | Control plane: single Go binary + Postgres — device registry, claim-code enrollment, agent WebSocket channel, auth (sessions + API tokens), built-in admin panel |
| `agent/` | `logos-agent` for OpenWrt/Linux nodes (enroll, persistent outbound management channel, clean leave) and `logos-adopt` (SSH adoption tool with pre-adoption snapshot and full-cleanup offboarding) |
| `deploy/` | Dockerfile + docker-compose for self-hosting |
| `docs/` | Product docs (see below) |

## Documentation / Документация

| Document | Contents |
|---|---|
| [docs/PRD.md](docs/PRD.md) | Product Requirements Document: vision, personas, user flows (zero-touch enrollment, fleet admin, overlay networks), requirements by release, architecture, risks. *Включает резюме на русском.* |
| [docs/roadmap-mvp.md](docs/roadmap-mvp.md) | MVP (Tier 1) roadmap: PRD §5.1 requirements broken into milestones M0–M3, with current status. |
| [docs/market-analysis.md](docs/market-analysis.md) | Competitive landscape: OpenWISP, GenieACS, UniFi/Omada/Meraki, GL.iNet GoodCloud, Teltonika RMS, Tailscale/ZeroTier/NetBird/Netmaker — strengths, weaknesses, positioning. *Включает резюме на русском.* |
| [docs/pricing.md](docs/pricing.md) | Open-core monetization model: free Community Edition + paid on-prem Enterprise Edition + managed cloud (incl. a dedicated, isolated RU-region cloud), tier design, competitor pricing references, unit economics. *Включает резюме на русском.* |

## Status

Working through the [MVP roadmap](docs/roadmap-mvp.md). End-to-end today:
enrollment (claim codes + per-node mTLS), node registry and live agent
channel, package management, UCI config push with auto-revert on lost
connectivity, monitoring in the heartbeat, SSH adoption with pre-adoption
snapshot, full-cleanup offboarding, and **WireGuard overlay networks v1**
(server-coordinated full mesh with on-device keys and subnet-router mode).
Remaining for the MVP: first-run captive portal, image builder wrapper,
alerts, remote terminal — see the roadmap.
