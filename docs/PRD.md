# Logos — Product Requirements Document (PRD)

> **Status:** Draft v0.1 · **Date:** 2026-06-09 · **Owner:** @vadimonix
> Open-source SaaS for managing fleets of OpenWrt routers (and, later, any Linux node that routes network traffic).

---

## Краткое резюме (Russian Executive Summary)

**Logos** — open-source платформа для централизованного администрирования роутеров под управлением OpenWrt. Лёгкий агент на роутере устанавливает исходящее соединение с головным сервером (control plane), поэтому роутеры могут подключаться откуда угодно через интернет — без белых IP, проброса портов и VPN-доступа к площадке.

**Ключевые сценарии:**

1. **Zero-touch подключение.** Пользователь включает роутер с прошитым агентом, подключается к его Wi-Fi/LAN — и его перебрасывает на страницу первичной настройки (captive-portal-style onboarding). Там он авторизуется, задаёт базовые параметры домашней/офисной сети (SSID, пароль, часовой пояс) и адрес головного сервера. Роутер автоматически регистрируется на сервере и подтягивает готовую конфигурацию своего тенанта.
2. **Администрирование через панель.** Управление установленными пакетами (opkg/apk), push конфигураций (UCI), обновление прошивок, мониторинг состояния сети (uptime, трафик, клиенты, загрузка CPU/RAM/flash), алерты.
3. **Виртуальные подсети.** Роутеры одного тенанта объединяются в overlay-сеть на WireGuard: устройства за разными роутерами видят друг друга в сетевом окружении, как будто находятся в одной LAN — «Tailscale для целых сетей, а не отдельных устройств».
4. **Мульти-тенантность для MSP.** Один сервер обслуживает многих клиентов: организации, роли, шаблоны конфигураций, массовые операции.

**Модель дистрибуции:** open-core с разделением изданий (по образцу GitLab CE/EE):

- **Community Edition** — открытая self-hosted версия, бесплатно (с возможностью добровольных донатов), с намеренными ограничениями (точный набор — открытый вопрос; кандидаты: одна организация, только локальная аутентификация, single-instance, стандартный брендинг, только операционные алерты), которые снимаются апгрейдом до **Enterprise Edition** — платной on-premise лицензии с enterprise-модулями (мульти-тенантность, SSO/SAML, HA, white-label и security operations: детальное аудит-логирование с SIEM-экспортом, детект подозрительных паттернов, flow logs) и поддержкой. Принцип: CE никогда не менее защищённая — гигиена безопасности (mTLS, 2FA, подписанные обновления, патчи) не гейтится; платной становится только надстройка наблюдаемости/детекта/комплаенса.
- **Logos Cloud** — managed-облако с тарификацией за подключённую ноду.
- **Logos Cloud RU** — отдельное изолированное облако для абонентов из России: хранение и обработка данных на территории РФ в соответствии со 152-ФЗ и требованием локализации (242-ФЗ), оплата в рублях, **отдельный сайт-витрина**, на который зарубежные пользователи не попадают.

Подробности — в [pricing.md](./pricing.md), анализ конкурентов — в [market-analysis.md](./market-analysis.md).

**Целевая аудитория:** MSP и малый бизнес, обслуживающие парки роутеров у клиентов; распределённые команды, объединяющие офисы и дома сотрудников в одну виртуальную сеть. Вторичная аудитория — продвинутые домашние пользователи OpenWrt.

---

## 1. Vision & Problem Statement

### 1.1 Problem

OpenWrt is the de-facto standard open firmware for routers, but managing more than one OpenWrt device is painful:

- **No fleet management.** LuCI and SSH are single-device tools. Managing 20 routers for clients means 20 browser tabs, 20 sets of credentials, and manual, error-prone repetition.
- **Devices live behind NAT.** Client routers sit behind carrier-grade NAT or dynamic IPs. Reaching them requires fragile port-forwarding, dynamic DNS, or a hand-rolled VPN per site.
- **Existing options force a trade-off.** Vendor clouds (UniFi, Omada, Meraki) are polished but lock you into one vendor's hardware and a closed control plane. Open-source tools (OpenWISP, GenieACS) are powerful but operationally heavy, dated in UX, and offer no first-class hosted option. Mesh-VPN products (Tailscale, ZeroTier, NetBird) connect devices brilliantly but do not *manage* the router itself — no package management, no config push, no firmware updates.
- **First-time setup is expert work.** Flashing OpenWrt is only step one; a non-expert end user cannot be asked to configure Wi-Fi, timezone, and a management VPN over SSH.

