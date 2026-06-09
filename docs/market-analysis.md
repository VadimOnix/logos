# Logos — Market & Competitive Analysis

> **Status:** Draft v0.1 · **Research date:** 2026-06-09 (all prices/facts accessed on this date unless noted)
> Companion to [PRD.md](./PRD.md) and [pricing.md](./pricing.md).

---

## Краткое резюме (Russian Executive Summary)

Рынок удалённого управления роутерами делится на четыре лагеря, и **ни один игрок не закрывает связку «открытый control plane + управление самим роутером (пакеты, конфиги, прошивки) + overlay-сеть между площадками» для стокового OpenWrt**:

1. **Open source (OpenWISP, GenieACS).** OpenWISP — единственная зрелая FOSS-платформа именно для OpenWrt: конфиги, мониторинг, прошивки, мульти-тенантность, даже provisioning WireGuard/VXLAN/ZeroTier-туннелей. Слабости: тяжёлый стек (~9 сервисов, Django-admin UI), сам проект признаёт его избыточность при <20 устройствах; нет публичного облака с прозрачным прайсингом. GenieACS — это TR-069 ACS для интернет-провайдеров: масштабируется до сотен тысяч CPE, но без мониторинга, overlay-сетей, мульти-тенантности, и стоковый OpenWrt не имеет TR-069-клиента.
2. **Вендорские облака (UniFi, Omada, Meraki, Aruba, Tanaza, Plasma Cloud).** Отполированный UX и zero-touch, но жёсткий hardware lock-in. Meraki — самый дорогой (~$115–200/год за AP) и «окирпичивает» устройства через 30 дней после истечения лицензии. Omada — ценовой ориентир низа рынка: ~$10–14/устройство/год.
3. **Облака для OpenWrt-железа (GL.iNet GoodCloud, Teltonika RMS, remote.it).** Подтверждают спрос на «роутер за NAT звонит домой», но GoodCloud и RMS привязаны к железу своих вендоров, а remote.it даёт только удалённый доступ без управления конфигурацией. Teltonika RMS — ориентир цены: ~$2.20/устройство/месяц.
4. **Mesh-VPN (Tailscale, ZeroTier, NetBird, Netmaker, Headscale).** Великолепная связность, но **никто из них не управляет роутером**: ни пакетов, ни UCI-конфигов, ни прошивок. Клиенты Tailscale/NetBird тяжелы для слабых устройств (~20+ МБ), у Netmaker вообще нет официального пакета OpenWrt.

**Ниша Logos**: открытый, self-hosted-able control plane уровня «docker compose up за 5 минут» + родное для OpenWrt управление флотом + встроенный WireGuard-overlay. Это пересечение OpenWISP × Tailscale, которое сегодня пусто — подтверждено независимыми исследованиями всех четырёх сегментов. Главные риски позиционирования: NetBird/Tailscale могут дорасти до управления устройствами; OpenWISP может упростить установку. Скорость и фокус на MSP-сценарии — ключ.

---

## 1. Landscape Overview

