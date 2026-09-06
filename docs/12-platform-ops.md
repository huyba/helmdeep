# Doc 12 — Platform Operations

---

## 1. Multi-Tenancy Model

| Layer | Isolation | Rationale |
|---|---|---|
| Control plane | Logical (row-level, tenant-scoped keys) | Cost efficiency; config data is small and low-risk |
| Orchestrator / journal | Logical, partitioned per tenant | Volume demands sharing; partitioning bounds blast radius |
| Sandbox (Tier A) | Physical per session | Kernel boundary is the security claim |
| Sandbox (Tier B) | Pooled per (tenant, agent, data class) | Never shared across tenants, even after wipe |
| Vector index | **Physical per tenant** | A filter bug here is a cross-tenant breach (doc 08 §5) |
| Audit ledger | Per-tenant chain + per-tenant keys | Independently verifiable; supports tenant-held custody |

**Noisy-neighbor controls:** per-tenant quotas at admission, weighted fair queuing in the scheduler, token buckets at both gateways, and per-tenant circuit breakers so one tenant's failing connector cannot exhaust shared pools.

---

## 2. Deployment Topologies

| Topology | Control plane | Data plane | Buyer |
|---|---|---|---|
| **SaaS** | Ours | Ours | Mid-market, fast start |
| **BYOC** (primary for our wedge) | Ours (metadata only) | Customer VPC/subscription | Regulated enterprise: data never leaves their boundary |
| **Private** | Customer-hosted | Customer-hosted | Defense/public sector; heavy support burden |
| **Air-gapped** | Customer-hosted, offline updates | Customer-hosted | Out of scope until there is a paying design partner |

**BYOC is a product decision, not just a deployment option.** It forces: no shared-state assumptions between planes, versioned APIs with backward compatibility (customers upgrade on their schedule), offline-capable policy bundles, and telemetry that flows one way with configurable redaction. Designing for it later is a rewrite; designing for it now costs maybe 15% more effort.

**Regions & residency:** sessions pin to a region satisfying tenant residency; the runtime refuses cross-border execution rather than silently crossing (doc 03 §6). Model and connector availability per region is a first-class catalog attribute.

---

## 3. Scaling Strategy

| Component | Scaling axis | Bottleneck | Approach |
|---|---|---|---|
| Sandbox fleet | Concurrent sessions | Compute + warm-pool cost | Predictive pool sizing; spot/preemptible for Tier B; bin-pack by resource class |
| Orchestrator | Journal writes/s | Postgres write throughput | Partition by tenant+time; hot tail in Redis; archive to object store; consider a log-structured store beyond ~50k writes/s |
| PDP | Decisions/s | Bundle size, cache | Sidecar with local eval; bundles compiled and pruned per agent scope |
| Model Gateway | RPS + streaming conns | Provider limits, connection pools | Per-provider pools, queue with priority, provider-side quota tracking |
| Tool Gateway | RPS | Downstream SaaS limits | Bulkheads per connector, adaptive rate limiting from response headers |
| Vector search | QPS + index size | Memory/ANN | Per-tenant indexes, tiering, replica read scaling |
| Ledger | Writes/s (never sampled) | Durability path | Batched hash-chain commits with group commit; this is on the critical path for irreversible actions, so it gets dedicated capacity |

**Scale targets:** 10k concurrent sessions/region at GA; 100k design ceiling. The first real wall is the journal write path — designing partitioning correctly at the start avoids a painful migration.

---

## 4. SLOs & Error Budgets

| SLO | Target | Consequence of breach |
|---|---|---|
| Control plane availability | 99.9% | Data plane keeps running on cached bundles |
| Data plane session-start success | 99.95% | Feature freeze until error budget recovers |
| Policy decision p99 | < 10ms | Perf regression blocks release |
| Sandbox cold start p95 | < 250ms (Tier A) / < 50ms (Tier B) | Pool tuning |
| Gateway overhead p99 | < 40ms model / < 35ms tool | Release gate |
| Ledger write success | 100% (no loss) | Irreversible actions blocked — availability sacrificed for integrity, by design |
| Credential revocation propagation | < 5s | Security incident review |
| Approval SLA attainment | ≥ 95% within configured SLA | Customer-facing metric; drives escalation tuning |

