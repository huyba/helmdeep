# Doc 14 — Competitive Teardown: Sycamore

**Purpose:** map Sycamore's complete *public* capability surface, identify what our design (docs 00–13) already covers, what it missed entirely, and where we should deliberately diverge.

**Source quality caveat:** Sycamore has **no public technical documentation, no API reference, no architecture docs**. The product is early access / waitlist (`forge.sycamore.so`). Everything below is reverse-engineered from their marketing site, `llms.txt`, pricing page, Labs research page, and press coverage as of Sept 2026. Treat capability claims as *stated intent*, not verified implementation — a company 6 months past seed has not shipped all of this.

---

## 1. The Big Miss: Sycamore is TWO products

Our docs 00–13 designed a governance runtime. That maps to **Guard** only. Sycamore's other half — **Forge** — is an entirely separate product we did not design at all, and it is arguably the one that sells.

| | **Forge** | **Guard** |
|---|---|---|
| Tagline | "Turn hard business processes into working software" | "Govern every AI system from request to runtime" |
| Job | Intent → production agentic software | Control plane for the whole AI estate |
| Buyer | Business owner / operator / builder | CISO / GRC / platform |
| Pricing | $50/mo Pro, $200/mo Max (flat, not per-seat, with usage credits) | Enterprise only, custom |
| Our coverage | **~10%** (mentioned as P2 "natural-language scaffolding") | **~85%** (docs 02–12) |

**Strategic read:** Forge is the land, Guard is the expand. Forge is self-serve, cheap, and gets a builder inside the account in a day. Guard is the enterprise contract that follows once the estate exists. Governance alone is a hard first sale — nobody buys a control plane before they have anything to control. **This sequencing is the single most important thing to learn from them**, and our roadmap (doc 13) had it backwards: we planned to sell the enforcement spine first.

---

## 2. Full Capability Map (their public surface)

### 2.1 Platform-level pillars
| # | Pillar | Description | Our coverage |
|---|---|---|---|
| 1 | **Progressive Trust System** | Agents earn autonomy from observation → action; every op isolated, auditable, governed | ✅ Doc 09 (our autonomy ladder is more specified than theirs publicly) |
| 2 | **Adaptive System Generation** | NL intent → production-ready systems: applications, integrations, agents | ❌ **Major gap** → new doc 15 |
| 3 | **Continuous Improvement** | Agents learn from outcomes; institutional knowledge across deployments | 🟡 Doc 08 §1.4 + doc 10 §6 (we have the loop, not the "compounding org intelligence" framing) |
| 4 | **Collective Intelligence** | Surface org knowledge; connect data/workflows/expertise across teams for multi-agent execution | 🟡 Doc 08 (we have permission-aware RAG + light knowledge graph; not the cross-team coordination layer) |

### 2.2 Forge — the build product (6 stages)
| Stage | What it does | Our coverage |
|---|---|---|
| **01 Understand** | Ingests documents, process maps, data, meetings, approved system access; learns how the work happens; **finds missing decisions**; drafts a plain-language spec for human review before building | ❌ None |
| **02 Build** | Researches, designs, codes, connects, tests; **divides plans among specialist agents**; independently verifies results; live preview while building; keeps steps/versions/decisions/tests/evidence together | ❌ None |
| **03 Connect** | Connects to CRM, docs, data platforms, ticketing, comms; builds interface/decisions/orchestration around existing systems of record | 🟡 Doc 05 (we have the gateway; not the "build around" generation) |
| **04 Standardize** | **Workspace blueprints**: define capabilities, design, identity, access, testing, review, release standards once; every project inherits; **readiness model** makes verified controls + remaining work explicit | ❌ None — and this is a genuinely good idea |
| **05 Run** | Promote through preview → dev → production; environment-specific access; deployment history; monitor usage/errors/latency; rollback; Sycamore-managed / dedicated / BYOC | 🟡 Doc 12 (topologies yes, environment promotion for generated apps no) |
| **06 Improve** | Start each change from the running app and its decisions; controlled release path; **apps expose tools via MCP for other agents** | 🟡 Partial |

Also: **readiness scoring** across capabilities, configuration, tests, security, operations, release — with evidence per level and remaining work. That's a productized "is this ready for prod" rubric. We have eval gates (doc 10) but not a readiness score users can see and chase.