| Product | Manages router config/packages/firmware? | Works on stock OpenWrt? | Overlay / site-to-site | Self-hostable control plane | Free tier | Typical paid price |
|---|---|---|---|---|---|---|
| **OpenWISP** | ✅ full | ✅ (agent) | ✅ OpenVPN/WireGuard/VXLAN/ZeroTier provisioning | ✅ (GPL-3.0) | ✅ all of it | Support/SaaS: quote-only |
| **GenieACS** | ✅ via TR-069 | ⚠️ needs 3rd-party CWMP client | ❌ | ✅ (AGPL-3.0) | ✅ all of it | Support: quote-only |
| **UniFi Site Manager** | ✅ (UniFi HW only) | ❌ | ✅ Site Magic SD-WAN (free) | ✅ (controller, not panel) | ✅ | Hosting from $29/mo (≤100 devices) |
| **TP-Link Omada CBC** | ✅ (Omada HW only) | ❌ | ⚠️ manual IPsec/WG | ✅ (software controller) | ✅ Essentials | ~$10–14/device/yr |
| **Cisco Meraki** | ✅ (Meraki HW only) | ❌ | ✅ AutoVPN (best-in-class) | ❌ | ❌ | ~$115–200/AP/yr; MX $185–515+/yr |
| **Aruba Central** | ✅ (Aruba HW only) | ❌ | ✅ SD-Branch | ❌ | ❌ (90-day eval) | ~$93–300/AP/yr |
| **Tanaza** | ⚠️ APs only (TanazaOS) | ⚠️ flash TanazaOS | ❌ | ❌ | ✅ 3 APs | from $1.99/AP/mo |
| **Plasma Cloud** | ✅ (own APs only) | ❌ | ❌ | ❌ | ✅ lifetime | $0 (hardware-funded) |
| **GL.iNet GoodCloud** | ✅ (GL.iNet HW only) | ❌ | ⚠️ S2S needs public IP on main node; AstroWarp paid | ❌ | ✅ unlimited devices | Enterprise/VAR quote-only |
| **Teltonika RMS** | ✅ (Teltonika HW only) | ❌ | ✅ RMS VPN Hubs (data-billed) | ⚠️ paid private RMS (quote) | 30 days/device + 5 GB | ~$2.20/device/mo (reseller credits) |
| **remote.it** | ❌ access only | ✅ (official package) | ⚠️ service-level, not L3 | ❌ | 5 devices, non-commercial | $10–250/mo per license |
| **Tailscale** | ❌ | ✅ pkg, but ~20 MiB | ✅ (the product) | ❌ (Headscale unofficial) | 6 users, unlim. devices | $8–18/user/mo; +$1/mo per extra tagged resource |
| **ZeroTier** | ❌ | ✅ small native pkg + LuCI | ✅ (L2!) | ⚠️ non-commercial only; removed from binaries in 1.16.0 | 10 devices | $18/mo + $2/device |
| **NetBird** | ❌ | ⚠️ pkg lags upstream | ✅ | ✅ free full stack (AGPL/BSD) | 5 users / 100 machines | $6–12/user/mo |
| **Netmaker** | ❌ | ❌ no official pkg | ✅ (kernel WG, fastest) | ✅ CE (Apache-2.0) | self-hosted unlimited | ~$2/active connection/mo (usage-based) |
| **Headscale** | ❌ | (uses Tailscale client) | ✅ | ✅ it *is* one (BSD-3) | everything | — |

Full per-product source URLs are in §6.

## 2. Deep Dives by Category

### 2.1 Open-source OpenWrt/CPE management — our closest relatives

**OpenWISP** — Django-based modular platform; `openwisp-config` agent on the router polls the controller over HTTPS (optionally inside a management VPN), config modeled as NetJSON → rendered to UCI.

- **Strengths:** the only complete FOSS "UniFi-like" for stock OpenWrt; battle-tested (public Wi-Fi of Italian municipalities); built-in monitoring, firmware upgrader, RADIUS/captive portal, x509 PKI, multi-tenancy; provisions OpenVPN/WireGuard/WireGuard-over-VXLAN/ZeroTier tunnels from templates; very active (releases 1.2.1–1.2.3 in Mar–Apr 2026, commits on research day).
- **Weaknesses:** operational weight is the #1 community complaint — top HN comment: *"I need to administer 10-20 APs... why is it necessary to run 9 services?"*; maintainer concedes it's overkill under ~20 devices and the FAQ says so; production deploy is Ansible-first (Docker images long labeled not production-recommended); Django-admin UI, not a modern SPA; VPN provisioning is hub-and-spoke templating, **not** a Tailscale-style auto-mesh coordinator with NAT traversal/relays.
- **Monetization:** commercial support + hosted SaaS by openwisp OÜ — **no public prices** (quote-only). This leaves "transparent, affordable hosted option" unoccupied even within its own niche.
- **Lesson for Logos:** the demand is proven; the unmet requirement is *operational simplicity* (single binary + Postgres, 5-minute compose-up) and a first-class mesh. OpenWISP's Roadmap 2030 (TR-069/USP, NETCONF) shows where it's heading — ISP-grade CPE, not MSP/teams UX.

