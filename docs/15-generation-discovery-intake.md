# Doc 15 — Adaptive System Generation, Estate Discovery & Intake

**Why this doc exists:** docs 00–13 designed the governance runtime (the "Guard" half). Doc 14's teardown showed we missed three things that carry the go-to-market: **intent→system generation**, **AI estate discovery**, and **use-case intake**. This doc designs them.

---

# PART A — Adaptive System Generation ("Build")

## A1. The Job

> A business operator describes an outcome in plain language. The platform learns how the work actually happens, drafts a reviewable specification, builds the application + agents + integrations, tests them, and promotes them through environments into production — with governance attached from the first artifact.

**Key reframe:** this is not "prompt → app." It is **process discovery → spec → build → verify → promote**. The spec review step is what makes it enterprise-grade; without it you have a demo, not a product.

## A2. Pipeline

```
┌── 1. UNDERSTAND ────────────────────────────────────────────┐
│ Inputs: documents, process maps, spreadsheets, meeting      │
│ transcripts, ticket history, approved read-only system access│
│                                                              │
│ Process-mining agent → draft process model                   │
│   • actors, steps, decisions, exceptions, systems touched     │
│   • data objects + where they live                            │
│   • rules (explicit) and gaps (implicit / undocumented)       │
│ Gap interrogation: surfaces MISSING DECISIONS as questions    │
│   ("what happens when the invoice has no PO?") → human answers│
│   stored WITH the affected step                               │
│                                                              │
│ Output: plain-language SPECIFICATION (reviewable artifact)    │
└──────────────────────────────────────────────────────────────┘
        ↓  human review + sign-off (business + technical + risk)
┌── 2. PLAN ───────────────────────────────────────────────────┐
│ Spec → build plan: components, agents, connectors, data model,│
│ UI surfaces, tests, required permissions, risk tier            │
│ Blueprint applied (§A4) → inherited standards                  │
│ Cost + time estimate; explicit list of what will need approval │
└──────────────────────────────────────────────────────────────┘
        ↓  plan approval
┌── 3. BUILD ──────────────────────────────────────────────────┐
│ Supervisor decomposes plan → specialist agents:                │
│   researcher · schema/data · backend · frontend · connector ·  │
│   test-author · security-reviewer                              │
│ Runs in our own governed runtime (docs 03–06) — dogfooding      │
│ Independent verification agent: separate context, checks output │
│   against SPEC, not against the builder's own reasoning         │
│ Live preview environment updated continuously                   │
│ Every step, version, decision, test, artifact retained           │
└──────────────────────────────────────────────────────────────┘
        ↓
┌── 4. VERIFY & SCORE ─────────────────────────────────────────┐
│ Readiness scoring (§A5) across 6 dimensions with evidence      │
│ Generated eval suite from the spec's acceptance criteria        │
└──────────────────────────────────────────────────────────────┘
        ↓
┌── 5. PROMOTE ────────────────────────────────────────────────┐
│ preview → development → production                             │
│ Environment-scoped credentials & data (prod creds NEVER in dev) │
│ Auto-registration into the governance registry (Part C)         │
│ Deployment history + one-click rollback                         │
└──────────────────────────────────────────────────────────────┘
        ↓
┌── 6. IMPROVE ────────────────────────────────────────────────┐
│ Change requests start FROM the running system and its decisions │
│ Same controlled path: spec delta → plan → build → verify → promote│
│ Generated apps may expose their own tools via MCP (§A6)         │
└──────────────────────────────────────────────────────────────┘
```

## A3. The Specification Artifact

The spec is the contract between humans and the generator. It must be readable by a business owner and precise enough to generate from:

```yaml
spec_version: 3
process: "Vendor invoice exception handling"
owner: group:ap-operations
outcome: "Exceptions resolved or escalated within 2 business days"
actors:
  - { role: ap_clerk, human: true }
  - { role: exception_agent, human: false, autonomy_target: L2 }
systems_of_record:
  - { name: netsuite, access: [read:invoice, write:journal_entry], criticality: high }
  - { name: sharepoint, access: [read:contracts] }
steps:
  - id: s1
    name: "Classify exception type"
    performer: exception_agent
    inputs: [invoice, po, receipt]
    decision_rules:
      - "No PO and amount < $5,000 → route to auto-match attempt"
      - "No PO and amount >= $5,000 → escalate to buyer"   # ← answered gap, see below
    open_questions_resolved:
      - q: "What if there is no PO at all?"
        answered_by: user:dana@corp
        answer: "Threshold at $5,000, buyer approval above"
        answered_at: 2026-09-03
  - id: s2
    name: "Draft journal entry"
    performer: exception_agent
    reversibility: compensable          # ← flows into doc 05/06 machinery
    requires_approval_below_trust: 3
exceptions:
  - { condition: "vendor not in master", handling: "human task to AP supervisor" }
acceptance_criteria:                     # ← becomes the eval suite
  - "95% of no-PO invoices under threshold auto-matched without human edit"
  - "Zero journal entries posted without an approval record"
data_classes: [financial.ledger, vendor.pii]
non_goals: ["Does not handle intercompany invoices"]
```