### 1.2 Vision

**Logos is the open control plane for every network edge.** Plug in a Logos-enabled router anywhere in the world, walk through a one-page setup, and it appears in your admin panel — configured, monitored, updatable, and meshed with the rest of your fleet. The core is open source and self-hostable; a managed cloud makes it a one-click SaaS.

One sentence: *what Tailscale did for connecting devices, Logos does for owning and operating the routers themselves.*

### 1.3 Why now

- WireGuard is in the Linux kernel and packaged for OpenWrt — cheap, fast overlay networking on $30 hardware.
- OpenWrt 24.x+ provides modern `ubus`/`uci` APIs and the `apk` package manager transition, giving a stable programmatic surface.
- The open-core SaaS playbook (Grafana, Supabase, Tailscale/Headscale) is proven: self-hosters become the community; teams that don't want to operate infrastructure pay for cloud.
- MSPs are actively looking for non-Meraki/non-Broadcom-licensed alternatives as subscription costs climb.

## 2. Personas

| Persona | Description | Top needs |
|---|---|---|
| **Marina, MSP engineer** (primary) | Operates 50–500 routers deployed at client homes/branches. | Multi-tenant panel, config templates, bulk operations, zero-touch enrollment her clients can survive, alerting before the client calls. |
| **Ilya, IT lead of a distributed team** (primary) | 15-person company, 3 small offices + remote employees. Wants every site on one virtual network. | Easy site-to-site overlay, ACLs between subnets, visibility into who/what is on the network, no per-seat VPN pricing. |
| **Dmitry, homelab enthusiast** (secondary) | Runs OpenWrt at home and at his parents' place. Self-hosts everything. | Free self-hosted control plane, docker-compose install, no cloud dependency, good docs. He is the community and the funnel. |

## 3. Glossary

- **Node** — a managed device. v1: an OpenWrt router. Later: any Linux machine that routes traffic.
- **Agent** — the lightweight Logos daemon on the node. Maintains an outbound connection to the control plane; executes config/package/monitoring tasks via `ubus`/`uci`/`opkg`(`apk`).
- **Control plane (headend)** — the server: API, admin panel, device registry, config store, overlay-network coordinator. Self-hosted or Logos Cloud.
- **Tenant (organization)** — isolation unit: users, nodes, templates, overlay networks. An MSP has one org per client.
- **Overlay network (virtual subnet)** — WireGuard-based mesh joining selected nodes (and their LANs) of a tenant into one routable virtual network.

## 4. User Stories & Key Flows

### 4.1 Zero-touch enrollment (the signature flow)

> As an end user with zero networking knowledge, I plug in the router and get a working, centrally managed network in under 5 minutes.

1. Router boots a Logos-enabled OpenWrt image (or has the `logos-agent` package installed). Agent detects it is unprovisioned.
2. User connects to the router (default Wi-Fi/LAN). Any HTTP request is intercepted (captive-portal style) and redirected to the local **first-run setup page**.
3. Setup page asks for: control-plane host (pre-filled if the image was built with one — an MSP ships images pointing at their headend), and an **enrollment code / claim link** OR sign-in to the control plane.
4. User sets basic home-network parameters: SSID + password, admin password, timezone. Everything else comes from the tenant's template.
5. Agent performs enrollment: generates a keypair, calls the control plane's enrollment endpoint with the code, receives its node identity + mTLS client certificate, opens the persistent management channel, and pulls the tenant's ready-made configuration.
6. The node appears in the admin panel as *online*; the user's browser shows "you're done".

Acceptance criteria:
- Enrollment works behind NAT/CGNAT with only outbound 443 available.
- An unclaimed router exposes no remote management surface.
- Re-flashing/reset produces a clean re-enrollment path; the panel shows the old node as replaceable.
- An admin can also pre-register nodes by serial/key (batch import) so devices auto-claim without a code (true zero-touch for MSP shipments).

### 4.2 Fleet administration

