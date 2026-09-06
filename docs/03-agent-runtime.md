# Doc 03 — Agent Runtime & Isolation

**Owns:** executing an agent session safely, with bounded resources, resumable state, and zero ambient authority.

---

## 1. Execution Model

A **Session** is one bounded execution of an agent instance. It is:
- **Identified** by `session_id`, tied to an immutable `agent_version` digest.
- **Bounded** by a budget vector: wall-clock, CPU-seconds, memory, tokens, tool-call count, spend ($).
- **Durable**: every step transition is checkpointed; a node loss resumes elsewhere.
- **Isolated**: no network except the gateway; no host filesystem; no ambient credentials.

### 1.1 The step loop

```
step = { observation, reasoning, action }
while not terminal and budget_remaining:
    1. Runtime materializes context (working memory + retrieved + tool results)
    2. Agent code (guest) decides an action  ── may itself call the model
    3. Runtime intercepts the action at the host boundary  → PEP
    4. Checkpoint(step_id, state_delta, action_record_id)   ← durable
    5. Execute action via gateway; capture result
    6. Append result to working memory (bounded, evicted per policy)
```

The guest never performs I/O. It emits **action intents** over a host ABI; the runtime host process performs the I/O. This inversion is what makes policy unbypassable and replay deterministic.

### 1.2 Host ABI (the sandbox contract)

A minimal, stable interface — deliberately small so both runtime tiers and all guest frameworks implement one thing:

```
host.model.invoke(request)        -> stream|response
host.tool.call(tool, args)        -> result | Denied | PendingApproval
host.memory.query(spec)           -> documents (already ACL-filtered)
host.memory.write(scope, item)    -> ack
host.agent.spawn(agent, input, scope_attenuation) -> handle
host.human.ask(prompt, options)   -> response          // HITL as a first-class call
host.checkpoint(label, state)     -> ack
host.log(event)                   -> ack
host.clock()/host.random(seed)    -> deterministic under replay
```

Note `host.clock` and `host.random`: nondeterminism must be *mediated* or replay is impossible. All nondeterministic values are recorded at first execution and replayed from the journal.

---

## 2. Isolation Tiers

| | **Tier A — microVM** | **Tier B — Pooled Sandbox** |
|---|---|---|
| Tech | Firecracker microVM, per-session kernel | gVisor/seccomp-hardened container, pooled + recycled |
| Used for | Code execution, `write`/`irreversible` tools, untrusted third-party agent code, long sessions (UC-2, UC-3, UC-4) | Read-only reasoning + retrieval, high-volume short sessions (UC-1) |
| Cold start | ~125–250ms (warm pool → ~30ms) | ~10–30ms |
| Memory floor | ~128–256MB | ~48MB |
| Blast radius | Kernel boundary | Syscall-filter boundary |
| Recycling | Destroy after session; never reused | Reused only within the same tenant + agent + data class, wiped between sessions |

**Selection rule** is derived, not user-chosen: `tier = A if (agent declares code_exec) or (any bound tool has reversibility != reversible) or (data_class ∈ restricted set) else B`. Users may force A, never force B. This prevents "developer picks the cheap tier for a risky agent."

### 2.1 Defense-in-depth inside the sandbox
1. **Network:** default-deny egress; only a unix socket / vsock to the host gateway. No DNS. No metadata endpoint (IMDS blocked explicitly — a classic cloud escape path).
2. **Filesystem:** read-only root, tmpfs scratch with quota, no host mounts. Code artifacts delivered as content-addressed, signature-verified layers.
3. **Credentials:** the sandbox holds only a short-lived **session token** identifying the session to the host; it never holds downstream secrets (see doc 04).
4. **Syscalls:** seccomp allowlist; no `ptrace`, no raw sockets, no `mount`.
5. **Resource caps:** cgroups v2 for CPU/mem/pids/io; hard kill on breach with a graceful-drain grace period.
6. **Time-bounding:** every session has a hard deadline; long-running sessions must checkpoint-and-suspend rather than hold compute.

---

## 3. Session Lifecycle & State

### 3.1 States
```
PENDING → ADMITTED → RUNNING ⇄ SUSPENDED
                        ↓            ↓
                  WAITING_APPROVAL   ↓
                        ↓            ↓
        COMPLETED / FAILED / TERMINATED(kill) / EXPIRED
```

**SUSPENDED** is the economically important state: a session waiting on a human approval or a slow external job releases its sandbox entirely. State lives in durable storage; resume rehydrates into a fresh sandbox. Without this, UC-2 (approval-heavy) would pin idle compute for hours.

### 3.2 Checkpointing design

Two mechanisms, chosen per tier:

