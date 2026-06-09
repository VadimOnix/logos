# Logos — Pricing & Monetization Model (Cloud Distribution)

> **Status:** Draft v0.1 · **Research date:** 2026-06-09 (all competitor prices accessed on this date)
> Companion to [PRD.md](./PRD.md) and [market-analysis.md](./market-analysis.md).

---

## Краткое резюме (Russian Executive Summary)

**Модель — open-core по образцу Tailscale/Grafana:** ядро (агент + control plane) открыто и бесплатно self-hosted без лицензионных ключей; монетизация — managed-облако **Logos Cloud** с тарификацией **за подключённую ноду** и проприетарными cloud/enterprise-модулями (SSO/SAML, audit-export, white-label, биллинг).

**Предлагаемая тарифная сетка облака:**

| Тариф | Цена | Для кого |
|---|---|---|
| **Free** | $0 — до 5 нод, 1 overlay-сеть, 7 дней истории метрик | Энтузиасты; маркетинговый бюджет проекта |
| **Team** | **$2/нода/мес** (минимум $10/мес), 30 дней метрик, алерты, image builder | Распределённые команды, малый бизнес |
| **MSP / Business** | **$4/нода/мес** с объёмными скидками (от $3 при 100+, от $2.50 при 500+), мульти-тенантность, SSO, audit log, white-label, консолидированный биллинг | MSP, интеграторы |
| **Enterprise** | Кастом (BYO-cloud, SLA, годовой инвойс) | Крупные парки, провайдеры |

**Обоснование цены $2–4/нода/мес из рынка:** Teltonika RMS ≈ $2.20/устройство/мес; Tanaza от $1.99/AP/мес; Omada ≈ $10–14/устройство/год (нижний якорь); Tailscale берёт $1/мес за каждый tagged-ресурс сверх лимита и $8–18 за пользователя; Meraki ≈ $10–17/мес за AP (верхний якорь). Logos даёт «управление + mesh» — две ценности в одной ноде, поэтому $2–4/мес между якорями Teltonika и Meraki обоснованы.

**Юнит-экономика:** себестоимость control plane ≈ $0.05–0.15/нода/мес (heartbeat + метрики + хранение), главный COGS — relay-трафик для нод за жёстким NAT (бюджетируем ~1–3 ГБ/нода/мес ≈ $0.01–0.03 при egress $0.01/ГБ у Hetzner-класса провайдеров); валовая маржа тарифа Team ≥ 90% при честном лимитировании relay. Free-тариф защищаем reclaim-политикой неактивных нод (урок Supabase).

**Ключевые принципы** (из анализа Tailscale/Grafana/GitLab/Supabase/Plausible): предсказуемый биллинг по назначенным нодам (не «активным»); граница open/paid — по покупателю фичи (инженеру — всё открыто, менеджеру — Team, комплаенсу — MSP/Enterprise); публичное обещание «фичи никогда не уезжают из OSS в платное»; AGPL для control plane + Apache/BSD для агента.

---

## 1. Model: Open-Core, Cloud-Funded

```
┌────────────────────────────────────────────────────────────────┐
│  OPEN (free forever, no license keys)                          │
│  • logos-agent (Apache-2.0/BSD) — embeddable in firmware       │
│  • Control plane core (AGPL-3.0): enrollment, config push,     │
│    packages, monitoring, overlay coordinator, basic alerts,    │
│    single-org RBAC, API                                        │
│  • Relay server (like Tailscale's open DERP)                   │
├────────────────────────────────────────────────────────────────┤
│  PAID — Logos Cloud (hosted) and/or commercial modules         │
│  • Hosting itself: HA control plane, relay fleet, backups      │
│  • SSO/SAML/SCIM · audit-log export/SIEM · white-label         │
│  • Long metric retention · image-builder service at scale      │
│  • Multi-tenant MSP console · consolidated billing             │
└────────────────────────────────────────────────────────────────┘
```

Rationale (evidence in [market-analysis.md](./market-analysis.md) §5 and research sources below):