Three design points that matter:
1. **Resolved gaps are recorded with attribution.** "Who decided the $5,000 threshold and when" is exactly what an auditor asks about an automated process. Storing the answer next to the step is worth more than any generated code.
2. **Reversibility is declared at spec time**, so the governance machinery (docs 05–06) is wired before a single line is generated.
3. **Acceptance criteria compile to the eval suite** (doc 10 §3.1). No separate test-writing step; the spec *is* the test contract.

## A4. Workspace Blueprints

Standards defined once per workspace, inherited by every generated project:

```yaml
blueprint: acme-enterprise-standard
identity:      { auth: okta_oidc, service_identity: spiffe }
access:        { default_data_classes: [internal], require_obo: true }
design:        { component_library: acme-ds, accessibility: wcag_2.2_aa }
testing:       { min_coverage: 0.7, required_suites: [golden, adversarial, policy] }
review:        { security_review_required_above_risk: medium, no_self_approval: true }
release:       { environments: [preview, dev, prod], rollback_required: true }
runtime:       { tier: auto, region: us-west-2, residency: us }
observability: { trace_sampling: 0.2, siem_stream: splunk-prod }
```

This is the mechanism that lets governance scale without a central bottleneck: the platform team writes the blueprint once, and 200 generated apps inherit it. It also gives the readiness score something objective to score *against*.

## A5. Readiness Scoring

Six dimensions, each with levels and required evidence — visible to the builder as a progress bar toward "production-ready":

| Dimension | L1 | L2 | L3 (prod-ready) |
|---|---|---|---|
| **Capability** | Happy path works | Handles declared exceptions | Meets all acceptance criteria on eval suite |
| **Configuration** | Runs locally | Env-specific config, no hardcoded secrets | Blueprint-compliant, all connectors approved |
| **Tests** | Smoke tests pass | Golden suite ≥ bar | Golden + regression + adversarial + policy conformance |
| **Security** | Secret scan clean | Scopes minimized, data classes declared | Security review signed off; injection suite passes |
| **Operations** | Deployed to dev | Monitoring + alerts + owner assigned | SLOs defined, rollback tested, on-call routed |
| **Release** | Manual deploy | Promotion path configured | Approved by risk tier's required reviewers |

The score is not advisory — **promotion to production is gated on L3 across all six**, with per-dimension exception waivers that are themselves approved and recorded.

## A6. Generated Apps as MCP Servers

Every generated application can expose its capabilities as MCP tools, which means the org's agent ecosystem compounds: today's generated app becomes tomorrow's tool for another agent. Governed the same way as any other tool (doc 05) — schema registration, reversibility classification, risk rating, egress rules. This is the "collective intelligence" flywheel with an actual mechanism behind it.

## A7. Honest Risks with Generation

1. **Generated code is a supply-chain surface.** It runs with real credentials against systems of record. Mandatory: security-reviewer agent with independent context, dependency allowlist, generated-code sandboxing (Tier A always), and human security review above medium risk.
2. **Spec drift.** The running system diverges from the spec as it's edited. Mitigation: changes must go through spec deltas; direct edits to generated code are allowed but flagged and force a spec-reconciliation task.
3. **The 80% trap.** Generation gets to 80% fast and the last 20% is where enterprise processes live. Design for graceful escape: generated code is real, readable, exportable code the customer's engineers can take over — never a locked black box.
4. **It competes with every coding-agent company.** Strategically, this half is expensive and crowded (doc 14 §5). Build it only if the generation → governance funnel is the business model.

---

# PART B — AI Estate Discovery

**Promoted from P2 to P0.** You cannot govern what you cannot see, and every enterprise already has shadow agents. This is also the lowest-friction entry: read-only, no runtime dependency, immediate "oh no" moment in the first demo.

## B1. Discovery Channels