**GenieACS** — Node.js TR-069 (CWMP) ACS, MongoDB; scales to hundreds of thousands of CPEs; vendor-agnostic across DSL/fiber/LTE CPEs.

- **Strengths:** *the* open-source ACS for ISPs; programmable JS provisioning; clean REST NBI; proven at Community Fibre, Utility Warehouse.
- **Weaknesses:** single-maintainer cadence (v1.2.16 in Mar 2026 was a one-line fix; v1.3 "in development" for years); no TR-369/USP yet; RCE patched in v1.2.15 (exposed ACS = high-value target); no monitoring, no multi-tenancy, no overlay networking; stock OpenWrt lacks a TR-069 client.
- **Lesson for Logos:** TR-069 is the ISP world's language — a possible future southbound adapter (PRD v2+), not a starting point. Also a warning about bus factor.

**TIP OpenWiFi (uCentral)** — disaggregated OpenWrt-AP firmware + cloud controller; active but enterprise-consortium-driven, tiny community (gateway repo 34★), targets TIP-certified APs. Not a practical competitor for arbitrary routers.

### 2.2 Vendor cloud controllers — the UX bar and the lock-in foil

- **Ubiquiti UniFi:** Site Manager panel is free; Official Hosting from **$29/mo for up to 100 devices**; Site Magic SD-WAN (WireGuard) is free. Cheapest at scale, no expiry bricking — but UniFi hardware only and shallow multi-tenancy. *This is the UX bar Logos must meet.*
- **TP-Link Omada CBC:** free Essentials tier (unlimited devices, ZTP); Standard licenses ~**$13.99/device/yr** street (launch price $9.99). Devices keep last config on expiry. **The low-price anchor of the market.** US regulatory scrutiny of TP-Link (2025) is a real MSP procurement risk → opportunity for a neutral alternative.
- **Cisco Meraki:** the multi-tenant/MSP gold standard and best site-to-site UX (AutoVPN), but ~**$115–200/yr per AP**, small-branch MX **$185–515+/yr**, and devices **stop passing traffic 30 days after license expiry** — the most-hated lock-in in the industry and the strongest emotional argument for open infrastructure.
- **Aruba Central:** real MSP mode, AI-Ops; ~$93–300/AP/yr; notorious licensing complexity (GreenLake onboarding, per-device-class SKUs).
- **Tanaza:** hardware-independent (flash TanazaOS on third-party APs), from **$1.99/AP/mo**, free for 3 APs — proof that per-device ~$2/mo pricing works for MSPs; but APs-only, no routing/VPN story, small-vendor risk.
- **Plasma Cloud:** lifetime-free cloud bundled with own hardware; monetizes hardware + marketplace add-ons. Proof that "free panel" alone isn't a business — needs hardware or cloud margin.

### 2.3 OpenWrt-device clouds — demand proof for "phone home" management

- **GL.iNet GoodCloud:** free unlimited binding for GL.iNet routers, batch templates, remote SSH/web. Quote-only Enterprise/VAR. Site-to-Site limited (main node needs a public IP, ≤10 devices/network); newer AstroWarp overlay: $0 / $4.99 / $34.99+/mo tiers (data-capped). Community reports reliability issues ("semi-stable... would not use as the only configuration tool") and security unease about admin access routed via vendor cloud. *GoodCloud is the closest product to Logos's UX vision — minus openness, minus hardware neutrality.*
- **Teltonika RMS:** the most mature fleet platform in this group: credit model ≈ **$2.20/device/30 days** (reseller), 6 mo–10 yr packages, RMS Connect (access to LAN devices, data-billed), RMS VPN Hubs for multi-site. Teltonika-hardware-only; paid private/self-hosted RMS exists (quote-only). *Validates ~$2/device/mo as an accepted MSP price point and credit-based flexibility.*
- **remote.it:** hardware-agnostic remote access (official OpenWrt package), P2P with proxy fallback; $0 (5 devices, non-commercial) / $10 / $25 / $250 per-license tiers. Access only — no config/firmware management; pricing gets awkward between ~15 and 100 devices.

