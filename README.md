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

Adopt a whole fleet at once from a CSV inventory or an IP range (a fresh
single-use claim code is minted per router via an API token, only for hosts
that pass their checks):

```sh
logos-adopt fleet --server http://<control-plane-host>:8080 \
  --api-token <token> --csv routers.csv --concurrency 8
```

Leaving is just as easy and never requires the control plane to be
reachable:

```sh
logos-adopt remove --router 192.168.1.1 --cleanup   # revert to the pre-adoption snapshot
# or on the device itself: logos-agent leave [--cleanup]
```

### Production deployment (public domain + HTTPS)

The quick start above binds the panel to loopback and serves it over plain
HTTP — fine for evaluating on one machine. For routers that dial in from
other locations you need a **public address for the control plane** and TLS
on the panel. There are two listeners, handled differently:

| Port | Purpose | TLS |
|---|---|---|
| `8080` | panel / REST API | terminated by a reverse proxy in front |
| `8443` | agent mTLS channel | **terminated by the server itself** — never proxy-terminate it; expose it directly |

**You don't need a *dedicated* domain — a subdomain is enough** (e.g.
`logos.example.com`). A public static IP works too, but a name lets the
server move and lets Caddy fetch a certificate. Routers never need any
inbound port or public IP — the agent only dials **out**.

1. Point a DNS `A`/`AAAA` record at the host: `logos.example.com → <server IP>`.
2. In `deploy/.env` set both names to that domain:
   ```sh
   LOGOS_AGENT_HOST=logos.example.com     # baked into the agent channel cert
   LOGOS_PANEL_DOMAIN=logos.example.com   # Caddy gets a Let's Encrypt cert for it
   ```
3. Bring it up with the Caddy overlay (automatic HTTPS, no manual certs):
   ```sh
   cd deploy
   docker compose -f docker-compose.yml -f docker-compose.prod.yml up -d
   ```
4. Open firewall ports **80** and **443** (Caddy) and **8443** (agent
   channel). Port 8080 stays on loopback and never needs to be public.

The server exposes two probes for load balancers and orchestrators:
`GET /healthz` (liveness — the process is up, never touches the database)
and `GET /readyz` (readiness — returns `503` while Postgres is unreachable,
so traffic is only routed once the instance can actually serve). The compose
file health-checks the server on `/readyz`.

The panel is now at `https://logos.example.com`, and agents dial
`wss://logos.example.com:8443` automatically. Adopt routers against the
HTTPS URL:

```sh
logos-adopt run --router 192.168.1.1 --server https://logos.example.com --code LG-XXXXX-XXXXX
```

Prefer nginx? Terminate TLS for the panel only and leave 8443 alone:

```nginx
server {
    listen 443 ssl;
    server_name logos.example.com;
    # ssl_certificate / ssl_certificate_key from certbot, etc.
    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;   # panel WebSockets (terminal)
        proxy_set_header Connection "upgrade";
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    }
}
# Do NOT add a server block for 8443 — the agent channel terminates its own TLS.
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

**The Tier 1 MVP (PRD §5.1, F1–F14) is complete** — see the
[roadmap](docs/roadmap-mvp.md) for the milestone-by-milestone record.
End-to-end today: enrollment (claim codes + per-node mTLS, first-run
setup portal, preseeded images via `logos-imagebuilder`), node registry
and live agent channel, package management, UCI config push with
auto-revert on lost connectivity, monitoring with 24h metric history and
sparklines, offline/low-flash alerts (webhook / Telegram / SMTP),
audited remote terminal, SSH adoption (single router or fleet CSV/IP
range) with pre-adoption snapshot, full-cleanup offboarding, and
**WireGuard overlay networks v1** (server-coordinated full mesh with
on-device keys, subnet-router mode, and CIDR-overlap protection).

Work has moved on to early v1.0 items (PRD §5.2): fleet stats API with
panel summary strip and alert badges, a basic audit log with panel
viewer, and TOTP two-factor login are already in. Production deployment
ships with a Caddy auto-HTTPS overlay and liveness/readiness probes.
