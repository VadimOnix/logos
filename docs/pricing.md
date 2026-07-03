# Logos — Pricing & Monetization Model (Cloud Distribution)

> **Status:** Draft v0.1 · **Research date:** 2026-06-09 (all competitor prices accessed on this date)
> Companion to [PRD.md](./PRD.md) and [market-analysis.md](./market-analysis.md).

---

## Краткое резюме (Russian Executive Summary)

**Модель — open-core с разделением изданий по образцу GitLab CE/EE:**

- **Community Edition (CE)** — открытая self-hosted версия, бесплатно, с возможностью добровольных донатов (GitHub Sponsors / Open Collective — сигнал здоровья комьюнити, не строка дохода). Содержит всё ядро (агент, enrollment, конфиги, пакеты, мониторинг, overlay), но **с намеренными ограничениями**, снимаемыми апгрейдом до Enterprise (точный набор гейтов — открытый вопрос, кандидаты в §1.1: мульти-тенантность, SSO/SAML, audit-export, **security operations** — детальное аудит-логирование, детект подозрительных паттернов, flow logs, — HA-кластеризация, white-label). Жёсткий принцип: **CE никогда не менее защищённая, только менее наблюдаемая** — гигиена безопасности (mTLS, 2FA, подписанные обновления, патчи) всегда в CE.
- **Enterprise Edition (EE, on-premise)** — платная лицензия на тот же self-hosted дистрибутив: снимает ограничения CE и добавляет enterprise-модули + поддержку. Ориентир цены — $3–4/нода/мес в годовом исчислении (черновик, §3.1).
- **Logos Cloud** — managed-облако с тарификацией **за подключённую ноду**.

**Отдельное направление — Logos Cloud RU (§3.2):** изолированный регион для абонентов из России с хранением и обработкой данных на территории РФ (152-ФЗ, локализация по 242-ФЗ), оплатой в рублях через локальные платёжные системы и **отдельным сайтом-витриной**, на который зарубежные пользователи не попадают (и наоборот — RU-витрина не связана с глобальным сайтом). Потребует отдельного юрлица, регистрации оператора ПДн в Роскомнадзоре и полной инфраструктурной изоляции региона.

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

## 1. Model: Open-Core with a CE/EE Edition Split (GitLab-style)

```
┌────────────────────────────────────────────────────────────────┐
│  COMMUNITY EDITION (CE) — open source, free self-hosted        │
│  • logos-agent (Apache-2.0/BSD) — embeddable in firmware       │
│  • Control plane core (AGPL-3.0): enrollment, config push,     │
│    packages, monitoring, overlay coordinator, basic alerts,    │
│    single-org RBAC, API                                        │
│  • Relay server (like Tailscale's open DERP)                   │
│  • Deliberately limited — see §1.1 (upgrade path to EE)        │
├────────────────────────────────────────────────────────────────┤
│  ENTERPRISE EDITION (EE) — paid license, same on-prem deploy   │
│  • Removes CE limits + adds enterprise modules:                │
│    SSO/SAML/SCIM · audit-log export/SIEM · white-label ·       │
│    multi-tenant MSP console · HA clustering · support          │
├────────────────────────────────────────────────────────────────┤
│  LOGOS CLOUD — managed hosting (per-node billing)              │
│  • Two isolated offerings: Global and RU region (§3.2)         │
│  • HA control plane, relay fleet, backups, image builder       │
│  • Cloud tiers bundle EE feature set at Team/MSP levels        │
└────────────────────────────────────────────────────────────────┘
```

Rationale (evidence in [market-analysis.md](./market-analysis.md) §5 and research sources below):

