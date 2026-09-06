# Doc 02 — System Architecture

---

## 1. Architectural Model

AgentOS separates into **four planes**. This split is the backbone of the design: it determines failure isolation, deployment topology, and what a regulated customer can keep inside their own network.

| Plane | Owns | Failure semantics |
|---|---|---|
| **Control Plane** | Configuration, policy authoring, registries, trust state, approvals, eval orchestration | Outage must not stop running sessions. Data plane runs on cached, signed policy bundles. |
| **Data Plane** | Session execution, model calls, tool calls, memory access | Outage stops new work; durable state means no loss. |
| **Governance Plane** | Real-time policy decisions (PDP), credential brokering, approval gates | Fail-closed for write/irreversible actions; fail-safe (read-only degraded) for reads. |
| **Observability Plane** | Traces, metrics, action records, immutable audit ledger | Best-effort for metrics; **guaranteed durable** for action records (blocking write before irreversible actions). |

### 1.1 The Action Record — the system's central abstraction

Everything the platform does is modeled as an **Action**: a request by an agent identity to perform an operation on a resource. An Action Record is written for every one:

```jsonc
{
  "action_id": "act_01J...",           // ULID, monotonic
  "session_id": "ses_01J...",
  "trace_id": "...",                    // links to full reasoning trace
  "actor": {
    "agent_id": "ag_invoice_recon",
    "agent_version": "sha256:...",
    "delegation_chain": ["user:marcus@corp", "agent:workflow_onboarding", "agent:invoice_recon"],
    "trust_level": 2
  },
  "operation": {
    "type": "tool_call",                // tool_call | model_call | memory_write | agent_spawn
    "name": "netsuite.create_journal_entry",
    "reversibility": "compensable",     // reversible | compensable | irreversible
    "data_classes_touched": ["financial.ledger"],
    "arguments_digest": "sha256:...",   // full args stored separately w/ retention policy
    "egress_targets": ["netsuite.com"]
  },
  "decision": {
    "outcome": "allow_with_approval",
    "policy_bundle_version": "pb_2026_09_01_r3",
    "matched_rules": ["fin.je.require_dual_control"],
    "approver": "user:sofia@corp",
    "latency_ms": 6
  },
  "result": { "status": "success", "compensation_handle": "cmp_..." },
  "timestamp": "2026-09-06T10:14:22.113Z",
  "prev_hash": "sha256:...",            // hash chain for tamper evidence
  "signature": "..."                    // signed by runtime enclave key
}
```

**Why this matters:** compliance, replay, cost attribution, trust scoring, and incident containment are all queries over this one stream. Designing it first prevents the classic failure of bolting audit on later and discovering you cannot answer "who authorized this."

---

## 2. Component Catalog

### Control Plane
| Component | Responsibility | Store |
|---|---|---|
| **Agent Registry** | Agent definitions, versions, owners, dependencies, autonomy ceiling | Postgres + object store for artifacts |
| **Tool Registry** | Tool/connector catalog, schemas, required scopes, data classes, risk rating | Postgres |
| **Model Catalog** | Approved models, versions, pinning rules, per-model policy (PII allowed? residency?) | Postgres |
| **Policy Service** | Authoring, compilation, testing, distribution of signed policy bundles | Postgres + OCI registry for bundles |
| **Trust Engine** | Computes trust level per (agent, action-class) from evidence; promotion/demotion | Postgres + feature store |
| **Approval Service** | Approval requests, routing, SLA, escalation, delegation of approval authority | Postgres + queue |
| **Eval Service** | Eval suites, offline runs, scorecards, promotion gates | Postgres + object store |
| **Tenant/Org Service** | Tenancy, RBAC, quotas, billing entitlements | Postgres |

### Data Plane
| Component | Responsibility | Notes |
|---|---|---|
| **Session Orchestrator** | Durable execution of agent sessions & multi-agent workflows | Doc 06 |
| **Agent Runtime (Sandbox)** | Isolated execution of agent code + model loop | Doc 03 |
| **Model Gateway** | Routing, quotas, caching, redaction, fallback | Doc 07 |
| **Tool Gateway** | Tool invocation, egress control, response filtering | Doc 05 |
| **Credential Broker** | Mints just-in-time, narrowly scoped credentials | Doc 04 |
| **Memory Service** | Working/episodic/semantic memory, permission-aware retrieval | Doc 08 |