**Degradation modes, explicitly designed:**
- Control plane down → data plane runs on last-known-good signed bundles for a bounded TTL (e.g. 4h), then refuses new sessions but lets running ones finish.
- Guardrail service down → fail closed for writes, fail open (with taint) for reads.
- Ledger down → block irreversible actions, queue reversible records with durable local buffering.
- Model provider down → fallback chain, then queue-and-retry for async work, fail fast for sync.

---

## 5. Disaster Recovery

- **RPO 0** for Action Records and committed journal entries (synchronous replication within region, async cross-region with a monitored lag budget).
- **RPO ≤ 5 min** for session working state; a lost session resumes from its last checkpoint with at-most-one duplicate side effect prevented by idempotency keys.
- **RTO ≤ 30 min** for regional failover; control plane is active-active, data plane active-passive per region with pre-warmed capacity.
- **Quarterly game days**: region failover, provider outage, control-plane partition, ledger unavailability, mass credential revocation. Untested DR is a claim, not a capability.

---

## 6. Capacity & Unit Cost Model

Per-session cost components (illustrative for UC-1, a 20-second read-only session):

| Component | Estimate |
|---|---|
| Model tokens (in ~8k, out ~600, cached prefix) | $0.004–0.02 |
| Sandbox (Tier B, 20s @ pooled) | $0.0004 |
| Retrieval (2 queries + rerank) | $0.0006 |
| Gateway/policy/ledger overhead | $0.0002 |
| Trace/payload storage (amortized, tiered retention) | $0.0003 |
| **Platform overhead as % of model spend** | **~10–15%** |

For UC-3 (90-minute code-executing Tier A session) the mix inverts: sandbox compute becomes dominant, which makes suspend-on-idle (doc 03 §3.1) and context compaction the two highest-leverage cost features in the product.

**Pricing implications (for doc 13):** charge on a blend of *governed actions* and *sandbox-hours*, not pure seats — seats mismatch a world where one team runs 200 agents. Cap platform overhead visibly (e.g. "never more than 15% of your model spend") because the buyer's first objection is that governance doubles the cost of AI.

---

## 7. Operations

- **Upgrades:** control plane rolls continuously; data plane upgrades are drain-and-replace, never in-place under running sessions; BYOC customers get versioned releases with N-2 compatibility and a supported upgrade window.
- **Runbooks and on-call** owned by the platform team; agent-level alerts route to agent owners (doc 10 §4) — this separation is what keeps the platform team's pager survivable at scale.
- **Tenant lifecycle:** onboarding (IdP, connectors, policy baseline, first agent), suspension (freeze agents, preserve audit), offboarding (export evidence bundle, verified deletion with certificate).
- **Support tooling:** support engineers need trace access without payload access by default; payload access requires customer-approved break-glass, logged and time-boxed.

---

## 8. Open Questions

1. **Spot instances for Tier A?** Cheap, but eviction mid-session is disruptive even with checkpointing. **Leaning:** Tier B on spot, Tier A on-demand with reserved baseline.
2. **How far do we go on BYOC support burden?** Every customer on a different version is a support multiplier. **Leaning:** at most 3 supported versions, forced upgrade after 9 months, priced accordingly.
3. **GPU capacity for self-hosted models** (doc 07 §7) — do we manage it, or require the customer to bring an inference endpoint? **Leaning:** require an endpoint in v1; managing GPU fleets is a different company.
4. **Journal store choice at scale** — Postgres partitioning is fine to ~50k writes/s; beyond that we need a purpose-built log. Decide before the first customer crosses 20k.