- **The Tailscale cut line** — open clients + open relay — stays: the agent must be maximally embeddable in firmware images.
- **The GitLab CE/EE precedent** is the model: a genuinely useful open core, plus a paid Enterprise Edition *of the same self-hosted product* so an on-prem customer has an upgrade path without migrating to the cloud. GitLab's rule decides what gates: features whose buyer is a manager/compliance officer go to EE; features whose buyer is the engineer stay in CE.
- **Stewardship promise published on day 1, reworded for the edition split:** *a feature that has shipped in CE never moves to EE or cloud* — new enterprise-buyer features may launch as EE-only, but nothing is ever clawed back (anti-ZeroTier/Netmaker pattern — both suffered backlash after tightening: ZeroTier free tier 50→25→10 devices and controller removal from binaries; Netmaker's SSPL era and killed free SaaS tier). This keeps the CE limitations honest: they are *absences*, never *removals*.

### 1.1 CE limitations — the EE upgrade path (OPEN QUESTION)

The exact CE gate set is **not decided yet**. Constraints any candidate must satisfy:

1. **Never the data path.** Mesh connectivity, config push, package management on any number of nodes stays in CE — crippling the core would betray the open-source positioning and kill the funnel (see ZeroTier backlash, market-analysis §2.4).
2. **Buyer-based** (GitLab rule): the limitation must only hurt once an organization — not a hobbyist — depends on it.
3. **Enforceable without phone-home DRM** in AGPL code: EE modules live in a separate proprietary repo/binary, CE simply doesn't contain them (GitLab/NetBird structure), rather than license-key checks inside open code.
4. **CE is never less *secure*, only less *observable*.** Security **hygiene** — mTLS, secure enrollment, 2FA, signed artifacts, CVE patches, basic operational alerts — is always CE: a product that is remote root on fleets of routers cannot ship a "less safe" free edition (PRD risk §9). What gates to EE is **security operations**: the visibility/detection/compliance layer whose buyer is a security or compliance officer. This mirrors the market exactly: Tailscale holds network flow logs and SIEM log streaming at Premium ($18/user), Grafana holds audit logging at Enterprise, GitLab holds security scanning at Ultimate.

Candidate gates, ranked by fit with these constraints:

| Candidate CE limit | EE unlock | Fit |
|---|---|---|
| Single organization (no multi-tenancy) | Multi-tenant MSP console | ✅ strong — MSPs are the paying buyer by definition |
| Local auth only (email/password, 2FA) | OIDC/SAML/SSO, SCIM | ✅ strong — the industry-standard gate (Grafana, Tailscale, Supabase, GitLab all do it) |
| Basic audit (in-app log, short retention) | Audit export/SIEM streaming, long retention | ✅ strong |
| Basic operational alerting only | **Security operations suite**: detailed/forensic audit logging, anomaly & suspicious-pattern detection and alerts, overlay flow logs, compliance reporting | ✅ strong — the Tailscale Premium / Grafana Enterprise pattern; bounded by constraint 4 (hygiene never gated) |
| Single-instance deployment | HA clustering, horizontal scaling | ✅ strong — only large fleets need it |
| Standard branding | White-label panel | ✅ strong |
| Community support | Support contract w/ SLA | ✅ always |
| Metric retention cap (e.g., 30 days) | Unlimited/configurable retention | ⚠️ medium — easy to work around externally; fine as soft gate |
| Node-count cap in CE | Uncapped | ❌ avoid — punishes the engineer-buyer, invites forks; node caps belong to *cloud* tiers only |

**Decision needed before 1.0** (tracked as PRD §11 open question): pick the final set from the ✅ rows. Recommendation: ship CE = single-org, local-auth, single-instance, standard-branding, operational-alerting-only; EE = everything above it (multi-tenancy, SSO, HA, white-label, security-operations suite).

### 1.2 Donations for CE

CE accepts voluntary donations (GitHub Sponsors / Open Collective) from day 1. Treated strictly as a community-health signal and goodwill channel, **never as a revenue line in the financial model** — across every studied project donations are negligible next to cloud/EE revenue (Plausible: "the cloud subscription is our only source of funding"; Headscale runs on sponsorship precisely because it has no commercial arm). Optional perk ideas (no obligations attached): sponsor badge in the panel footer, name in release notes.

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

Self-hosted **CE** remains free with unlimited nodes (its limits are feature gates, never node caps — see §1.1); the paid self-hosted path is **EE** below.

### 3.1 Enterprise Edition (on-premise) pricing — draft

| | **EE (on-prem)** |
|---|---|
| Price | **$3.50/node/mo equivalent, billed annually** ($42/node/yr), volume breaks mirroring cloud MSP tier (≥100 → $3, ≥500 → $2.50) |
| Minimum | 25 nodes ($1,050/yr floor) — keeps EE from cannibalizing cloud Team |
| Includes | All CE features + EE modules (§1.1: multi-tenancy, SSO/SAML/SCIM, audit/SIEM, HA, white-label) + standard support; priority support and custom SLA as add-ons |
| Logic | Priced between cloud Team ($2 incl. hosting) and cloud MSP ($4 incl. hosting): the customer provides the infrastructure, we provide software + support. Anchors: Netmaker on-prem Pro self-service licenses; Teltonika "private RMS" (quote-only — we win by publishing the price); GitLab Premium $29/user/yr-scale economics translated to per-node units |
| Trial | 30-day full EE trial on top of any CE install (Netmaker's 2-week on-prem trial precedent, doubled) |

Status: **draft numbers** — validate against the final CE gate set (§1.1) and first 10 design-partner conversations before publishing.

### 3.2 Logos Cloud RU — dedicated Russia region

A separate cloud offering for customers in Russia, driven by data-localization law; **architecturally identical, commercially and legally isolated** from the global cloud.

| Aspect | Decision |
|---|---|
| **Storefront** | Separate marketing site + signup (e.g., `logos-cloud.ru`), in Russian, with RU payment methods. **Strict audience separation:** the global site does not link to or mention the RU storefront, and the RU storefront does not serve foreign users — geo/IP + billing-country checks at signup, marketing channels kept disjoint. Goal: foreign users never land on the RU offering and vice versa. |
| **Data residency** | All control-plane data (accounts, device registry, configs, metrics, logs) stored and processed in data centers on RF territory — compliance with **152-ФЗ «О персональных данных»** including the **242-ФЗ localization requirement** (personal data of RF citizens recorded/stored in databases located in Russia). Candidate infrastructure: Selectel / Yandex Cloud / Rostelecom-DC (cost research TODO). |
| **Legal** | Separate RF legal entity as the contracting party and personal-data operator; **operator registration with Roskomnadzor**; RU-law privacy policy and DPA; assess applicability of **187-ФЗ (КИИ)** if customers include critical-infrastructure operators, and of СОРМ-related obligations — legal review required before launch (flagged: this analysis is a product assumption, not legal advice). |
| **Isolation** | Dedicated control-plane deployment, relay fleet, database, backups and admin tooling inside RF; no cross-region replication with the global cloud; separate status page and support queue. An RU tenant's data never leaves the region. |
| **Billing** | RUB pricing via local processors (ЮKassa/CloudPayments-class); price list set independently of USD list (purchasing-power and cost-base adjusted, draft: ₽150–200/нода/мес Team-уровень — validate against local hosting costs and willingness-to-pay). |
| **Self-hosted angle** | CE/EE work anywhere by definition — for RF organizations that cannot use any foreign-linked cloud at all, **EE on-prem is the compliance-maximalist answer**, and the RU storefront sells both (RU cloud + EE licenses). |

Why a separate region is worth the overhead: global vendor clouds (Meraki, Aruba Central, UniFi hosted) have withdrawn from or are unusable in the RF market, while data-localization law blocks RU companies from foreign SaaS — a supply vacuum for exactly our product category. *Flag: RU competitor/pricing landscape was not covered by the 2026-06-09 research pass — dedicated research TODO before committing roadmap dates.*

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
4. **Stewardship promise published**: no shipped CE feature ever moves to EE/paid; new enterprise-buyer features may launch EE-only; accept community PRs that open paid features at our discretion, GitLab-style.
5. **License split**: agent Apache-2.0/BSD; control plane (CE) AGPL-3.0; EE/cloud modules proprietary, separate repo — no license-key checks inside open code (NetBird/GitLab-proven structure; avoids BSL/SSPL backlash).
6. **Never brick** (anti-Meraki): payment lapse degrades cloud management after grace, never the data path. Make this a published guarantee — it is a sales weapon against the strongest incumbent's biggest weakness.
7. **MSP margin story from day 1**: volume breaks + partner discount + consolidated billing — none of the mesh-VPN vendors had this early; it's our wedge into the channel.
8. **Keep a self-serve paid tier between free and enterprise** from the start (Tailscale's costly Personal Plus retirement / re-segmentation lesson).

## 6. Sources

Accessed 2026-06-09. Flags: Teltonika credit price is a US reseller's ($2.20, 5gstore.com) — no official list price exists; Omada US MSRP unpublished (street $13.99/yr, cdw.com); Grafana per-unit telemetry rates ($8/1k series, $0.50/GB) are JS-rendered and corroborated by third-party trackers only; Netmaker SaaS base fee undisclosed; UniFi Hosting tiers above $29/mo are checkout-only.

tailscale.com/pricing · tailscale.com/blog/pricing-v4 · tailscale.com/blog/free-plan · tailscale.com/opensource · zerotier.com/pricing · netbird.io/pricing · netmaker.io/pricing · netmaker.io/old-pricing · supabase.com/pricing · grafana.com/pricing · grafana.com/licensing · grafana.com/blog/grafana-loki-tempo-relicensing-to-agplv3 · about.gitlab.com/pricing · handbook.gitlab.com/handbook/company/stewardship/ · plausible.io (pricing) · plausible.io/blog/community-edition · teltonika-networks.com/products/rms · 5gstore.com/product/20774 · tanaza.com/tanaza-pricing-plans · cdw.com (Omada LIC-OCC-1YR #8343487; Aruba Q9Y58AAE #6484561) · rhinonetworks.com (Meraki MR/MX64) · documentation.meraki.com (Licensing FAQs) · store.ui.com/us/en/products/unifi-hosting · remote.it/pricing · astrowarp.net/pricing · getlatka.com/companies/plausible-analytics · news.ycombinator.com/item?id=47691281 (Tailscale v4 reaction)

---

*Related: [PRD.md](./PRD.md) · [market-analysis.md](./market-analysis.md)*