### Governance Plane
| Component | Responsibility |
|---|---|
| **PDP (Policy Decision Point)** | Evaluates policy; sidecar-cached for <10ms p99 |
| **PEPs (Enforcement Points)** | Embedded in Tool Gateway, Model Gateway, Memory Service, Orchestrator |
| **Guardrail Service** | Content classification, injection detection, PII detection, output validation |
| **Approval Gate** | Blocks execution pending human decision, with timeout policy |

### Observability Plane
**Trace Collector** (OTel-compatible, agent-semantic spans) · **Action Ledger** (append-only, hash-chained, WORM-backed) · **Metrics/Cost Engine** · **Replay Service**.

---

## 3. Core Flows

### 3.1 Session execution (the hot path)

```
Trigger (API/event/schedule)
  → Orchestrator: create session, load agent version + policy bundle, resolve principal & delegation chain
  → Admission control: quota, budget, trust ceiling, kill-switch check      [PEP-0]
  → Runtime: allocate sandbox (warm pool → microVM), inject session token (no raw creds)
  → LOOP:
      Agent code decides next step
      ├─ model_call  → Model Gateway [PEP-1: model allowed? PII redaction? budget?] → provider
      ├─ memory_read → Memory Svc     [PEP-2: permission-aware filter on retrieval]
      ├─ tool_call   → Tool Gateway   [PEP-3: policy → maybe Approval Gate → Credential Broker → egress]
      └─ spawn_agent → Orchestrator   [PEP-4: delegation depth, scope attenuation]
      Each step: durable checkpoint + Action Record write
  → Terminate: flush records, release credentials, snapshot memory, emit outcome event
  → Post-hoc: eval sampling, trust signal update, cost rollup
```

**Critical ordering rule:** for `irreversible` operations, the Action Record and approval decision are **durably committed before** the side effect is issued. For `reversible` operations, the record write may be async to protect latency. This is the throughput/compliance tradeoff, made explicit and per-operation rather than globally.

### 3.2 Policy decision path

PDP runs as a **sidecar next to each PEP** with a locally cached, signed policy bundle (OPA/Cedar-style). Bundle distribution is pub/sub with version pinning per session — meaning a policy change mid-session does not change the rules under a running agent unless the change is marked `emergency: true`, which force-invalidates.

Decision inputs: `principal` (delegation chain), `agent identity + version`, `operation`, `resource + data classes`, `context` (time, geo, session risk score, injection-detector verdict), `trust level`. Outputs: `allow | deny | allow_with_approval | allow_with_constraints` (e.g. row limits, redaction, rate cap) — the fourth outcome is what makes graduated autonomy expressible.

### 3.3 Trust progression loop
```
Action Records + human approval outcomes + eval scores + incident flags
  → Trust Engine (windowed aggregation per agent × action-class)
  → Proposed level change + evidence bundle
  → Owner approval (or auto-promote if policy allows)
  → New autonomy ceiling written to Agent Registry → new policy bundle → data plane
```
Demotion is **automatic and immediate** on incident; promotion is deliberate. Asymmetry by design.

---

## 4. Key Design Decisions & Tradeoffs

### ADR-1 — Runtime isolation: hybrid microVM + pooled container
**Decision:** Firecracker-class microVM for sessions that execute code or call `write`/`irreversible` tools; pooled gVisor-hardened containers for read-only reasoning sessions.
**Rejected:** all-microVM (cost: ~2–4× compute, cold start hurts UC-1 at 50k/day); all-container (unacceptable for UC-3 arbitrary code).
**Tradeoff accepted:** two runtime paths = more complexity in the scheduler and two security postures to maintain. Mitigated by a single sandbox interface contract (doc 03 §4).

