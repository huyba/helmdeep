# Doc 10 — Observability, Evaluation & Audit

Three distinct consumers with different needs, served by one event spine:
- **Developers** need to debug *why* an agent did something.
- **Operators** need SLOs, cost, and saturation.
- **Auditors/security** need immutable, complete, tamper-evident evidence.

---

## 1. Agent-Semantic Tracing

Standard APM spans don't capture agent semantics. We extend OpenTelemetry with our own conventions:

```
Span: session            (agent, version, trigger, principal chain, trust level, outcome, cost)
 ├─ Span: step[n]        (plan/reason summary, context token count)
 │   ├─ Span: model_call (provider, model+version, tokens in/out, cache hit, latency, safety verdicts)
 │   ├─ Span: retrieval  (query, filters applied, ACL partition, docs returned + scores, groundedness)
 │   ├─ Span: policy     (rule set version, matched rules, decision, latency)
 │   ├─ Span: tool_call  (tool+version, args digest, reversibility, egress target, idempotency key, result status)
 │   └─ Span: approval   (requested→decided, approver, decision, edit distance)
 └─ Span: compensation   (if any)
```

Trace and journal are linked by `session_id`; traces may be sampled, **the journal and Action Records never are**. That distinction keeps observability cost bounded without weakening the audit guarantee.

### 1.1 Payload handling
Prompts, retrieved documents, and tool payloads are the most useful and most sensitive artifacts. Policy: store by reference in tenant-controlled storage, encrypted with tenant keys, with data-class-aware retention (e.g. restricted-class payloads 30 days, metadata 7 years), and redaction applied before storage where policy requires. Digests are always retained so integrity is provable even after payload expiry.

---

## 2. Audit Ledger

Separate from telemetry, with stronger guarantees:
- **Append-only, hash-chained** (each record includes the previous record's hash), periodically anchored (published digest / WORM storage) so tampering is detectable.
- **Written synchronously before irreversible actions** (doc 02 §3.1).
- **Complete** — no sampling, no dropping under load; backpressure on the action path rather than losing records. If the ledger is unavailable, irreversible actions are blocked. This is a deliberate availability-vs-integrity tradeoff, and the right one for a governance product.
- **Queryable** for the questions auditors actually ask: by human principal, by agent, by data class, by resource, by time, by approval status.

---

## 3. Evaluation

### 3.1 Offline evals (pre-deployment gate)
| Suite | Purpose | Cadence |
|---|---|---|
| **Golden tasks** | Curated inputs with known-good outcomes per agent | Every version |
| **Regression set** | Historical failures + their fixes | Every version |
| **Adversarial set** | Injection, jailbreak, out-of-scope requests, ambiguous instructions | Every version + weekly |
| **Policy conformance** | Does the agent respect scope/refuse correctly? | Every version |
| **Cost/latency profile** | Budget conformance | Every version |

Scoring mixes deterministic checks (did it call the right tool with the right args? did it cite sources? did the structured output validate?) with LLM-judge rubrics for open-ended quality — judges themselves are versioned and periodically calibrated against human labels, because an uncalibrated judge silently redefines "good."

### 3.2 Online quality signals
Human approval outcomes, edit distance, downstream business outcomes (via customer-defined callbacks: was the ticket reopened? was the journal entry reversed?), user feedback, and self-consistency checks. These flow into the trust engine (doc 09 §1.1).

### 3.3 Replay & counterfactuals
Because sessions are journaled deterministically (doc 06 §1.1), we can:
- Replay a past session against a **new agent version** and diff the actions — the killer feature for safe upgrades ("this prompt change would have altered 12 of last week's 4,000 decisions; here they are").
- Replay against a **new policy bundle** to preview enforcement impact.
- Replay against a **new model version** to quantify provider-upgrade risk.

Caveat to state honestly: replay is faithful for the agent's *decision logic* given recorded observations. It cannot re-run the external world. Actions are simulated, not re-executed, and external state may have changed — so replay answers "would it have decided differently," not "would the outcome have been better."

---

## 4. Monitoring & SLOs

**Platform SLIs:** session start latency, policy decision latency, gateway overhead, sandbox pool availability, journal write latency, approval SLA attainment, ledger write success.

**Agent SLIs (per agent):** task success rate, human agreement rate, tool error rate, retrieval groundedness, cost per outcome, p95 session duration.

**Drift detectors:**
- Model drift — behavior change on a fixed probe set run continuously against pinned versions (catches silent provider changes).
- Retrieval drift — score distributions, corpus freshness, ACL staleness.
- Input drift — distribution shift in incoming tasks (the agent's world changed, not the agent).
- Cost drift — tokens per session trending up (usually context bloat or a loop).

Alerts route to the **agent owner**, not to the platform team. Ownership routing is the difference between a platform that scales to 300 agents and one that becomes a central team's pager hell.

---

## 5. Cost Attribution

Every Action Record carries cost components (model tokens, sandbox seconds, tool call fees, storage). Rollups: session → agent → team → business outcome.

Outputs that matter:
- **Cost per outcome** vs. the human baseline — the ROI number that justifies the program.
- **Cost anomaly detection** per agent version (catches loops and context bloat early).
- **Showback/chargeback** exports so business units feel their own spend.
- **Efficiency levers surfaced automatically**: "42% of this agent's tokens are re-sent context that could be cached"; "this agent's median session makes 3 redundant retrievals."

---

## 6. The Improvement Loop

```
Production traces + human decisions
   → mine failure clusters (group by failure mode, not by frequency)
   → candidate fixes: new exemplars, prompt change, tool schema fix, policy adjustment, retrieval tuning
   → offline eval on golden + regression + adversarial sets
   → replay diff on real historical sessions
   → canary → GA → trust re-evaluation
```

Two guardrails: (1) approved-with-edits data is the highest-signal training resource, so capture it structurally rather than as free text; (2) **no automatic behavior change** — every improvement lands as a reviewed, versioned artifact (doc 08 §1.4). Continuous learning without versioning is indistinguishable from uncontrolled drift, and no enterprise will accept it.

---

## 7. Open Questions

1. **Trace retention vs. cost.** Full payload retention for 4,000 sessions/day is expensive fast. Tiered retention (all metadata long, payloads short, sampled payloads long) is the plan — is sampled-payload retention sufficient for debugging? Needs validation with real support cases.
2. **LLM-judge cost and reliability** on high-volume agents — sample rate vs. confidence tradeoff; consider distilled small judges for common rubrics.
3. **Customer-defined outcome callbacks** are the best quality signal but require integration work from the customer. How do we make that a 10-minute task rather than a project?
4. **Cross-tenant benchmarking** ("your agent's approval rate is in the 40th percentile for this template") is compelling but privacy-sensitive. Aggregate-only, opt-in, differential-privacy-style noise — or skip entirely in v1.