- **Logical checkpoint (default, both tiers).** Event-sourced: the journal of `(step, action intent, action result, nondeterminism record)` is the state. Resume = replay journal into a fresh guest to reconstruct in-memory state. Cheap, portable, enables deterministic replay for evaluation. Requires guest determinism given the journal — enforced by the ABI.
- **Memory snapshot (Tier A only, opt-in).** Firecracker snapshot of the VM for sessions with expensive non-reproducible in-memory state (e.g. a built index, a long code-exec workspace). Bigger (100s of MB), tenant-encrypted, TTL'd.

**Tradeoff:** logical checkpointing makes resume O(steps) instead of O(1). Mitigation: periodic *materialized state* snapshots every N steps to bound replay depth, plus a "context compaction" step that summarizes older working memory (which is also what keeps token cost bounded).

### 3.3 Working memory management
Context is a managed resource, not an ever-growing list:
- Bounded by token budget with policy-driven eviction (recency + relevance + pinned items).
- Compaction: when > X% of budget, summarize the oldest span into a compact note; the full span remains in the trace and episodic memory (nothing is lost for audit).
- Tool results larger than a threshold are stored by reference and re-fetched on demand rather than inlined — this alone cuts token spend materially in UC-3.

---

## 4. Scheduling & Capacity

### 4.1 Warm pools
Per (tier, region, resource-class, tenant-class) warm pools, sized by a predictive controller on arrival-rate EWMA + scheduled workload calendar. Warm sandboxes are pre-booted, un-personalized VMs that receive the session token and artifact mount at claim time.

**Cost control:** warm-pool idle cost is the platform's single biggest waste risk. Policy: pool size = `p95(concurrent_starts_in_window) × safety_factor`, floor by tenant SLA tier, aggressively decayed off-peak. Free/dev tiers get no warm pool (accept cold start).

### 4.2 Admission control & fairness
Before a session starts, admission control checks: tenant concurrency quota, spend budget (hard + soft caps), agent-level rate limit, global kill-switch, trust ceiling, and current platform saturation. Under saturation, sessions are shed by priority class: `interactive_human_waiting > business_critical_batch > background > dev`. Shedding returns a retryable error with backoff hints, never a silent drop.

### 4.3 Noisy-neighbor and fairness
Per-tenant weighted fair queuing on the runtime scheduler; per-tenant token-bucket at the Model and Tool gateways. A runaway agent (infinite tool loop) is caught by: per-session action-count budget, loop detector (repeated identical action signature > N), and spend velocity alarm.

---

## 5. Failure Handling

| Failure | Behavior |
|---|---|
| Sandbox crash | Journal intact → reschedule; if crash repeats 3×, fail session with diagnostic bundle |
| Node loss | Orchestrator lease expiry → resume elsewhere from last checkpoint |
| Model provider 5xx/timeout | Gateway-level retry w/ jitter, then fallback model if policy allows, else suspend-and-retry |
| Tool timeout | Configurable per tool; result recorded as `timeout` and surfaced to the agent as an observation (agents must handle failure as data, not exceptions) |
| Approval timeout | Policy-driven: `escalate | auto-deny | auto-approve(never for irreversible) | suspend indefinitely` |
| Poison step (repeated failure on resume) | Quarantine session, alert owner, preserve full state for debugging |
| Budget exhaustion | Graceful termination with partial-result callback; agent gets one final "wrap up" step |

**Idempotency:** every tool call carries an idempotency key derived from `(session_id, step_id, args_digest)`. On resume after an ambiguous failure, the Tool Gateway consults an outcome cache before re-issuing — this is what prevents "the agent created three journal entries because the node died mid-call."

---

## 6. Multi-Region & Data Residency

Sessions are pinned to a region satisfying the tenant's residency policy. The runtime refuses to start if the required model or tool endpoint is not available in-region (rather than silently crossing a border — a common and expensive compliance failure).

---

## 7. Open Questions

1. **Snapshot restore across kernel versions** — Firecracker snapshots are sensitive to host changes; do we forbid snapshot resume across host upgrades (simpler, occasional forced replay) or maintain compatibility windows?
2. **GPU-attached sandboxes** for agents doing local model inference or heavy vision work — needed for v2; changes the pooling economics substantially.
3. **Guest determinism enforcement** — how strictly do we police guest code that reaches for nondeterminism outside the ABI (e.g. `time.time()` in Python)? Options: monkeypatch the stdlib in our SDK (pragmatic), or accept "best-effort replay" for foreign frameworks and mark those sessions non-replayable. **Leaning:** monkeypatch in our SDK, flag foreign sessions as `replay: approximate`.
4. **Session-level encryption of working memory at rest** — always on, or only for restricted data classes (cost/latency)?