### 2.3 Guard — the govern product (6 stages)
| Stage | What it does | Our coverage |
|---|---|---|
| **01 Discover** | Inventories every AI use case, agent, application, model, connector, **MCP server**. Forge-built systems auto-register; external agents join via **telemetry, A2A, or direct registration**. Separates **declared intent from observed behavior** | ❌ We deferred this to P2 (FR-G7). **They lead with it.** |
| **02 Approve** | Conditional intake capturing purpose, data/tool access, models, decisions, human oversight; assigns risk; runs evaluations; routes reviews **without self-approval**; approved budgets/models/boundaries **become runtime policy**; material changes trigger re-review | 🟡 Doc 09 §6 has lifecycle governance; we lack the *intake form → risk tier → review routing* product |
| **03 Enforce** | Every agent gets identity; model/tool/data access routed through policy; approval gates; scoped controls; **revocation stops the agent across governed environments**; authority expands via evaluations and contracts on change | ✅ Docs 04, 05, 07, 09 — our deepest area |
| **04 Operate** | Live monitoring of requests, model/tool use, latency, tokens, spend; budgets, rate limits, routing; block or pause out-of-policy behavior | ✅ Docs 07, 10, 12 |
| **05 Prove** | Tamper-evident record of approvals, policy checks, access, changes, revocations; **replay**; **export as evidence pack**; **stream to SIEM** | ✅ Doc 10 (we have hash-chained ledger + replay + evidence export; SIEM streaming was implicit — make it explicit) |
| **06 Ask Guard** | NL query over the estate: who owns an agent, what changed, where spend is rising, which approvals are expiring — answered from **role-appropriate evidence** with links to records | ❌ None. Cheap to build on our Action Records, high demo value. |

### 2.4 Adoption model
"Do we have to adopt the whole platform at once?" → No. Start with registry and intake, runtime controls, cost management, or audit evidence — **each feature works independently while sharing the same identity, policy, and evidence model.** That modularity is a deliberate land strategy and a good architectural constraint to copy.

### 2.5 Labs — their stated research agenda (= their roadmap)
1. Multi-Agent Orchestration & Reliability — coordination protocols, fault tolerance, "growing more reliable the more they're used"
2. Agent Safety & Security — staying in bounds, resisting adversarial inputs, predictable in unfamiliar environments
3. Long-Horizon Memory & Context
4. Meta-Learning — agents acquiring skills without retraining
5. Progressive Trust & Governance
6. Adaptive System Generation

Our docs 06, 11, 08 line up with 1–3. We have nothing on 4 (meta-learning), and doc 15 addresses 6.

### 2.6 Pricing model (worth copying the shape)
- **Pro $50/mo**, **Max $200/mo** — flat, explicitly **not per-builder**, each including an equal dollar amount of usage credits. Credits cover models + compute for build, test, deploy, and run. Hosting draws from the same credits (first 3 months free). Credits don't roll over; hard monthly caps available.
- **Enterprise custom** — this is where Guard lives: registry + intake, runtime identity/policy, audit evidence, SSO/SAML/SCIM, dedicated or BYOC, custom review workflows.

**Read:** the flat-fee-plus-credits model neatly sidesteps the "seats don't fit agents" problem I flagged in doc 12 §6, and the $50 entry price is a credit-card decision. Our proposed "governed actions + sandbox hours" is more honest but harder to buy. **Recommend adopting credits-with-a-cap for the build product and action/estate-based pricing only for the governance product.**

---

## 3. Gap Analysis — what to add to our design

### P0 — architectural gaps (change the design)
| Gap | Why it matters | Where it goes |
|---|---|---|
| **Adaptive System Generation (Forge equivalent)** | It's the land motion and half the product | **New doc 15** |
| **AI estate discovery** (telemetry, gateway, A2A, direct registration; declared vs. observed behavior) | Enterprises have shadow agents *today*; this is the "aha" in the first demo. Also: the gateway we already built is the natural discovery sensor. | Promote from P2 → P0; new section in doc 09 |
| **Use-case intake & risk-tiered review workflow** | Turns our policy engine into something GRC can actually operate; "approve the use case, not just the tool" | Doc 09 §6 expansion |
| **Workspace blueprints** (standards inherited by every project) | Scales governance without central bottleneck; strong enterprise story | Doc 15 |
| **Readiness scoring** | Makes "production-ready" a measurable, chase-able number | Doc 15 + doc 10 |

