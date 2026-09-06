# Doc 13 — Roadmap & Execution

---

## 1. The Sequencing Principle

The temptation is to build the impressive middle (orchestration, multi-agent, natural-language agent generation). The correct order is **the enforcement spine first**: identity → policy → gateway → audit. Everything else is replaceable; the spine is what customers cannot build themselves and cannot bolt on later.

A useful test for every roadmap item: *if we shipped without this, would a CISO still approve production use?* Items that fail this test are P0. Items that merely make developers happier are P1.

---

## 2. Phases

### Phase 0 — Foundations (months 0–3)
**Goal:** one agent runs end-to-end with real enforcement.

- Agent Registry (definitions, versions, digests) + `agent.yaml` schema
- Runtime Tier B only (pooled hardened sandbox) with the host ABI
- Tool Gateway with 3 first-party connectors + MCP mounting
- Credential Broker: agent-principal mode (OBO in Phase 1)
- PDP with a minimal policy language + PEPs at both gateways
- Action Records + hash-chained ledger
- Model Gateway with 2 providers, version pinning, budgets
- Python SDK + local emulator
- **Exit:** UC-1 (support deflection) running for a design partner at Trust Level 1, with a complete audit trail.

### Phase 1 — Trust & Durability (months 3–6)
- Durable execution + journal + suspend/resume
- Approval Service (routing, SLA, batching, Slack + mobile)
- Trust Engine with L0–L2 and the promotion workflow
- OBO token exchange for the top connectors
- Permission-aware retrieval (Memory Service v1)
- Tracing + eval suites + promotion gates
- Runtime Tier A (microVM) for write-capable agents
- **Exit:** UC-2 (financial close) running with dual control and an auditor-acceptable evidence export.

### Phase 2 — Scale & Multi-Agent (months 6–10)
- Supervisor + pipeline topologies, agent-to-agent auth with attenuation
- Leases, semantic collision detection, saga compensation
- Replay & counterfactual diffing
- L3 (sampling-based autonomy), drift detection, cost engine
- BYOC deployment
- TypeScript SDK, framework hosting (LangGraph/OpenAI Agents SDK)
- **Exit:** UC-4 (cross-functional workflow) in production at 2+ customers; 100+ agents under management at one customer.

### Phase 3 — Depth (months 10–18)
- Injection defense hardening + independent action verification
- Institutional learning loop (exemplar mining → versioned promotion)
- Compliance packs (SOX/HIPAA/EU AI Act evidence templates)
- Shadow-agent discovery
- Marketplace of governed agent templates and connectors
- Natural-language agent scaffolding (only now — it is worthless without the spine beneath it)

---

## 3. MVP Definition (what Phase 0 must prove)

A design partner can:
1. Register an agent with declared tools, scopes, and data classes.
2. Run it in production against real systems, with every action authorized and recorded.
3. See a complete trace and export an audit bundle.
4. Revoke it instantly.
5. Show the CISO evidence that the agent cannot exceed the invoking user's permissions.

**Not in MVP:** multi-agent, durable resume, microVM, trust levels above L1, replay, BYOC. Say no to all of it in Phase 0.

---

## 4. Build vs. Buy

| Component | Decision | Reasoning |
|---|---|---|
| Sandbox (Firecracker/gVisor) | **Buy/OSS** | Do not write a hypervisor |
| Policy engine (Cedar/OPA) | **Buy/OSS** | Mature, formally analyzable |
| Workload identity (SPIFFE/SPIRE) | **Buy/OSS** | Standard, integrates with customer infra |
| Durable execution core | **Build API, consider Temporal underneath** | Contract must be ours (ADR-2) |
| Vector store | **Integrate** | Customers have one; we enforce permissions ourselves |
| Tracing | **OTel + build agent semantics** | Interop is a selling point |
| Ledger | **Build** | Hash-chaining is simple; the guarantees are our differentiator |
| Credential Broker | **Build on Vault** | Custom logic, standard storage |
| Guardrail classifiers | **Buy first, build where weak** | Injection detection quality is a moving target |

---

## 5. Team Shape

Phase 0 minimum (~8–10 engineers):
- 2 runtime/systems (sandbox, scheduler, ABI)
- 2 platform/backend (registry, orchestrator, journal)
- 1 security engineer (identity, broker, threat model) — **hire first, not later**
- 1 policy/governance engineer
- 1 applied AI (guardrails, evals, retrieval quality)
- 1 SDK/DX
- 1 SRE
Plus: 1 PM, 1 design (the approval experience is a real design problem, not a form).

By Phase 2: ~25–30 engineering, adding connectors, frontend, and a dedicated red team.

---

## 6. Risks

| Risk | Likelihood | Impact | Mitigation |
|---|---|---|---|
| Hyperscaler bundles 80% of this free | High | High | Cloud-neutrality + BYOC + isolation depth; sell to CISO not to the AI team |
| Overhead tax makes us skippable | Medium | High | Benchmark as a release gate; publish the overhead number |
| Category consolidates before we get traction (Sycamore is well-funded and moving) | High | High | Pick a vertical beachhead (financial services or healthcare) rather than horizontal; win on evidence quality |
| Agent frameworks standardize away our runtime contract | Medium | Medium | Host foreign frameworks; contribute to standards rather than fight them |
| Complexity outruns the team | High | High | Ruthless phasing; Phase 0 has one runtime tier and three planes |
| Enterprise sales cycle (9–15 months) outlasts runway | High | Fatal | Design-partner model with paid pilots; land in one BU, not enterprise-wide |
| Prompt injection incident at a customer | Medium | High | Bounded blast radius by design; pre-written IR playbooks; honest pre-sale positioning so the incident is survivable |

---

## 7. Positioning Against Sycamore & Peers

Sycamore has capital, a proven enterprise founder, and a head start on the same thesis. Competing head-on horizontally is not viable for a small team. Two viable stances:

- **Vertical depth.** Pick financial services (SOX + dual control + irreversibility semantics are natively expressible in our model) and become the platform whose evidence exports auditors already accept. Depth in one vertical beats breadth for a challenger.
- **Open core.** Open-source the runtime + policy spine, monetize the control plane, governance packs, and support. Turns the category leader's closed platform into a lock-in argument in our favor, and it is how infrastructure categories are usually won from behind.

**A third, more honest option:** this design is also valuable as a *reference architecture and portfolio artifact* rather than a company. The systems reasoning here — isolation tiers, delegation attenuation, taint-based policy, saga compensation for agents, deterministic replay — is exactly the material senior infrastructure interviews probe, and it demonstrates range across distributed systems, security, and applied AI. Building the whole thing requires a funded team; building a credible **Phase 0 prototype** is a one-person, three-month project with real signal value either way.

---

## 8. Immediate Next Steps

1. **Pick the stance** (§7) before writing code — it changes what Phase 0 optimizes for.
2. **Prototype the spine:** sandbox + host ABI + PDP + one connector + Action Ledger. Two weeks to a demo where an agent is *denied* correctly and the denial is provable.
3. **Write the threat model as a public artifact** — in this category, the security document is the marketing.
4. **Find one design partner** with a real compliance requirement; their auditor's acceptance criteria are the actual product spec.
5. **Benchmark the overhead early.** If the spine costs more than 15%, the design needs rework before it needs features.
