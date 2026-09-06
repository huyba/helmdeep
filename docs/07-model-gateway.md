# Doc 07 — Model Gateway

**Owns:** every model invocation. Provides vendor neutrality, cost/quota control, safety pre- and post-processing, and the version pinning that makes agent behavior reproducible.

---

## 1. Why a Gateway (and not direct SDK calls)

If agents call providers directly, the enterprise loses: spend control, version pinning, PII controls, failover, caching, and a consistent record of what was sent to a third party. That last one is a compliance blocker on its own — "prove no PHI left our tenancy" is unanswerable without a chokepoint.

---

## 2. Normalized Interface

```
ModelRequest {
  purpose: "reasoning" | "extraction" | "classification" | "embedding" | "vision" | "code"
  messages: [...]                    // normalized roles + typed content parts
  tools?: [...]                      // normalized tool schema
  response_format?: json_schema      // structured output contract
  constraints: { max_tokens, temperature, stop, seed? }
  routing_hints: { latency_class: "interactive"|"batch", quality_floor: "high"|"standard" }
  context: { session_id, agent_id, data_classes, residency, tenant }
}
```

Agents declare **purpose and constraints**, not a model name, by default. Model selection is a *policy decision*, which means the platform can upgrade models, respond to outages, and enforce cost tiers without touching agent code. Agents may pin a specific model when reproducibility matters (evals, regulated workloads) — pinning is allowed, hardcoding a vendor is not.

---

## 3. Routing & Model Policy

Routing inputs: purpose · quality floor · latency class · data classes present · tenant residency · cost budget state · provider health · agent's pinned version (if any).

```
route(request) → provider+model+version
  1. Filter: models permitted for these data_classes & residency  (hard constraint)
  2. Filter: models approved in the tenant's Model Catalog        (hard constraint)
  3. Rank: quality tier ≥ floor, then cost, then p95 latency, then provider health
  4. Apply: canary/traffic-split rules from the catalog
```

**Model Catalog** entries carry: approved status, version pinning rule, allowed data classes, residency, cost per token, quality tier per purpose, deprecation date, and a link to the eval scorecard that justified approval. A model is not usable until it has an eval scorecard for the purposes it is approved for — this is the enterprise's answer to "how do you know a model change won't break your agents?"

### 3.1 Version pinning & the silent-upgrade problem
Provider aliases (e.g. "latest") are **banned** at the gateway. Every call resolves to an explicit version, recorded in the Action Record. When a provider deprecates a version, the catalog raises a migration task, evals run against the replacement, and the diff is presented before the pin moves. This is the single most valuable feature for enterprises burned by "our agent's behavior changed overnight."

### 3.2 Fallback
On 5xx/timeout/rate-limit: retry with jitter → same-tier alternate provider (if policy allows cross-vendor for this data class) → degraded tier with a `quality_degraded` flag propagated into the session (so downstream policy can require approval for actions taken under degraded reasoning). Fallback is *recorded*, never silent.

---

## 4. Safety Pipeline

**Inbound (before provider):**
1. Data-class check — is this content permitted to leave for this provider/region?
2. PII detection + policy action: `allow | redact | tokenize | block`. Tokenized values are re-hydrated only inside the gateway when needed downstream.
3. Untrusted-content wrapping — tool results and retrieved documents are delimited and labeled as data, never merged into the instruction channel.
4. Prompt-injection detection on newly introduced untrusted spans; a hit sets session taint (docs 04, 11).
5. Budget/quota admission.

**Outbound (after provider):**
6. Structured-output validation against the declared schema; auto-repair once, then fail as a structured observation.
7. Content safety classification (per tenant's configured thresholds).
8. Sensitive-data leak check — did the output echo restricted data into a channel that will egress?
9. Cost/token accounting and Action Record write.

Each stage has a latency budget; the whole safety pipeline targets **< 25ms p99 added**, achieved by running detectors in parallel and using small fast classifiers rather than LLM-based judging on the hot path (LLM judging is reserved for async sampling in doc 10).

---

## 5. Caching

| Layer | What | Benefit | Risk control |
|---|---|---|---|
| **Provider prompt cache** | Long stable prefixes (system prompt, tool schemas) | Large cost/latency win | Cache keys must include tenant + data-class to prevent cross-tenant reuse |
| **Exact-match response cache** | Identical request digest within TTL | Big for classification/extraction | Only for `purpose ∈ {classification, extraction, embedding}`; never for reasoning steps that must reflect fresh state |
| **Semantic cache** | Near-duplicate queries | Meaningful in UC-1 | Off by default; opt-in per agent, with a similarity floor and mandatory sampling audit — semantic cache hits are a correctness risk |
| **Embedding cache** | Content-addressed by text digest + model version | Very high hit rate | Invalidate on model version change |

Cache entries are tenant-scoped and encrypted; cache hits are recorded in the Action Record (a cached response is not the same evidence as a fresh one, and auditors care).

---

## 6. Quotas, Budgets & Cost Control

Hierarchical budgets: `tenant → business unit → agent → session`. Each has soft (alert) and hard (block) thresholds, with time windows.

- **Pre-flight estimation:** the gateway estimates token cost before the call; a session that would exceed its remaining budget is stopped before spending.
- **Spend velocity alarms:** anomaly detection on $/minute per agent catches runaway loops faster than absolute budgets.
- **Cost attribution** flows to the Action Record → cost engine → per-outcome unit economics (doc 10 §5). "Cost per resolved ticket" is the metric that sells the platform internally.
- **Downgrade policy:** on budget pressure, policy may route non-critical purposes to cheaper models rather than hard-failing — configurable, and recorded.

---

## 7. Self-Hosted & Private Models

Enterprises with strict residency run open-weight models in their own VPC. The gateway treats these as another provider with a capability profile (context window, tool-calling quality, structured-output reliability). Routing policy can require self-hosted models for specific data classes — this is often the only way to get `restricted` data class agents approved at all.

Implication for capacity (doc 12): self-hosted inference means GPU capacity planning, batching, and autoscaling become platform concerns, not just the provider's.

---

## 8. Reliability & Performance

- Streaming pass-through with backpressure; first-token latency is what users feel.
- Per-provider connection pools, circuit breakers, and independent rate-limit accounting (respect provider headers, do not guess).
- Multi-region provider endpoints selected by residency then latency.
- **Overhead SLO: < 40ms p99** added to any model call, excluding safety pipeline stages that policy makes mandatory.

---

## 9. Open Questions

1. **Semantic cache in regulated workloads** — probably never enable for `restricted` classes. Should it be structurally disallowed rather than configurable?
2. **Do we allow cross-vendor fallback by default?** It maximizes uptime but means data may reach a different processor than the DPA anticipated. **Leaning:** off by default, opt-in per data class, with a clear consent record.
3. **Fine-tuning / adapters.** If a tenant fine-tunes a model on their data, the catalog must track lineage and prevent leakage across tenants. In scope for v2, not v1.
4. **Provider-side agentic features.** Providers increasingly offer their own tool-execution and memory. Using them would bypass our gateways and destroy our governance story. **Decision: we do not use provider-side tool execution.** We accept the extra latency of running tools ourselves.