### 2.4 Mesh-VPN / overlay — the "virtual subnets" benchmark

- **Tailscale:** the UX standard. Free: 6 users, unlimited devices; Standard $8, Premium $18/user/mo (seat-based since "Pricing v4", Apr 2026); +$1/mo per tagged (server-type) resource beyond 50 — *a direct template for per-node pricing*. Closed, hosted-only control plane; OpenWrt package is heavy (~20–23 MiB; ~4.5 MiB UPX-minified build) — struggles on 8–16 MB-flash devices.
- **ZeroTier:** custom L2 protocol (not WireGuard) — unique LAN-like semantics (multicast, bridging); smallest native client of the commercial trio, official OpenWrt + LuCI packages, embedded in GL.iNet/Teltonika firmware. But: free tier cut 50→25→10 devices; BSL 1.1 license; self-hosted controller now non-commercial-only **and removed from official binaries (v1.16.0)** — significant community backlash. *A cautionary tale on tightening openness.*
- **NetBird:** the most complete fully-open Tailscale alternative — BSD-3 client + AGPL-3 control plane, free self-hosted full stack, cloud $0 (5 users/100 machines) / $6 / $12 per user/mo. Younger, OpenWrt package lags upstream badly (0.24.3 packaged vs 0.27 needed for exit nodes). *Closest philosophical neighbor; their gap is router management.*
- **Netmaker:** fastest raw WireGuard throughput, Apache-2.0 CE, but no official OpenWrt package (Docker-on-USB workaround), weaker NAT traversal (zero-touch traversal is paywalled), and a history of licensing/pricing whiplash (SSPL era, killed SaaS free tier in 2024). Usage-based ~$2/active connection/mo.
- **Headscale:** BSD-3 OSS reimplementation of Tailscale's coordination server (v0.28.0, Feb 2026); single binary + SQLite; tolerated by Tailscale Inc. Single-tailnet scope, no multi-tenancy, third-party UIs only. *Proof that an open control plane has organic demand — and that Tailscale treats it as a funnel, not a threat.*

**Confirmed gap:** none of the five does router configuration management — no package install, no UCI push, no firmware orchestration. They stop at the tunnel.

## 3. The Gap Logos Fills

```
                      Manages the router itself
                     (packages/config/firmware)
                            ▲
        Meraki/Omada/Aruba  │   ╔══════════╗
        GoodCloud/RMS       │   ║  LOGOS   ║     ← empty quadrant:
        (vendor-locked HW)  │   ╚══════════╝       open + any-OpenWrt +
                            │  OpenWISP             built-in auto-mesh
                            │  (no mesh coordinator,
                            │   heavy ops)
   ─────────────────────────┼─────────────────────────▶
                            │                Built-in overlay mesh
        remote.it           │   Tailscale/NetBird      (NAT traversal,
        (access only)       │   ZeroTier/Netmaker       virtual subnets)
                            │   (don't touch router config)
```

Logos's defensible combination:

1. **Hardware-neutral**: any device that runs OpenWrt (later any Linux) — vs. every vendor cloud and GoodCloud/RMS.
2. **Open, self-hostable control plane** — vs. Tailscale/ZeroTier/Meraki/GoodCloud; and *operationally trivial* (single binary + Postgres) — vs. OpenWISP's 9 services.
3. **Mesh as a first-class feature with NAT traversal and relays** — vs. OpenWISP's template-provisioned hub-and-spoke tunnels.
4. **Fleet management as a first-class feature** — vs. all mesh-VPN products.
5. **Transparent per-node cloud pricing** — vs. quote-only OpenWISP SaaS, GoodCloud Enterprise, private RMS (see [pricing.md](./pricing.md)).

