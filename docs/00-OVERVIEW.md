# AgentOS — Trusted Agent Runtime Platform for the Enterprise
## Document 00 — Overview & Master Index

**Status:** Design draft v1
**Author:** Huy (Overarching AI)
**Date:** September 2026

---

## 1. Problem Statement

Foundation models can now reason and take actions. Enterprises cannot deploy them safely at scale. The gap is not model capability — it is **infrastructure for trust**.

Concretely, when an enterprise moves from "one chatbot" to "300 agents acting across Salesforce, Workday, GitHub, and internal databases," six problems appear simultaneously:

1. **Identity crisis.** An agent acting "on behalf of" a user is neither the user nor a service account. Existing IAM has no primitive for it. Audit logs say "svc-agent-prod did it," which is useless for compliance.
2. **Blast radius.** A single prompt injection in a fetched web page can turn a helpful agent into an exfiltration tool with the user's full OAuth scope.
3. **Non-determinism vs. change management.** Enterprises gate software changes with tests and approvals. An agent's behavior changes when the model provider ships a new checkpoint, when a prompt is edited, or when retrieved context shifts.
4. **Coordination chaos.** Multiple agents operating on shared state (the same ticket, the same customer record, the same repo) collide, duplicate work, and cascade failures.
5. **No graduated autonomy.** Today it is binary: either a human approves every action (no value) or the agent is fully autonomous (unacceptable risk).
6. **Zero observability.** When an agent produces a wrong outcome, teams cannot answer: which model version, which context, which tool call, which policy allowed it, and what would have happened otherwise.

**AgentOS is the runtime and control plane that solves these six.** It is not an agent framework and not a model provider. It is the layer between them and the enterprise.

---

## 2. Vision & Positioning

> **A trusted, secured, and governed runtime environment where enterprises build, deploy, and orchestrate AI agents at scale — where every agent action is isolated, authorized, observable, and reversible, and where agents earn autonomy through demonstrated reliability.**

### 2.1 Design philosophy — five principles

| # | Principle | Implication |
|---|---|---|
| P1 | **Agents are workloads, not users.** | Every agent gets a first-class cryptographic identity, a lifecycle, an owner, and a resource budget. |
| P2 | **Authority is delegated, scoped, and expiring — never ambient.** | No agent ever holds a long-lived credential. All access is brokered, just-in-time, and narrowed to the task. |
| P3 | **Trust is earned, not configured.** | Autonomy levels advance based on measured outcomes (approval rates, eval scores, incident-free actions), not a checkbox. |
| P4 | **Every action is reversible or approvable.** | Actions are classified by reversibility; irreversible ones require compensation handlers or human approval. |
| P5 | **The untrusted boundary is the model output, not the user.** | Anything the model emits — and anything it reads — is untrusted input to the policy layer. |

### 2.2 Explicit non-goals

- We do not build foundation models.
- We do not replace LangGraph / CrewAI / OpenAI Agents SDK — we **host** them. Framework-agnostic by design.
- We are not an RPA product; we do not do screen scraping of legacy UIs (v1).
- We are not a data platform; we integrate with existing warehouses and vector stores.

### 2.3 Competitive context

The category is forming fast: Sycamore ($65M seed, Mar 2026, ex-Atlassian CTO) is the highest-profile "trusted agent OS" entrant; LangSmith and AgentOps own observability; hyperscalers (Bedrock AgentCore, Azure AI Foundry, Vertex Agent Engine) are bundling runtime with their clouds; a wave of agent-governance startups (Singulr, AvePoint AgentPulse, Geordie) attack discovery and runtime policy.

**Our defensible wedge (choose one, do not straddle):**
- **Wedge A — Governance-first, cloud-neutral.** Enterprises with multi-cloud + regulated workloads (finance, health, public sector) that cannot accept a hyperscaler-locked runtime. Sell to CISO + Head of Platform, not to the AI team.
- **Wedge B — Runtime isolation depth.** Be the only platform with true microVM-per-session isolation, deterministic replay, and cryptographic action provenance — a technical moat the "workflow builder" players cannot retrofit.

This design pursues **A + B together**: cloud-neutral control plane, deep runtime isolation as the technical differentiator.

---

## 3. Personas

| Persona | Role | What they need from AgentOS | Primary KPI |
|---|---|---|---|
| **Ana — Agent Developer** | App/ML engineer in a business unit | SDK, local emulator, fast deploy, traces, evals | Time-to-first-production-agent |
| **Raj — Platform Engineer** | Central platform team | Multi-tenancy, quotas, capacity, upgrades, on-call | Platform SLO, cost/agent-hour |
| **Sofia — Security/GRC Lead** | CISO org | Policy authoring, audit evidence, data residency, incident response | Auditable action coverage, mean-time-to-contain |
| **Marcus — Business Owner** | Ops director who "employs" agents | Approve/reject queue, outcome dashboards, cost per outcome | Task success rate, hours saved |
| **Dana — Data Owner** | Owns a system of record | Guarantee agents only touch permitted rows | Zero unauthorized access events |

---

## 4. Architecture at a Glance