- As an MSP engineer, I group nodes by tenant/site/tag and apply **config templates** (UCI fragments with variables); drift between template and device state is detected and reported.
- As an admin, I view and manage **installed packages** on a node or a group: list, install, remove, upgrade (`opkg`/`apk`), with rollout in batches and automatic halt on failure spikes.
- As an admin, I push **firmware upgrades** (sysupgrade with config preservation), staged: canary → fleet.
- As an admin, I open a **remote terminal / LuCI proxy** to any online node through the management channel (audited).
- Every change is **versioned**; I can diff and roll back a node's config.

### 4.3 Network observability

- As an admin, I see per-node: online/offline, uptime, WAN IP, firmware/agent version, CPU/RAM/flash, traffic per interface, connected clients (DHCP leases / wireless associations), Wi-Fi signal quality.
- I define **alerts**: node offline > N min, flash > 90%, WAN flapping, new device on LAN (optional). Delivery: email/webhook/Telegram.
- Metric history with configurable retention (short on self-hosted default; longer retention is a cloud-tier feature).

### 4.4 Virtual subnets (overlay networks)

> As an IT lead, I select three office routers and one cloud VM, click "create network", and devices behind them can reach each other by IP/name as if on one LAN.

- Overlay is WireGuard; the control plane is the coordinator (key distribution, IPAM for the overlay, route advertisement), data path is peer-to-peer where possible, with relay fallback for hard NAT.
- A node can advertise its LAN subnet into the overlay (subnet-router mode) — whole networks see each other, not just the routers.
- ACLs: which subnets/nodes may talk to which (default deny between tenants, default allow inside a network, rule editor).
- mDNS/DNS: name resolution across the overlay (`node-name.network.logos.internal`), optional mDNS reflection so devices show up in "network neighborhood".
- Conflict handling: detect overlapping LAN ranges and propose renumbering or NAT mapping.

### 4.5 Multi-tenancy & access (MSP)

- Organizations with isolated nodes, networks, templates; an MSP user can hold roles across many orgs.
- RBAC: owner / admin / operator / read-only.
- Audit log of every action (who pushed what to which node).
- White-label-able panel (logo, domain) — commercial tier.

## 5. Functional Requirements by Release

### 5.1 MVP (v0.x) — "manage and mesh 10 routers"

| # | Requirement | Priority |
|---|---|---|
| F1 | `logos-agent` for OpenWrt 23.05/24.10: outbound persistent channel (WebSocket/gRPC over TLS 443), survives reboots/reconnects, < 1 MB installed | Must |
| F2 | Enrollment: claim-code flow + first-run local setup page (captive redirect) | Must |
| F3 | Control plane: device registry, node detail page, online/offline state | Must |
| F4 | Config push: UCI get/set/commit with versioning and rollback | Must |
| F5 | Package management per node: list/install/remove/upgrade | Must |
| F6 | Basic monitoring: heartbeat, system metrics, interface traffic, client list | Must |
| F7 | Overlay networks v1: create network, attach nodes, WireGuard full mesh w/ relay fallback, subnet-router mode | Must |
| F8 | Single-org auth: email+password, sessions, API tokens | Must |
| F9 | Self-hosted deployment: single `docker compose up` (server + Postgres), docs | Must |
| F10 | Remote terminal to node via management channel | Should |
| F11 | Alerts: node offline (email/webhook) | Should |

### 5.2 v1.0 — "MSP-ready"

- Multi-tenancy (organizations), RBAC, audit log.
- Config templates with variables; drift detection; group/bulk operations with staged rollout.
- Firmware upgrade orchestration (sysupgrade), pre-registered/batch enrollment by key or serial.
- Overlay ACLs, overlay DNS, overlap detection.
- Metric history + dashboards; alert rules engine; Telegram/Slack delivery.
- OIDC/SSO login; 2FA.
- Image builder service: download a sysupgrade image pre-baked with agent + headend host + enrollment key.
- **Logos Cloud**: hosted control plane, billing per node (see pricing.md).

### 5.3 Editions & distribution (packaging requirements)

