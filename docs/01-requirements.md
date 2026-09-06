# Doc 01 — Requirements & Use Cases

---

## 1. Reference Use Cases (drive all requirements)

We design against four concrete workloads. Every architectural decision must be justifiable against at least one.

### UC-1 — Tier-1 Support Deflection (high volume, low risk)
An agent reads an incoming ticket, searches the knowledge base and past tickets, drafts a resolution, and either replies directly (if trust level allows) or queues for agent review. ~50k executions/day, p95 latency budget 20s, mostly read-only tools, one write (post reply).

**Stresses:** throughput, cost per execution, permission-aware retrieval, progressive trust.

### UC-2 — Financial Close Assistant (low volume, high risk)
An agent reconciles ledger entries across ERP and bank feeds, flags variances, drafts journal entries. ~200 executions/month. Writes to a system of record. SOX-relevant.

**Stresses:** audit evidence, segregation of duties, approval workflow, deterministic replay, irreversible-action handling.

### UC-3 — Engineering Agent Fleet (long-running, code-executing)
Agents triage bugs, write patches, open PRs, run tests in a sandbox. Sessions run 5–90 minutes, execute arbitrary code, clone repos, hit package registries.

**Stresses:** strong isolation, egress control, supply-chain risk, long-running session state, cost of idle compute.

### UC-4 — Cross-Functional Workflow (multi-agent coordination)
A "new enterprise customer onboarding" workflow spans a contract agent (legal doc review), a provisioning agent (IT systems), and a finance agent (billing setup), coordinated with dependencies and shared state.

**Stresses:** orchestration semantics, agent-to-agent auth, collision avoidance, saga/compensation, partial failure.

---

## 2. Functional Requirements

### 2.1 Build & Author (FR-B)
| ID | Requirement | Priority |
|---|---|---|
| FR-B1 | Define an agent declaratively (`agent.yaml`: identity, model policy, tools, memory, guardrails, autonomy ceiling) | P0 |
| FR-B2 | SDK in Python + TypeScript to implement agent logic; framework-agnostic host interface | P0 |
| FR-B3 | Local emulator running the same runtime contract as production (tools mocked or proxied through the real gateway with dev credentials) | P0 |
| FR-B4 | Import/host agents built with LangGraph, OpenAI Agents SDK, CrewAI without rewrite | P1 |
| FR-B5 | Natural-language agent scaffolding ("describe intent → generated agent skeleton + tool bindings") | P2 |
| FR-B6 | Versioned agent artifacts with immutable digests; prompts are versioned assets, not code comments | P0 |

### 2.2 Deploy & Run (FR-R)
| ID | Requirement | Priority |
|---|---|---|
| FR-R1 | Execute agent sessions in isolated sandboxes with enforced CPU/mem/time/token budgets | P0 |
| FR-R2 | Support sync (request/response), async (job), scheduled, and event-triggered invocation | P0 |
| FR-R3 | Durable sessions: survive process/node failure, resume from last committed step | P0 |
| FR-R4 | Long-running sessions up to 24h with suspend/resume (no billing for idle wall-clock) | P1 |
| FR-R5 | Blue/green + canary rollout of agent versions with automatic rollback on eval/SLO regression | P1 |
| FR-R6 | Kill switch: terminate a single session, an agent version, or all agents of a tenant within 5s | P0 |

### 2.3 Govern (FR-G)
| ID | Requirement | Priority |
|---|---|---|
| FR-G1 | Every tool call, model call, and memory write is authorized by policy before execution | P0 |
| FR-G2 | Policy expressed as code, versioned, testable, with dry-run ("what would this policy have blocked last week?") | P0 |
| FR-G3 | Progressive autonomy: agents advance/regress through trust levels based on measured signals | P0 |
| FR-G4 | Human-in-the-loop approval with routing, SLA, escalation, and mobile-capable approval surface | P0 |
| FR-G5 | Immutable, tamper-evident audit trail sufficient for SOX/HIPAA/EU AI Act evidence | P0 |
| FR-G6 | Data residency and sovereignty constraints enforceable per tenant/agent/data class | P1 |
| FR-G7 | Agent discovery: detect unregistered/shadow agents calling enterprise APIs | P2 |