### P1 — product-surface gaps (don't change architecture, add features)
| Gap | Effort | Note |
|---|---|---|
| **"Ask the estate" NL interface** | Low | Sits directly on Action Records + registry; excellent demo, low risk |
| **SIEM streaming + evidence pack export** | Low | Make explicit in doc 10; it's a procurement checkbox |
| **A2A protocol support** | Medium | Agent-to-agent interop standard; needed for discovering third-party agents |
| **Apps exposing tools via MCP outward** | Medium | Our doc 05 treats MCP as inbound only; bidirectional is the ecosystem play |
| **Environment promotion (preview → dev → prod) for generated apps** | Medium | Doc 12 covers deployment topologies, not app environments |
| **Meta-learning / skill acquisition** | High, research | Their Labs item 4; we have nothing. Probably fine to skip — it's a research bet, not a v1 feature |

### Where our design is genuinely *deeper* than their public surface
Not everything needs copying — several areas are ours to win on:
1. **Isolation depth.** They say "sandboxed" and "isolated." We specify two runtime tiers, microVM vs. pooled, selection rules, recycle policy (doc 03). No public evidence they go this deep.
2. **Delegation attenuation math.** Our monotonic narrowing invariant + delegation chains in every record (doc 04 §2.1) is more rigorous than "every agent gets an identity."
3. **Taint-based policy for injection defense** (docs 04 §4.2, 11 §3). Their Labs page states the goal ("resist adversarial inputs"); we specify a mechanism.
4. **Reversibility taxonomy driving saga compensation** (docs 05 §1.1, 06 §4). This is the sharpest idea in our set and I see no public equivalent.
5. **Deterministic replay for counterfactual upgrade diffing** (doc 10 §3.3). They say "replay"; we specify the journal semantics that make it real, and we're honest about its limits.
6. **Semantic collision detection / leases** (doc 06 §3). Their Labs item 1 says they're researching coordination; we have a design.

**Implication:** if we compete, compete on *specified mechanism vs. stated intent*. Publish the threat model and the runtime design. A CISO can read ours and check the math; they cannot check a marketing page.

---

## 4. What Their Structure Teaches About Ours

Three structural lessons worth adopting regardless of whether we build the whole thing:

1. **Split the product by buyer, not by layer.** Forge sells to the person with the problem; Guard sells to the person with the liability. Our doc set was organized by architecture layer, which is right for engineers and wrong for GTM. Any real plan needs both cuts.
2. **Modular adoption with a shared spine.** "Each feature works independently while sharing the same identity, policy, and evidence model" is exactly the right architectural constraint — it forces the Action Record / identity / policy core to be genuinely reusable, and it's how you sell into an org that won't rip-and-replace.
3. **Discovery before enforcement.** Inventory the estate first, enforce second. It's a lower-friction entry (read-only, no runtime dependency) and it generates the urgency that justifies the enforcement purchase.

---

## 5. Honest Strategic Assessment

They have $65M, a founder who was CTO of Atlassian, Fortune 100 pilots, researchers from Stanford/Cornell, and engineers from Meta/Google/Atlassian. Building both halves — a code-generation product *and* a governance control plane — is a 40+ engineer, multi-year effort.

**Direct head-on competition is not viable for a small team.** The realistic options from doc 13 §7 stand, now better informed:

- **Vertical wedge** — build Guard-only for one regulated vertical, with evidence exports their auditors already accept. Sycamore will chase horizontal Fortune 100 breadth; depth in financial services or healthcare is defensible.
- **Open core** — open-source the runtime + policy + Action Record spine. Their moat is a closed platform; ours would be the standard everyone else's agents get governed by. This is how challengers win infrastructure categories from behind, and their own modularity claim shows the spine is separable.
- **Reference architecture / portfolio** — the honest option. This doc set, plus a working Phase 0 prototype, is strong evidence of senior systems judgment across distributed systems, security, and applied AI. Sycamore's Labs page is essentially a list of the hardest problems in this space; having designed answers to four of six is itself the interview.

**What I'd do:** don't build Forge. It's the expensive half, it competes with every coding-agent company simultaneously, and it's where their $65M advantage compounds fastest. If you build anything, build the governance spine — narrow, deep, and open — for one vertical.
