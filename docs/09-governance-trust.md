# Doc 09 — Governance & Progressive Trust

**The product's core differentiator.** Anyone can run agents; the reason an enterprise buys a platform is to make agent autonomy *defensible* to a CISO, an auditor, and a board.

---

## 1. The Autonomy Ladder

Autonomy is granted per **(agent × action-class)**, not per agent. An agent may be Level 3 for "search knowledge base" and Level 0 for "issue refund" simultaneously. This granularity is what makes the ladder usable in practice.

| Level | Name | Behavior | Exit criteria to next level |
|---|---|---|---|
| **L0** | **Observe** | Agent runs; writes are *simulated* (dry-run) and diffed against what a human then does. Nothing external changes. | ≥N sessions, agreement rate ≥ threshold, zero policy violations, eval score ≥ bar |
| **L1** | **Suggest** | Agent proposes; a human executes (or one-click approves each action). | Approval rate ≥ threshold with low edit distance; sustained over time window |
| **L2** | **Act with approval** | Agent executes reversible actions autonomously; compensable/irreversible require approval. | Reversible-action error rate below bar; no incidents; owner sign-off |
| **L3** | **Act with sampling** | Agent executes compensable actions autonomously; X% sampled for human review; irreversible still gated. | Sampled review agreement ≥ threshold over larger volume; incident-free window |
| **L4** | **Autonomous within bounds** | Full autonomy inside a declared envelope (value caps, entity scope, time windows); anything outside the envelope drops to L2 behavior. | Reserved; requires explicit executive approval + compensating controls |

**Asymmetry rule:** promotion is deliberate (evidence + human approval); demotion is automatic and immediate on incident, policy violation, eval regression, model version change, or drift alarm. An agent that gets demoted must re-earn its level — but with credit for prior history, so a single bad day does not reset months of evidence.

### 1.1 Trust scoring — inputs
Windowed, per (agent version, action class):

| Signal | Source | Weight rationale |
|---|---|---|
| Human agreement rate (approve / approve-with-edits / reject) | Approval Service | Strongest direct signal |
| Edit distance on approved-with-edits | Approval Service | Distinguishes "fine" from "barely acceptable" |
| Eval suite score | Eval Service | Catches regressions before production exposure |
| Policy violation attempts | PDP | Even blocked attempts indicate misalignment |
| Incident linkage | Incident mgmt | Hard demotion trigger |
| Outcome quality (business KPI) | Customer-defined callback | The metric that actually matters, when available |
| Drift indicators | Monitoring | Model/retrieval/tool-behavior change |
| Volume & diversity | Action Records | Prevents "10 easy successes" from earning autonomy |

Scoring must be **statistically honest**: use Wilson lower bounds rather than raw rates, require minimum sample sizes, and stratify by task difficulty so an agent cannot game promotion by cherry-picking easy cases. A naive "95% success → promote" rule is the most likely way this feature fails in production.

### 1.2 Version binding
Trust attaches to an **agent version digest**, not to the agent name. A new version starts at a level derived from — but not equal to — its predecessor: minor prompt change inherits with a probation window; model change or tool-scope change forces a step down. This is the mechanism that answers "what happens when they change the prompt?", the question every security reviewer asks.

---

## 2. Policy Model

### 2.1 Policy layers (evaluated together, deny wins)
1. **Platform baseline** — non-negotiable (no agent may disable audit; no ambient credentials; irreversible actions always recorded pre-execution).
2. **Tenant policy** — enterprise-wide rules from security/GRC.
3. **Business unit policy** — delegated authoring within tenant guardrails.
4. **Agent policy** — declared in `agent.yaml`, may only *narrow*.
5. **Contextual overlays** — time-boxed (incident mode, quarter-end freeze, elevated-risk mode).

### 2.2 Policy as code
- Written in Cedar/Rego, stored in git, reviewed via PR, tested with unit tests and **dry-run against historical Action Records** ("this new rule would have blocked 43 actions last month — here they are").
- Compiled to signed bundles, versioned, distributed to sidecar PDPs, pinned per session (doc 02 §3.2).
- Emergency policies can force-invalidate running sessions.

### 2.3 What policy can express
`allow | deny | require_approval(role, sla) | allow_with_constraints(row_filter, redaction, rate_cap, value_cap) | require_dual_control | log_only(shadow mode)`

`log_only` matters: it lets a security team roll out a rule in observation mode and measure impact before enforcing — the difference between a policy engine people use and one they route around.