### 2.4 Observe & Improve (FR-O)
| ID | Requirement | Priority |
|---|---|---|
| FR-O1 | Full trace of every session: prompts, model versions, retrieved context, tool I/O, policy decisions | P0 |
| FR-O2 | Deterministic replay of any past session against a new agent version | P1 |
| FR-O3 | Offline eval suites + online quality signals; gate promotions on both | P0 |
| FR-O4 | Cost attribution per session/agent/team/business outcome | P0 |
| FR-O5 | Drift detection on model behavior, retrieval quality, and tool error rates | P1 |
| FR-O6 | Feedback loop: approved/edited/rejected human decisions become eval data and few-shot exemplars | P1 |

---

## 3. Non-Functional Requirements

| Dimension | Target | Notes |
|---|---|---|
| **Availability** | 99.9% control plane, 99.95% data plane (single region); 99.99% with multi-region | Control-plane outage must NOT stop running sessions (cached policy bundles, fail-safe mode) |
| **Latency** | Policy decision p99 < 10ms; sandbox cold start p95 < 250ms; warm start < 30ms; gateway overhead on model call p99 < 40ms | Overhead budget total < 5% of end-to-end agent latency |
| **Throughput** | 10k concurrent sessions/region at GA; 100k design ceiling | |
| **Durability** | Zero loss of committed session steps and action records; audit ledger 7-year retention | |
| **Isolation** | No cross-tenant data path; kernel-level isolation for code-executing sessions | Verified by continuous adversarial testing |
| **Scalability** | Linear scale-out of runtime pool; control plane sharded by tenant | |
| **Recovery** | RPO 0 for audit/action records, RPO 5min for session state; RTO 30min region failover | |
| **Cost** | Platform overhead < 15% of underlying model+compute spend at scale | The pitch dies if we double the cost of running agents |
| **Portability** | Deployable to AWS/GCP/Azure and customer VPC; no managed-service lock-in on critical path | |

---

## 4. Key User Journeys

### J1 — Ana ships her first agent (target: < 1 day)
1. `helmdeep init` scaffolds `agent.yaml` + handler.
2. She declares tools from the **Tool Registry** (`salesforce.read_account`, `kb.search`) — the registry shows required scopes and data classes.
3. `helmdeep dev` runs locally against the emulator; tool calls proxy through the real gateway with her own delegated identity, so she sees actual permission errors early.
4. `helmdeep deploy --env staging` produces an immutable version, runs the eval suite, publishes a scorecard.
5. Trust Engine assigns **Level 0 (Observe)**: agent runs but all writes are simulated and diffed.
6. After 200 sessions with ≥95% human agreement, it is eligible for **Level 1**; Marcus (business owner) approves promotion in one click.

### J2 — Sofia investigates an incident
1. Alert: "Agent `invoice-recon` attempted access to a data class it has never touched."
2. She opens the session trace, sees the exact retrieved document containing injected instructions, sees the policy decision that blocked the tool call.
3. She clicks "contain": revokes the agent's brokered credentials, freezes the version, and queues all in-flight sessions for review — one action, propagated in < 5s.
4. She exports a signed evidence bundle for the auditor.

### J3 — Marcus grants more autonomy
1. Weekly digest: `support-triage` handled 4,200 tickets; humans approved 97.4% unchanged, edited 2.1%, rejected 0.5%; zero policy violations; cost $0.11/ticket vs. $6.40 human baseline.
2. He promotes reply-posting from "approve each" to "auto-post for confidence ≥ threshold, sample 5% for audit."
3. Trust Engine records the change with the evidence that justified it — this is the artifact an auditor asks for.

---

## 5. Success Metrics

**Platform health**
- Time-to-first-production-agent (target < 5 days for a new team)
- % of enterprise agents registered on-platform (shadow-agent coverage; target > 90% by year 2)
- Platform overhead ratio (target < 15%)

**Trust outcomes**
- % of agent actions covered by an explicit policy decision (target 100%)
- Autonomy progression rate: median days from Level 0 → Level 2
- Escaped-incident rate per 100k actions (target < 1, trending down)
- Human approval load per 1k actions (must fall as trust rises — this proves the model works)

**Business**
- Cost per completed business outcome vs. human baseline
- Agent task success rate (measured by outcome, not by "did it respond")

---

## 6. Scope Boundaries

**In scope v1:** runtime, identity/authz, tool gateway, orchestration, policy/trust engine, observability/audit, model gateway, SDK.

**Out of scope v1:** natural-language agent generation (FR-B5), shadow-agent discovery (FR-G7), fine-tuning pipeline, our own vector DB, on-prem air-gapped deployment, non-English guardrail packs beyond top 5 languages.

**Deliberately never:** we do not train on customer data; we do not persist model I/O outside tenant-controlled storage; we do not offer "just disable the policy engine" as a supported configuration.