### Threats to this positioning

| Threat | Likelihood | Counter |
|---|---|---|
| NetBird (open control plane, AGPL, fast velocity) adds config management | Medium | Move fast on OpenWrt-native depth (UCI/opkg/sysupgrade, captive onboarding) — far from their DNA; consider interop instead of war (Logos overlay could even speak their protocol). |
| Tailscale adds device management | Low-medium (they integrate with MDMs instead) | Same as above; their closed control plane keeps the open niche ours. |
| OpenWISP ships a simple compose deployment + polished UI | Medium | Out-innovate on mesh + onboarding UX; their architecture (Django monolith, Ansible-first) changes slowly; their roadmap points at ISP/TR-069, not MSP UX. |
| GL.iNet opens GoodCloud or pushes AstroWarp hard | Low | They monetize hardware; opening the cloud undermines their VAR program. Hardware-neutrality stays our edge. |
| ZeroTier-style enshittification accusations *against us* if open-core line is drawn badly | — | Draw the Tailscale/GitLab line publicly from day 1 (see pricing.md, stewardship promise). |

## 4. Strengths/Weaknesses Summary of Key Rivals

| Competitor | Their strength we must match | Their weakness we exploit |
|---|---|---|
| OpenWISP | Feature completeness, OpenWrt nativeness, community trust | Deployment weight, dated UX, no auto-mesh, quote-only SaaS |
| Tailscale | Onboarding magic, NAT traversal, docs, free tier generosity | Closed control plane, no router management, heavy client on small flash |
| NetBird | Fully open stack, modern panel, per-user pricing | No device config mgmt, OpenWrt package lag, young |
| Meraki | MSP multi-tenancy, AutoVPN UX, templates | Price, expiry-bricking, lock-in — our marketing foil |
| Omada | $10–14/yr price anchor, free tier, ZTP | Hardware lock-in, feature-stripped cloud tier |
| GoodCloud | Free unlimited fleet panel for its HW, batch ops, remote SSH | GL.iNet-only, reliability complaints, closed, S2S needs public IP |
| Teltonika RMS | Credit pricing flexibility, LAN-device access, VPN hubs | Teltonika-only, data-billed VPN, credit admin overhead |

## 5. Licensing Considerations (input for PRD §11 open question)

Evidence gathered (details in pricing research, §6 sources):