---

## 3. Approval Experience

Approval load is the limiting factor on value. Design accordingly:

- **Rich context, not raw JSON.** Every request shows: plain-language description, the *effect* (simulated diff where possible), the evidence the agent used, similar past decisions and how they went, and the cost of delay.
- **Batching & sampling** as first-class modes (doc 06 §5).
- **Mobile-first surface** plus Slack/Teams integration — approvals that require opening a web console do not get done within SLA.
- **Delegation & coverage:** approval authority can be delegated with expiry; unattended approvals escalate rather than silently expire.
- **Approval analytics:** which agents generate the most approval load, what fraction are rubber-stamped (a rubber-stamp rate above ~98% means the gate is theater and should be replaced by sampling), which approvers are bottlenecks.

---

## 4. Change Management

Enterprises gate change; agents change continuously. We reconcile this by making **every behavior-affecting input a versioned artifact**:

| Input | Versioned? | Change control |
|---|---|---|
| Agent code | Yes (digest) | PR + CI + eval gate |
| Prompts | Yes (separate assets) | Same as code — prompts are code |
| Model version | Yes (pinned) | Catalog migration workflow + eval diff |
| Tool schemas | Yes | Major-version pinning |
| Policy bundles | Yes | PR + dry-run + staged rollout |
| Knowledge corpus | Snapshot-versioned for evals | Freshness monitoring |
| Trust level | Yes (with evidence) | Promotion workflow |

**Promotion pipeline:** dev → staging (synthetic + replayed real traffic) → canary (small % of traffic, mandatory sampling) → GA, with automatic rollback on eval or SLO regression. Rollback restores the previous agent version *and* its previous trust level.

---

## 5. Compliance Mapping

| Framework | What it demands | How we satisfy it |
|---|---|---|
| **SOX** | Controls over financial reporting; segregation of duties; evidence | Dual-control policy, approver ≠ initiator, immutable Action Records, quarterly access review export |
| **HIPAA** | PHI minimum necessary; BAA-covered processing | Data-class routing (PHI → approved models/regions only), redaction, per-class audit |
| **GDPR/CCPA** | Lawful basis, minimization, erasure, automated-decision transparency | Explicit memory writes, subject-keyed deletion, decision records with reasoning traces, human-review path |
| **EU AI Act (high-risk)** | Risk management, logging, human oversight, accuracy monitoring, technical documentation | Trust ladder = documented human oversight; Action Ledger = logging; eval suites = accuracy monitoring; this doc set = technical documentation |
| **SOC 2 / ISO 27001** | Access control, change management, monitoring | §4, doc 04, doc 10 |
| **NIST AI RMF** | Govern/Map/Measure/Manage | Registry (map), evals+trust (measure), policy+approvals (manage) |

**Evidence packaging** is a product feature, not a report we run manually: an auditor-facing export producing, for a stated period, every agent, its authority, its actions, its approvals, its incidents, and its version history — signed and hash-chained.

---

## 6. Agent Registry & Lifecycle Governance

Every agent has: an owning group, a business purpose, a risk classification, a data-class inventory, a review date, and a decommission plan. Governance rituals the platform supports directly:
- **Onboarding review** — risk-tiered; high-risk agents require security + data-owner sign-off before first production run.
- **Periodic recertification** — quarterly: is this agent still needed, still correctly scoped, still performing?
- **Decommissioning** — revoke grants, archive memory per retention policy, preserve audit records, notify dependents.
- **Orphan detection** — owner left the company, no runs in 90 days, or failing evals with no owner response → auto-suspend.

---

## 7. Open Questions

1. **Who owns promotion decisions?** Business owner (fast, knows the work) or security (slow, knows the risk)? **Leaning:** business owner within a security-set ceiling — security defines the maximum reachable level per risk tier; business decides within it.
2. **Can trust be transferred across tenants?** "This agent template is L3-proven at 40 customers" is powerful but privacy-fraught and possibly misleading. **Leaning:** publish aggregate template quality signals, never transfer trust levels automatically.
3. **How do we prevent gaming?** If teams are measured on autonomy level, they will optimize for promotion. Stratified sampling, adversarial eval sets, and mandatory difficulty-mix are partial answers; incentive design is the customer's problem but our metrics shape it.
4. **Regulatory drift.** EU AI Act implementation details and sectoral rules keep moving. Policy packs must be shippable as content updates, not code releases.