```
┌──────────────────────────────────────────────────────────────────────┐
│                            CONTROL PLANE                             │
│  Agent Registry │ Policy Service │ Trust Engine │ Approval Service    │
│  Model Catalog  │ Tool Registry  │ Eval Service │ Audit Ledger        │
└───────────────────────────────┬──────────────────────────────────────┘
                                │ (config, policy bundles, decisions)
┌───────────────────────────────▼──────────────────────────────────────┐
│                             DATA PLANE                               │
│                                                                      │
│   ┌────────────┐   ┌──────────────┐   ┌──────────────────────────┐   │
│   │Orchestrator│──▶│ Agent Runtime│──▶│ Tool Gateway (egress)     │──▶ SaaS/DB/APIs
│   │ (durable)  │   │  (microVM)   │   │  + Credential Broker      │   │
│   └─────┬──────┘   └──────┬───────┘   └──────────────────────────┘   │
│         │                 │                                          │
│         │                 ▼                                          │
│         │          ┌──────────────┐   ┌──────────────────────────┐   │
│         └─────────▶│Model Gateway │   │ Memory & Knowledge Svc    │   │
│                    └──────────────┘   └──────────────────────────┘   │
│                                                                      │
│   Cross-cutting: Policy Enforcement Points (PEP) at every arrow      │
└──────────────────────────┬───────────────────────────────────────────┘
                           │ (traces, events, action records)
                    ┌──────▼──────────────────────┐
                    │ Observability & Audit Plane │
                    └─────────────────────────────┘
```

**The single most important architectural idea:** every arrow crossing a component boundary passes through a **Policy Enforcement Point** that evaluates `(agent identity, action, resource, context, trust level)` before the call proceeds, and emits an immutable **Action Record** afterward. This is what makes the system governable rather than merely observable.

---

## 5. Document Index

| Doc | Title | What it covers |
|---|---|---|
| **00** | Overview & Master Index | This document |
| **01** | [Requirements & Use Cases](01-requirements.md) | Functional/non-functional requirements, journeys, success metrics, scope boundaries |
| **02** | [System Architecture](02-architecture.md) | Component catalog, control/data plane split, core flows, key design decisions & tradeoffs |
| **03** | [Agent Runtime & Isolation](03-agent-runtime.md) | Execution model, microVM sandboxing, session lifecycle, checkpoint/replay, resource governance |
| **04** | [Identity & Authorization](04-identity-authz.md) | Agent identity, delegation chains, credential broker, scoped tokens, RBAC/ABAC/ReBAC |
| **05** | [Tool Gateway & Connectors](05-tool-gateway.md) | Tool registry, MCP integration, egress control, data classification, tool-level policy |
| **06** | [Orchestration Engine](06-orchestration.md) | Durable execution, multi-agent topologies, state & concurrency, compensation/rollback |
| **07** | [Model Gateway](07-model-gateway.md) | Routing, versioning, caching, quotas, fallback, redaction, cost control |
| **08** | [Memory & Knowledge](08-memory-knowledge.md) | Working/episodic/semantic memory, permission-aware RAG, institutional learning loop |
| **09** | [Governance & Progressive Trust](09-governance-trust.md) | Policy model, autonomy ladder, HITL approvals, change management, compliance mapping |
| **10** | [Observability & Evaluation](10-observability-eval.md) | Agent-native tracing, audit ledger, evals, replay/counterfactuals, cost attribution |
| **11** | [Security & Threat Model](11-security-threat-model.md) | Threat catalog, prompt injection defense, exfiltration control, supply chain, IR playbooks |
| **12** | [Platform Operations](12-platform-ops.md) | Multi-tenancy, deployment topologies, scaling, SLOs, DR, capacity & unit-cost model |
| **13** | [Roadmap & Execution](13-roadmap.md) | Phasing, MVP definition, build-vs-buy, team shape, risks, open questions |
| **14** | [Competitive Teardown: Sycamore](14-competitive-teardown-sycamore.md) | Full public capability map (Forge + Guard), gap analysis vs. this design, where we're deeper, strategic read |
| **15** | [Generation, Discovery & Intake](15-generation-discovery-intake.md) | The missing half: adaptive system generation, AI estate discovery, use-case intake & risk review |

---

## 6. How to Read This Set

- **Executives / investors:** 00 → 01 (§ metrics) → 13.
- **Security reviewers:** 00 → 04 → 09 → 11.
- **Engineers implementing:** 02 → 03 → 06 → 05 → then their component doc.
- **For interview / portfolio use:** 02, 03, 06, and 11 contain the deepest systems reasoning and explicit tradeoff analysis.

---

## 7. Top-Level Open Questions

These are decided in the individual docs but flagged here because they shape everything:

1. **Isolation depth vs. cold-start.** microVM (Firecracker) gives real isolation but 100–200ms cold start and higher memory floor. gVisor is cheaper but weaker. **Decision (doc 03):** microVM for tool-executing/code-executing sessions, shared pooled containers for read-only reasoning sessions.
2. **Do we own orchestration or host others'?** **Decision (doc 06):** own a durable execution substrate; support foreign frameworks as guest processes inside it.
3. **Single-tenant vs. multi-tenant data plane.** **Decision (doc 12):** multi-tenant control plane; per-tenant data plane available as BYOC for regulated buyers — this is a sales requirement, not just a technical one.
4. **How much do we depend on a specific model vendor?** **Decision (doc 07):** hard abstraction; no vendor-specific features in the agent contract.