| Channel | Mechanism | Catches |
|---|---|---|
| **Native** | Platform-built agents auto-register at deploy | Everything we built |
| **Gateway** | Agents routed through our model/tool gateway are inventoried by construction | Anything using our infra |
| **Telemetry** | OTel collectors, proxy logs, egress logs, SaaS audit logs (who's calling OpenAI/Anthropic from inside the network?) | Shadow agents using approved networks |
| **Identity** | OAuth grant inventory in the IdP (which apps hold `mail.send` on behalf of users?), service-account analysis | Agents acting via delegated SaaS access |
| **A2A / protocol** | Agents announcing themselves via agent-to-agent protocols | Third-party and partner agents |
| **SaaS-native** | Vendor admin APIs listing AI features enabled in Salesforce/M365/etc. | Bought (not built) AI |
| **Direct registration** | Manual/API entry with intake form | The long tail |

## B2. The Estate Record

```jsonc
{
  "asset_id": "ai_asset_...",
  "type": "agent | application | model | connector | mcp_server | ai_feature",
  "origin": "built_native | built_external | bought | unknown",
  "owner": { "group": "...", "individual": "...", "confidence": "attested|inferred" },
  "declared": {            // what intake said
    "purpose": "...", "data_classes": [...], "tools": [...], "models": [...],
    "human_oversight": "approval_required", "risk_tier": "high"
  },
  "observed": {            // what telemetry actually sees
    "data_classes": [...], "tools": [...], "models": [...],
    "egress_destinations": [...], "volume_30d": 14203, "spend_30d": 4120.55
  },
  "divergence": [          // ← the highest-value field in the whole system
    { "field": "data_classes", "declared": ["internal"], "observed": ["restricted"],
      "severity": "high", "first_seen": "2026-08-14" }
  ],
  "lifecycle": { "status": "active", "approved_version": "...", "last_review": "...", "review_due": "..." }
}
```

**Declared vs. observed divergence is the product.** An inventory is a spreadsheet; a divergence engine is a control. It catches scope creep, silent model swaps, undeclared data access, and abandoned agents — the things that actually go wrong between annual reviews.

## B3. From Discovery to Control
```
discovered (unmanaged) → claimed (owner assigned) → intake submitted
   → risk-tiered → approved with boundaries → routed through gateway (now enforceable)
   → monitored for divergence → periodic recertification → decommissioned
```
The step that converts a *finding* into *control* is routing the agent through our gateway. Until then we can observe and alert but not enforce — be honest about that in sales, because customers will assume discovery equals control.

---

# PART C — Use-Case Intake & Risk Review

**"Approve the use case, not just the tool."** The intake form is where governance becomes operable for a GRC team that does not read policy code.

## C1. Conditional intake
Questions branch on answers, so a low-risk internal summarizer answers 6 questions and a customer-facing agent with PII answers 40:

- Purpose and business owner
- Data: classes accessed, systems, residency, retention
- Tools: which, what reversibility, what blast radius
- Models: which, hosted where, any training on our data
- Decisions: what does it decide, does it affect individuals (EU AI Act trigger), can it act on customers
- Human oversight: approval design, escalation, fallback when it fails
- Volume, budget, expected autonomy target

## C2. Risk tiering → automatic control mapping
| Tier | Trigger examples | Auto-applied controls |
|---|---|---|
| **Low** | Internal, read-only, non-PII | Self-service; standard logging; L2 autonomy ceiling |
| **Medium** | Internal writes, or PII read | Security review; eval suite required; L2 ceiling; quarterly recert |
| **High** | Customer-facing, financial writes, PHI, or irreversible actions | Security + data-owner + legal review; dual control; L1 ceiling until evidence; monthly recert; independent action verification (doc 11 §3.5) |
| **Prohibited** | Regulated automated decisions without human review, etc. | Blocked with a policy citation |

**The tier's outputs become runtime policy automatically** — this is the join between the GRC product and the enforcement spine, and it is the reason both halves must share one identity/policy/evidence model.

## C3. Review routing
No self-approval (structurally enforced, doc 06 §5). Parallel reviews with per-reviewer SLAs, escalation on timeout, and full decision provenance. **Material change triggers re-review**: model swap, new tool, new data class, autonomy promotion, or divergence detected in Part B.

## C4. "Ask the Estate"
NL query over the registry + Action Records, answered from role-appropriate evidence with links to underlying records:
> "Which agents touch customer PII and haven't been reviewed in 90 days?"
> "Where is agent spend rising fastest this month?"
> "Who approved the invoice agent's write access, and what evidence did they see?"

Cheap to build on data we already have, extremely strong in a demo, and genuinely useful to a GRC lead who will never open a policy file. Guardrail: it answers **from records, with citations** — never generates an assessment of its own.

---

## D. Revised Roadmap Impact

Doc 13's phasing needs one change: **discovery + intake move into Phase 0**, because they are the low-friction entry that makes the enforcement spine sellable.

| Phase | Was | Now |
|---|---|---|
| **0** | Registry + runtime + gateway + ledger | **+ Estate discovery (telemetry/gateway/IdP) + intake & risk tiering + Ask-the-Estate** |
| **1** | Trust, durability, approvals | unchanged |
| **2** | Multi-agent, scale, BYOC | + blueprints, environment promotion |
| **3** | Depth | + generation (Part A) — **only if** the funnel model is chosen |

**Recommendation stands from doc 14 §5:** build the governance spine with discovery-first entry; treat Part A (generation) as optional and expensive. Discovery → intake → enforcement → evidence is a complete, coherent product on its own, and it is the half a small team can actually ship.