### ADR-2 — Own the orchestration substrate; host foreign frameworks
**Decision:** implement durable execution ourselves (event-sourced, deterministic replay) rather than mandating Temporal, and let LangGraph/CrewAI agents run *inside* our sessions as guest code.
**Rationale:** durable state + Action Records + policy interception must be at the same layer; delegating to a general workflow engine makes agent-semantic replay and per-step policy awkward.
**Tradeoff:** significant build cost (est. 3–4 engineer-quarters for a solid v1). Revisit: Temporal as an implementation detail *underneath* our API is acceptable if it accelerates GA — the API contract is what we must own.

### ADR-3 — Policy at the gateway, not in the agent
**Decision:** agents cannot self-attest. All enforcement is outside the sandbox, at gateways the agent cannot bypass (network egress is default-deny; only the gateway is reachable).
**Rationale:** anything inside the sandbox is compromised the moment prompt injection succeeds.
**Consequence:** the sandbox has *no* direct internet, no ambient cloud credentials, no DNS except to the gateway. This is non-negotiable and shapes doc 03 and 05.

### ADR-4 — Cloud-neutral control plane, pluggable data plane
**Decision:** control plane runs on Kubernetes with Postgres + object store + Kafka-compatible bus; no proprietary managed services on the critical path. Data plane can be deployed into a customer VPC ("BYOC") reporting to our control plane.
**Tradeoff:** we give up hyperscaler-managed conveniences and take on more ops burden; in exchange we win the regulated multi-cloud buyer, which is our wedge.

### ADR-5 — Model-vendor abstraction is strict
**Decision:** the agent contract exposes a normalized interface (messages, tools, structured output, streaming, caching hints). Vendor-specific features are opt-in behind capability flags and never required.
**Rationale:** the buyer's fear of lock-in is our sales advantage; also lets us fail over during provider incidents.
**Tradeoff:** we lag on brand-new vendor features by weeks. Acceptable.

### ADR-6 — Memory is a service with permission-aware retrieval, not a library
**Decision:** all long-term memory and RAG goes through Memory Service which enforces document-level ACL at query time, using the *end user's* effective permissions, not the agent's.
**Rationale:** the single most common enterprise RAG breach is an index that flattens permissions.
**Tradeoff:** retrieval latency +10–30ms and much harder index design (doc 08).

---

## 5. Technology Choices (opinionated starting point)

| Layer | Choice | Why |
|---|---|---|
| Sandbox | Firecracker + Kata-style shim; gVisor for pooled tier | Proven microVM isolation, ~125ms boot floor |
| Orchestrator | Go, event-sourced, Postgres + Kafka | Determinism, operational maturity |
| Policy | Cedar (or OPA/Rego) compiled to signed bundles | Formal-ish semantics, fast local eval, testability |
| Control plane API | gRPC + REST gateway, Protobuf contracts | Multi-language SDKs |
| Data stores | Postgres (config/state), Kafka (events), S3-compatible (artifacts/traces), ClickHouse (analytics), WORM bucket + hash chain (audit) | |
| SDK | Python + TypeScript | Where agent developers live |
| Identity | SPIFFE/SPIRE for workload identity; OIDC for humans; token exchange (RFC 8693) for delegation | Standards over invention |
| Observability | OpenTelemetry with agent-semantic conventions | Interop with customer stacks |

---

## 6. What Could Kill This Architecture

1. **Overhead tax.** If the gateway + policy + audit path adds >15% latency/cost, buyers route around us. Mitigation: aggressive local caching, async records for reversible ops, benchmark as a release gate.
2. **Framework churn.** If agent frameworks converge on a standard runtime contract we don't match, we become a bad host. Mitigation: track MCP and emerging agent-runtime standards; contribute rather than fork.
3. **Hyperscaler bundling.** AWS/Azure give away 80% of this with their model service. Mitigation: cloud-neutrality + isolation depth + governance evidence quality — the 20% regulated buyers pay for.
4. **Complexity collapse.** Four planes, two runtime tiers, and a trust engine is a lot for a small team. Mitigation: phase ruthlessly (doc 13); v1 ships three planes and one runtime tier.