- **AGPL-3.0** is the proven "defensible but OSI" choice: Grafana's 2021 relicense and Plausible CE drew minimal backlash; NetBird uses it for exactly our architecture (BSD client + AGPL control plane). Blocks closed-source cloud resellers.
- **BSL/SSPL-style moves** (ZeroTier BSL, Netmaker's SSPL era) consistently produced community backlash and forks; avoid.
- **Apache-2.0 everything** (Supabase) works when the moat is operational rather than legal — but our control plane is a single easy binary by design, so the operational moat is deliberately weak.
- **GitLab's CE/EE structure** is the proven template for "limited open self-hosted + paid on-prem enterprise upgrade": MIT core, proprietary EE modules in the same deployment, gates chosen by *buyer* ("who cares the most about the feature"), and a published stewardship promise that nothing ever moves CE→EE. The risk to manage: if CE limits hit hobbyists (node caps, data-path limits), the community reacts as it did to ZeroTier — gates must be organizational features only.
- **Recommendation:** **agent + client libraries: Apache-2.0 or BSD** (maximize embedding in firmware builds, like NetBird/Tailscale clients); **control plane CE: AGPL-3.0**; EE/cloud modules (multi-tenancy, SSO, audit export, HA, white-label, billing) proprietary in a separate repo, forming the CE→EE on-prem upgrade path (final gate set: open question, pricing.md §1.1). Publish a GitLab-style stewardship promise: shipped CE features never move to paid.

### Regional note: Russia (research TODO)

Logos plans a dedicated, isolated RU cloud region with a separate storefront (pricing.md §3.2), motivated by 152-ФЗ/242-ФЗ data-localization requirements. Working market hypotheses, **not yet covered by the 2026-06-09 research pass**: Meraki/Aruba/UniFi cloud offerings are withdrawn from or impractical in the RF market; data-localization law blocks RU companies from foreign SaaS; local alternatives for multi-vendor OpenWrt fleet management are scarce. A dedicated competitor/pricing research pass for the RU market (incl. local NMS vendors, Kvant/«Базальт»-class import-substitution players, hosting costs at Selectel/Yandex Cloud/Rostelecom-DC) is required before roadmap commitment.

## 6. Sources

All accessed 2026-06-09. Quote-only / unverifiable items are marked in the text above; notable flags: OpenWISP and GenieACS commercial prices are not published; UniFi Hosting tiers above $29/mo are checkout-only; Omada multi-year US MSRP unpublished (street prices cited); Teltonika credit price is a US reseller's ($2.20), not an official list price; Netmaker SaaS base fee undisclosed; ZeroTier New-Central exact free-plan network/admin counts unconfirmed.

**Open source:** openwisp.org · openwisp.org/faq · openwisp.org/commercial-support · openwisp.io/docs/stable/general/architecture.html · openwisp.io/docs/dev/general/roadmap-2030.html · github.com/openwisp/openwisp-controller/releases · news.ycombinator.com/item?id=42950016 · genieacs.com · genieacs.com/support · github.com/genieacs/genieacs/releases · forum.genieacs.com/t/tr-369-usp-support/6326 · forum.openwrt.org/t/management-platforms-for-thousands-of-openwrt-routers/24330 · github.com/Telecominfraproject/wlan-cloud-ucentralgw

**Vendor clouds:** store.ui.com/us/en/products/unifi-hosting · help.ui.com/hc/en-us/articles/4415364143511 · help.ui.com/hc/en-us/articles/16750417515159 (Site Magic) · omadanetworks.com (.../omada-cloud-based-controller/) · cdw.com/product/.../8343487 (LIC-OCC-1YR) · tp-link.com/us/support/faq/3365 · documentation.meraki.com (Licensing FAQs) · rhinonetworks.com (MR/MX64 licenses) · hummingbirdnetworks.com, cloudwifiworks.com (MX67) · cdw.com/product/.../6484561 (Aruba Q9Y58AAE) · arubanetworking.hpe.com/techdocs/central (lifecycle) · tanaza.com/tanaza-pricing-plans · plasma-cloud.com

**OpenWrt-device clouds:** gl-inet.com/solutions/goodcloud · docs.gl-inet.com/router/en/4/interface_guide/cloud · gl-inet.com/solutions/site-to-site · astrowarp.net/pricing · forum.gl-inet.com/t/.../37656 · teltonika-networks.com/products/rms · wiki.teltonika-networks.com/view/RMS_VPN_Hubs · 5gstore.com/product/20774 · community.teltonika.lt/t/can-rms-be-self-hosted/6796 · remote.it/pricing · remote.it/getting-started/openwrt · github.com/remoteit

**Mesh-VPN:** tailscale.com/pricing · tailscale.com/blog/pricing-v4 · tailscale.com/docs/how-to/set-up-small-tailscale · zerotier.com/pricing · docs.zerotier.com/openwrt · discuss.zerotier.com/t/zerotier-1-16-0-self-hosted-controller/28269 · netbird.io/pricing · docs.netbird.io/manage/settings/plans-and-billing · github.com/netbirdio/netbird (LICENSE) · netmaker.io/pricing · netmaker.io/old-pricing · github.com/juanfont/headscale

---

*Related: [PRD.md](./PRD.md) · [pricing.md](./pricing.md)*
