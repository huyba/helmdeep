# Doc 06 — Orchestration Engine

**Owns:** durable execution of sessions and multi-agent workflows; coordination, concurrency control, and failure/compensation semantics.

The central claim: *agent orchestration is distributed systems work, not prompt work.* When multiple agents act on shared enterprise state, the failure modes are the classic ones — lost updates, partial failure, duplicate side effects, deadlock — plus a new one: **semantic collision**, where two agents each do something individually correct that is jointly wrong.

---

## 1. Execution Substrate

### 1.1 Event-sourced durable execution
Every session is a deterministic state machine driven by an append-only journal.

```
Journal entry := { seq, type, payload, timestamp, nondeterminism_record? }
types: SESSION_START | STEP_BEGIN | ACTION_INTENT | POLICY_DECISION | ACTION_RESULT
     | HUMAN_RESPONSE | CHECKPOINT | SUSPEND | RESUME | SESSION_END | COMPENSATION
```

Properties:
- **Exactly-once side effects** in practice, via idempotency keys + outcome cache (doc 05 §6), even though delivery is at-least-once.
- **Deterministic replay** — replaying the journal reconstructs identical in-memory state (given ABI-mediated nondeterminism, doc 03 §1.2).
- **Time-travel debugging & counterfactuals** — replay with a modified agent version to see what *would* have happened (doc 10).

Implementation: Go service, journal in Postgres (partitioned by tenant/time) with hot tail in Redis, archived to object storage. Leases + fencing tokens for single-writer guarantees per session.

### 1.2 Why not just use a workflow engine?
Temporal/Restate solve durability well. We still own the layer because we need per-step policy interception, agent-semantic journal entries (prompts, retrievals, decisions), and replay that eval tooling can consume. **Pragmatic path:** define our API and journal semantics first; implementing the durability core on Temporal internally is an acceptable accelerator as long as the contract stays ours (ADR-2).

---

## 2. Multi-Agent Topologies

We support four, deliberately limited — each additional topology multiplies failure modes.

| Topology | Shape | Use | Coordination cost |
|---|---|---|---|
| **Single** | one agent, loop | UC-1 | none |
| **Supervisor → workers** | one planner delegates to specialists, aggregates | UC-4 | moderate; parent owns state |
| **Pipeline** | fixed DAG of stages, typed handoffs | document processing, ETL-ish | low; static analysis possible |
| **Blackboard (bounded)** | agents read/write a shared, versioned workspace with explicit locks | research/investigation tasks | high; requires §3 machinery |

**Not supported:** free-form agent-to-agent chatter with emergent topology. It is unauditable, unbounded in cost, and its failure modes are unbounded. If a customer needs it, they build it as a supervisor pattern with explicit message contracts.

### 2.1 Agent-to-agent invocation
`host.agent.spawn(agent, input, scope_attenuation)` creates a **child session** with:
- attenuated identity (doc 04 §2.1), depth counter incremented,
- its own budget carved from the parent's remaining budget (budgets are hierarchical — a runaway child cannot exceed the parent's allocation),
- a typed contract: input/output schemas declared on the agent definition, validated both ways,
- lifecycle coupling: parent cancellation cancels children; child failure surfaces as a structured result, not an exception.

---

## 3. Shared State & Collision Control

This is where multi-agent systems actually break in production.

### 3.1 Resource leases
Before acting on a shared business entity, an agent acquires a **semantic lease**:

```
lease = acquire(resource="crm:account:A-4471", intent="update_contract_terms",
                mode=exclusive|shared, ttl=5m, holder=agent_instance)
```

- Leases are on *business entities*, not rows — the unit that matters for collision.
- Conflicting intents on the same entity are serialized; the second agent receives a structured `Contended` observation and can wait, re-plan, or defer. (Making contention *visible to the agent as data* is far better than blocking silently.)
- TTL + heartbeat, auto-release on session end; fencing tokens prevent a resumed zombie session from acting on a stale lease.

### 3.2 Semantic collision detection
Beyond locks: an **intent registry** records the declared intent of active sessions. Before an irreversible action, the orchestrator checks for conflicting concurrent intents (e.g. two agents independently issuing refunds for the same order). On conflict: escalate to human, or defer per policy. This catches the case where each agent holds a *different* lock but the combined outcome is wrong.

### 3.3 Idempotent workflow keys
Externally triggered workflows carry a business idempotency key (e.g. `ticket_id`), so a duplicate webhook does not spawn a second workflow — a mundane but extremely common source of duplicate agent actions.

