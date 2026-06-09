# logos

> everywhere anywhere access

**Logos** is an open-source SaaS for administering fleets of OpenWrt routers (and, eventually, any Linux node that manages network traffic). A lightweight agent on the router dials out to a control plane — self-hosted or managed cloud — giving you a single admin panel for enrollment, configuration, package management, monitoring, and WireGuard-based virtual subnets that join routers (and the LANs behind them) into one network.

**Logos** — open-source платформа для централизованного администрирования роутеров на OpenWrt (а в перспективе — любых Linux-нод, управляющих сетевым трафиком). Лёгкий агент на роутере устанавливает исходящее соединение с головным сервером (self-hosted или облачным), давая единую панель для подключения устройств, управления конфигурацией и пакетами, мониторинга и объединения роутеров в виртуальные подсети на WireGuard.

## Documentation / Документация

| Document | Contents |
|---|---|
| [docs/PRD.md](docs/PRD.md) | Product Requirements Document: vision, personas, user flows (zero-touch enrollment, fleet admin, overlay networks), requirements by release, architecture, risks. *Включает резюме на русском.* |
| [docs/market-analysis.md](docs/market-analysis.md) | Competitive landscape: OpenWISP, GenieACS, UniFi/Omada/Meraki, GL.iNet GoodCloud, Teltonika RMS, Tailscale/ZeroTier/NetBird/Netmaker — strengths, weaknesses, positioning. *Включает резюме на русском.* |
| [docs/pricing.md](docs/pricing.md) | Open-core monetization model: free self-hosted core + managed cloud, tier design, competitor pricing references, unit economics. *Включает резюме на русском.* |

## Status

Pre-development: product definition phase. No code yet — start with the PRD.