- **The Tailscale cut line** — open clients + open relay, monetize the hosted control plane — is the cleanest and best-received split; even a community reimplementation of our server (a "Headscale of Logos") would act as a funnel, not a threat.
- **The GitLab rule** for what's paid: gate by *buyer*, not capability. Everything an engineer needs to run a network is open; team-scale collaboration is Team; compliance/management-at-scale (SSO, audit, white-label) is MSP/Enterprise.
- **Stewardship promise published on day 1:** features never move from open to paid (anti-ZeroTier/Netmaker pattern — both suffered backlash after tightening: ZeroTier free tier 50→25→10 devices and controller removal from binaries; Netmaker's SSPL era and killed free SaaS tier).

## 2. Competitor Price Anchors (accessed 2026-06-09)

### Per-managed-device platforms (what MSPs compare us against)

| Product | Price, normalized | Notes |
|---|---|---|
| TP-Link Omada CBC | **~$0.85–1.17/device/mo** ($10–14/yr street) | Low anchor; cloud tier is feature-stripped, HW-locked |
| Tanaza | **from $1.99/AP/mo** | HW-independent APs; validates ~$2/mo psychology |
| Teltonika RMS | **~$2.20/device/mo** (reseller credit) | Closest functional analog (fleet mgmt + VPN hubs); HW-locked |
| Aruba Central (AP Foundation) | **~$7.75–12.50/AP/mo** ($93–150/yr) | Enterprise tier |
| Cisco Meraki (MR Enterprise) | **~$9.50–16.70/AP/mo** ($115–200/yr) | High anchor; devices brick on expiry |
| UniFi Official Hosting | **$29/mo for ≤100 devices** (~$0.29/device/mo at full load) | Flat-fee model; HW-locked, panel itself free |
| GL.iNet GoodCloud | Free (personal) / quote (Enterprise/VAR) | Demand proof for free fleet panel; closed |

### Overlay-network platforms (what teams compare us against)

| Product | Price | Notes |
|---|---|---|
| Tailscale | Free: 6 users, unlim. devices; **$8/user/mo** Standard, **$18** Premium; **+$1/mo per tagged resource** >50 | The $1/mo/resource is a direct per-node template |
| ZeroTier | Free: 10 devices; **$18/mo incl. 10 devices + $2/device/mo**; $179/mo incl. 100 + $1.80 | Per-device pricing precedent: $1.80–2.00 |
| NetBird | Free: 5 users/100 machines; **$6/user/mo** Team, $12 Business | Self-hosted free unlimited |
| Netmaker | CE free self-hosted; SaaS ~**$2/active connection/mo**, declining tiers $1 → $0.50 → $0.25/device | Usage-based per-device precedent |
| remote.it | Free 5 devices; $10/$25/$250 per-license tiers | Awkward mid-range — gap to avoid |

### Open-core SaaS structure references

| Company | Ladder | Lesson |
|---|---|---|
| Tailscale | $0 / $8 / $18 / custom | 3 price points + custom; free tier = marketing budget ("How our free plan stays free"); switched usage→seat billing in 2026 because buyers want **predictability** |
| Grafana Cloud | $0 / PAYG ($19 platform fee + usage) / $25k-min annual | Generous free tier; platform-fee floor; enterprise commit floor |
| Supabase | $0 / $25 / $599 / custom | Resource-backed free tier needs **idle reclamation** (1-week pause); plan fee is a floor, usage is the meter |
| GitLab | $0 / $29 / $99 per user | Buyer-based gating doctrine; published stewardship promise |
| Plausible | from $9/mo cloud; AGPL CE self-hosted | Small-team viability ($3.1M ARR, 10 people) on "cloud is the only funding" |

## 3. Proposed Logos Cloud Tiers

Unit of billing: **assigned node** (a device enrolled in the tenant), not "active node" — predictable invoices (the lesson Tailscale learned the expensive way). Nodes can be unassigned/archived to stop billing.

| | **Free** | **Team** | **MSP** | **Enterprise** |
|---|---|---|---|---|
| Price | $0 | **$2/node/mo** (min $10/mo, annual −20%) | **$4/node/mo**, volume: ≥100 → $3, ≥500 → $2.50 | Custom, annual invoice |
| Nodes | up to 5 | unlimited | unlimited | unlimited |
| Overlay networks | 1 | 10 | unlimited | unlimited |
| Organizations (tenants) | 1 | 1 | **multi-tenant console** | multi-tenant + federation |
| Metric retention | 7 days | 30 days | 90 days | custom |
| Relay traffic included | 1 GB/node/mo | 5 GB/node/mo, then $0.05/GB | 5 GB/node/mo, then $0.05/GB | custom |
| Alerts | email | email/webhook/Telegram/Slack | + escalation policies | + custom |
| Image builder | community images | per-tenant baked images | white-label images | custom |
| Auth | email/password, 2FA | + OIDC | + SAML/SSO, SCIM | + custom IdP |
| Audit log | — | 30 days | 1 year + SIEM export | custom |
| White-label panel | — | — | ✅ | ✅ |
| Support | community | standard (next business day) | priority | SLA, dedicated |
| Extras | — | — | consolidated billing across tenants, partner margin program | BYO-cloud deployment, MSA |

Self-hosted remains free and unlimited forever; paid self-hosted support contracts (and an optional "supported enterprise self-hosted" bundle with the proprietary modules) can be added later without changing this table.

### Why these numbers

- **$2/node/mo (Team)** sits exactly on the validated cluster: Teltonika $2.20, Tanaza $1.99, ZeroTier $1.80–2.00, Netmaker $1–2. We deliver strictly more per node (management **and** mesh), but as a new entrant we price *at* the cluster, not above it. A 10-node distributed team pays $20/mo — vs. $64/mo for 8 Tailscale seats (which wouldn't manage the routers at all).
- **$10/mo minimum** on Team avoids the unprofitable $2–6/mo long tail (Grafana's $19 platform-fee logic) without remote.it's awkward per-license jumps.
- **$4/node/mo (MSP)** ≈ half of Aruba Foundation, ~⅓ of Meraki, for hardware-neutral gear with no expiry-bricking; volume breaks mirror Netmaker's declining schedule and land a 100-node MSP at $300/mo — well inside the budget envelope MSPs already pay vendors.
- **Free = 5 nodes** covers home + parents + VPS (the Dmitry persona, who becomes our evangelist) but not a billable client deployment; 1 overlay network keeps it personal-scale. More generous than ZeroTier (10 devices but neutered self-hosting), stingier than Tailscale's unlimited devices — because our nodes carry real relay/metrics COGS (see §4).
- **3 price points + custom** matches the observed ladder ($0/$8/$18 Tailscale; $0/$29/$99 GitLab); tier jumps stay ≤ ~2×.

### Billing mechanics

- Monthly per-node proration; archive a node → billing stops (Teltonika's credit lapse → service suspension is the anti-pattern: nodes **never lose data-path** when payment lapses, they lose cloud management after a 30-day grace — the anti-Meraki guarantee, stated loudly in marketing).
- Annual prepay −20%; MSP partner margin 15–20% off list for certified partners.
- Free-tier hygiene (Supabase lesson, applied pre-emptively): nodes offline > 90 days on Free are auto-archived (one-click restore); no compute is reserved per free tenant, so no Supabase-style pause resentment.

## 4. Unit Economics (order-of-magnitude, to validate before launch)

Assumptions for the managed cloud on Hetzner/OVH-class infrastructure (egress ≈ $0.01/GB or included; 3-node HA control plane + Postgres + TSDB ≈ $150–300/mo serving ~10k nodes):

| Cost component | Per node / month | Notes |
|---|---|---|
| Control-plane compute + DB share | ~$0.02–0.05 | heartbeats (30–60 s), config ops are rare events |
| Metrics ingest + storage (30 d) | ~$0.02–0.05 | ~10 series/node, 1-min resolution, downsampled |
| Relay (DERP-like) traffic | ~$0.01–0.10 | the volatile one: $0 for p2p-capable nodes; budget 1–3 GB/node/mo fleet-average at $0.01/GB; hard-NAT-heavy fleets can spike — hence metered relay overage at $0.05/GB |
| Support + payment fees amortized | ~$0.10–0.25 | dominates at small scale; Stripe ~2.9% + $0.30 |
| **Total COGS** | **~$0.15–0.45** | |
| **Gross margin @ $2 (Team)** | **~78–92%** | healthy SaaS margin |
| **Gross margin @ $4 with breaks (MSP)** | **~85–94%** | |

Break-even sanity check: at $2/node and ~$1.6k/mo fixed costs (infra + tooling, pre-salary), ~1,000 paid nodes ≈ break-even on infrastructure and a meaningful maintainer stipend; 5,000 paid nodes ≈ one full-time salary + infra. Plausible's trajectory ($3.1M ARR, 10 people, bootstrapped) is the realistic upside model for this category.

**Validation TODO before launch:** measure real relay-traffic distribution in beta (the only COGS able to invert the margin); load-test heartbeat/metrics cost per 10k nodes; confirm payment-fee drag at $10-minimum invoices.

## 5. Principles Checklist (from open-core research)

1. **Free tier is a real free tier, not a trial** — it's the marketing budget (Tailscale doctrine); size it just above homelab scale.
2. **Predictable unit**: assigned nodes, never "active this month" (Tailscale's v4 reversal).
3. **Buyer-based gates** (GitLab): engineer → open; team lead → Team; compliance/MSP-owner → MSP/Enterprise. The standard paid set — SAML/SSO, SCIM, audit/SIEM, white-label, SLA — matches what every reference company gates.
4. **Stewardship promise published**: no feature ever moves open → paid; accept community PRs that open paid features at our discretion, GitLab-style.
5. **License split**: agent Apache-2.0/BSD; control plane AGPL-3.0; cloud modules proprietary, separate repo (NetBird-proven structure; avoids BSL/SSPL backlash).
6. **Never brick** (anti-Meraki): payment lapse degrades cloud management after grace, never the data path. Make this a published guarantee — it is a sales weapon against the strongest incumbent's biggest weakness.
7. **MSP margin story from day 1**: volume breaks + partner discount + consolidated billing — none of the mesh-VPN vendors had this early; it's our wedge into the channel.
8. **Keep a self-serve paid tier between free and enterprise** from the start (Tailscale's costly Personal Plus retirement / re-segmentation lesson).

## 6. Sources

Accessed 2026-06-09. Flags: Teltonika credit price is a US reseller's ($2.20, 5gstore.com) — no official list price exists; Omada US MSRP unpublished (street $13.99/yr, cdw.com); Grafana per-unit telemetry rates ($8/1k series, $0.50/GB) are JS-rendered and corroborated by third-party trackers only; Netmaker SaaS base fee undisclosed; UniFi Hosting tiers above $29/mo are checkout-only.

tailscale.com/pricing · tailscale.com/blog/pricing-v4 · tailscale.com/blog/free-plan · tailscale.com/opensource · zerotier.com/pricing · netbird.io/pricing · netmaker.io/pricing · netmaker.io/old-pricing · supabase.com/pricing · grafana.com/pricing · grafana.com/licensing · grafana.com/blog/grafana-loki-tempo-relicensing-to-agplv3 · about.gitlab.com/pricing · handbook.gitlab.com/handbook/company/stewardship/ · plausible.io (pricing) · plausible.io/blog/community-edition · teltonika-networks.com/products/rms · 5gstore.com/product/20774 · tanaza.com/tanaza-pricing-plans · cdw.com (Omada LIC-OCC-1YR #8343487; Aruba Q9Y58AAE #6484561) · rhinonetworks.com (Meraki MR/MX64) · documentation.meraki.com (Licensing FAQs) · store.ui.com/us/en/products/unifi-hosting · remote.it/pricing · astrowarp.net/pricing · getlatka.com/companies/plausible-analytics · news.ycombinator.com/item?id=47691281 (Tailscale v4 reaction)

---

*Related: [PRD.md](./PRD.md) · [market-analysis.md](./market-analysis.md)*