---

## 4. Failure & Compensation (Saga Semantics)

Agents cannot use distributed transactions across SaaS systems, so we use sagas with explicit compensation.

```
Workflow step i: forward action A_i  (records compensation_handle C_i if compensable)
On failure at step k:
   for i = k-1 downto 1: execute C_i   (best-effort, recorded, retried with backoff)
   irreversible steps have NO C_i → workflow enters PARTIAL_FAILURE
                                   → human remediation task created with full context
```

**Design rules:**
1. **Order irreversible actions last.** The orchestrator statically analyzes a workflow definition and *warns at registration* if an irreversible step precedes reversible ones — a linting rule that prevents entire classes of production incidents.
2. **Compensation is itself an action** — policy-checked, recorded, and possibly requiring approval. It is not a privileged backdoor.
3. **Partial failure is a first-class outcome**, not an exception. It produces a structured remediation task listing exactly what succeeded, what was compensated, and what needs human action.
4. **Compensation may fail.** Then we escalate loudly with a full evidence bundle. Silent failure here is the worst possible outcome for trust.

---

## 5. Human-in-the-Loop as a Workflow Primitive

`host.human.ask()` and policy-triggered approval gates both suspend the session (doc 03 §3.1) and create an **Approval Task**:

```yaml
approval_task:
  session_id, action_record_id, agent, requester_chain
  what: "Create journal entry JE-2026-0914 for $412,300"
  why: "Variance of $412,300 identified between GL 4000 and bank feed"
  evidence: [links to trace, retrieved docs, prior similar approvals]
  proposed_action: { tool, args (rendered human-readable), simulated_effect }
  routing: role:finance_controller, exclude: delegation_chain.root   # segregation of duties
  sla: 4h, on_timeout: escalate → auto_deny
  options: [approve, approve_with_edits, reject, reject_with_guidance]
```

Design notes that matter more than they look:
- **`approve_with_edits`** captures the highest-value training signal in the system — what the human actually wanted. It feeds evals and few-shot exemplars (doc 10 §6).
- **`reject_with_guidance`** returns text to the agent, which may re-plan; this converts approvals from a gate into a teaching loop.
- **Segregation of duties** is enforced structurally: the approver cannot be the human who initiated the chain, for actions policy marks as dual-control.
- **Batching:** approvals for similar low-risk actions are grouped ("approve all 14 of these tier-1 replies") or sampled ("auto-approve, review 5%") — without this, approval load caps the platform's value at Level 1 forever.

---

## 6. Scheduling & Triggers

| Trigger | Semantics |
|---|---|
| **API/sync** | Caller waits; strict latency budget; suspension not allowed (fail fast if approval needed, or degrade to async with a handle) |
| **Async job** | Returns handle; result via webhook/poll |
| **Scheduled** | Cron with jitter, per-tenant concurrency caps, catch-up policy (skip vs. backfill — must be explicit, defaults to skip) |
| **Event** | Consumed from a bus (Kafka/EventBridge/webhooks) with dedupe by business key, DLQ, and replay-from-offset support |

Priority classes and shedding as in doc 03 §4.2. Workflows also carry a **deadline** propagated to children so a workflow cannot outlive its business relevance.

---

## 7. Observability Hooks

The orchestrator emits the spine of the trace: session span → step spans → action spans, with policy decisions and lease events attached. Because the journal is the source of truth, traces are reconstructable even if the telemetry pipeline drops data — an important property when the audit story depends on it.

---

## 8. Open Questions

1. **Blackboard consistency model.** Optimistic (versioned workspace, conflict on write) vs. pessimistic (leases everywhere)? Optimistic is cheaper but agents handle conflict poorly. **Leaning:** leases for writes, optimistic reads, in v2.
2. **Cross-workflow deadlock.** Two workflows each holding leases the other needs. Detect via wait-for graph and abort the younger workflow (classic), or use ordered lease acquisition (requires knowing the resource set upfront — often impossible for agents)? **Leaning:** wait-for detection + youngest-abort, with the abort surfaced as a re-planning opportunity.
3. **Budget inheritance semantics.** Should a child's unused budget return to the parent? Simpler not to; wasteful if not. **Leaning:** return on clean completion only.
4. **Do we expose the journal to customers?** It is the best debugging artifact we have, and also the most sensitive (contains prompts and retrieved data). **Leaning:** yes, with data-class-aware redaction and retention controls.