| Channel | What it is | Release |
|---|---|---|
| **Community Edition (CE)** | Open-source self-hosted control plane + agent; voluntary donations accepted (GitHub Sponsors / Open Collective), never modeled as revenue. Full core (enrollment, config, packages, monitoring, overlay) with deliberate, *buyer-based* limitations that form the EE upgrade path. Gate set TBD (see §11 and pricing.md §1.1); candidates: single organization, local auth only, single-instance, standard branding, operational alerting only. Never node-count caps, never data-path limits, and **never reduced security hygiene** — mTLS, 2FA, signed updates, CVE patches are always CE; what gates is the security *operations* layer. | MVP |
| **Enterprise Edition (EE)** | Paid on-prem license over the same deployment: removes CE limits, adds enterprise modules (multi-tenancy, SSO/SAML/SCIM, HA clustering, white-label, and the **security-operations suite**: detailed/forensic audit logging + SIEM export, anomaly & suspicious-pattern detection/alerts, overlay flow logs, compliance reporting) + support. EE modules are a separate proprietary distribution — no license-key checks inside open code. In-place CE→EE upgrade is a hard product requirement. | v1.0 |
| **Logos Cloud (Global)** | Managed hosted control plane, per-node billing, bundles EE feature set at Team/MSP tiers. | v1.0 |
| **Logos Cloud RU** | Dedicated, fully isolated Russia region: data stored/processed in RF data centers (152-ФЗ, 242-ФЗ localization), RF legal entity as operator (Roskomnadzor registration), RUB billing via local processors, **separate Russian-language storefront with strict audience separation** — foreign users must not land on the RU offering (geo/billing-country checks; disjoint marketing), and the global site does not surface it. No cross-region data replication. | v1.x (after Global cloud; legal review gate) |

### 5.4 Future (v2+)

- Generic Linux node support (Debian/Alpine "node" package; nftables/networkd drivers alongside UCI).
- App marketplace on nodes (AdGuard Home, Tor, SQM presets as one-click apps).
- Client-device VPN access into overlays (user devices join via WireGuard profiles) — overlaps with Tailscale, evaluate against partnering/integrating instead.
- TR-069/USP southbound adapter for ISP-grade CPE.
- Federation between control planes.

## 6. Non-Functional Requirements

| Area | Requirement |
|---|---|
| **Security** | mTLS for agent↔server with per-node certs and rotation; enrollment codes single-use and expiring; signed agent/firmware artifacts; control plane never stores user LAN traffic; secrets encrypted at rest; rate-limited brute-force-proof enrollment endpoint; security disclosure policy. |
| **Footprint** | Agent ≤ 1 MB flash, ≤ 10 MB RSS idle; must run on 16 MB-flash/128 MB-RAM devices. Written in Go (single static binary, like Tailscale's OpenWrt approach) or Rust; no Python on the node. |
| **Resilience** | Node keeps routing if control plane is unreachable (control plane is *not* in the data path except relays); agent retries with backoff + jitter; config changes are atomic with auto-revert if the node loses connectivity after apply (like `uci` rollback / "safe mode"). |
| **Scale** | Single control-plane instance: 10k nodes; horizontal scaling for cloud. Heartbeat interval adaptive to fleet size. |
| **Privacy** | Self-hosted = zero phone-home (opt-in anonymous telemetry only). Cloud: metrics/config metadata only, documented data inventory, EU region option. |
| **Regional compliance** | Cloud must be **region-shardable with zero cross-region data flow** (accounts, registry, configs, metrics, backups, admin tooling per region) — prerequisite for Logos Cloud RU (152-ФЗ/242-ФЗ data localization) and future EU/other sovereignty regions. Storefront/signup must support per-region audience separation. |
| **Compatibility** | OpenWrt 23.05+ (opkg) and 24.10+/25.x (apk); all-target builds via OpenWrt SDK; LuCI coexistence (Logos does not replace local LuCI). |
| **Licensing** | Core (agent + control plane): Apache-2.0 or AGPLv3 (decide before first release — see market-analysis.md §licensing); cloud-only/enterprise modules in a separate repo/license (open-core split: SSO, white-label, long metric retention, audit export). |

## 7. High-Level Architecture

```
                    ┌─────────────────────────────────────────────┐
                    │              Control plane                  │
                    │  (self-hosted or Logos Cloud)               │
                    │                                             │
   Admin ──HTTPS──▶ │  Web panel (SPA) ── REST/gRPC API           │
                    │        │                                    │
                    │  Device registry · Config store (versioned) │
                    │  Template engine · Overlay coordinator      │
                    │  Metrics TSDB · Alerting · Audit log        │
                    │  Postgres ·(TSDB)· Relay servers (DERP-like)│
                    └────────────▲────────────────▲───────────────┘
                outbound mTLS WSS│                │
                 ┌───────────────┘                └───────────────┐
        ┌────────┴────────┐                        ┌──────────────┴──┐
        │  logos-agent    │◀═══ WireGuard mesh ═══▶│  logos-agent    │
        │  OpenWrt node A │     (p2p, relay        │  OpenWrt node B │
        │  ubus/uci/opkg  │      fallback)         │                 │
        └─────────────────┘                        └─────────────────┘
         LAN A devices ◀──────── overlay routing ────────▶ LAN B devices
```

Key decisions (to be validated in design docs, not here):

1. **Agent initiates everything.** Only outbound 443 needed; the persistent channel carries RPC (config, package ops, terminal) and telemetry. No inbound ports on the node.
2. **Control plane out of the data path.** Overlay traffic is p2p WireGuard; relays only when NAT traversal fails. A control-plane outage degrades management, never connectivity.
3. **UCI as the source of truth on-node**; the server stores desired state and reconciles — declarative, drift-aware, rollback-able.
4. **Boring, self-hostable stack**: one server binary (Go) + Postgres; panel is a bundled SPA. `docker compose up` in < 5 minutes is a hard requirement (this is the OpenWISP lesson).

## 8. Success Metrics

| Metric | Target (12 mo after MVP) |
|---|---|
| Time from flash → managed node (P50) | < 5 min |
| Self-hosted installs (telemetry opt-in + docker pulls as proxy) | 1,000+ |
| GitHub stars / active contributors | 3k+ / 10+ |
| Nodes under management (cloud) | 2,000+ |
| Cloud conversion: self-host orgs → paid cloud | ≥ 3% |
| Agent crash-free sessions | ≥ 99.9% |
| Control-plane-unreachable incidents causing node data-path outage | 0 |

## 9. Risks & Mitigations

| Risk | Impact | Mitigation |
|---|---|---|
| Bricking devices via bad config/firmware push | Trust-destroying | Atomic apply + auto-revert watchdog; staged rollouts; canary nodes; never touch bootloader. |
| Security: control plane = remote root on thousands of routers | Existential | mTLS, signed artifacts, minimal agent surface, external audit before 1.0, bug bounty, self-host option keeps blast radius per-org. |
| OpenWrt heterogeneity (hundreds of targets, opkg→apk migration) | High support load | Support top-N devices first; CI on real hardware lab; capability detection in agent. |
| Tailscale/NetBird add router management | Squeezed positioning | Differentiate on *owning the router* (packages, firmware, captive onboarding) and on open control plane; move fast on MSP features they won't build. |
| Self-hosters never convert to cloud | Revenue | Cloud-only conveniences (relays included, retention, image builder, SSO), per-node price low enough that ops time > subscription. |
| Solo-maintainer burnout | Project death | Small modular repos, contributor docs from day 1, boring tech, paid cloud funds maintenance. |

## 10. Out of Scope (v1)

- Replacing LuCI for deep single-device configuration (we template the common 90%).
- Managing non-Linux network gear (Mikrotik RouterOS, vendor firmware) — possibly via adapters later.
- Being a general VPN client for end-user laptops/phones (Tailscale's job; see v2 note).
- Traffic interception / DPI / content filtering as a service.
- Hardware sales.

## 11. Open Questions

1. License: Apache-2.0 (max adoption) vs AGPLv3 (protects against cloud resellers) — see market-analysis.md.
2. Agent transport: WebSocket+protobuf vs gRPC streams vs MQTT — prototype and measure on 128 MB-RAM targets.
3. Build our own DERP-like relay vs embed/reuse an existing one.
4. Overlay IPAM scheme: 100.64.0.0/10-style CGNAT space per tenant vs configurable.
5. Brand/name check: "logos" collision review (trademark, package names) before first public release.
6. **CE limitation set** (the EE upgrade path): final selection from the candidates in pricing.md §1.1 — decide before 1.0, publish with the stewardship promise.
7. **Logos Cloud RU**: legal structure (RF entity, Roskomnadzor operator registration, 187-ФЗ/СОРМ applicability), local infrastructure provider, RUB price list, and RU competitor research — all open; legal review is a launch gate.

---

*Related documents: [market-analysis.md](./market-analysis.md) · [pricing.md](./pricing.md)*
