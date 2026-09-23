# Status: design docs vs. code

**This file is always the current status**, rewritten in place as things
change. For what the status *was* at some point in the past — and how much
was left then — see the dated snapshots in [`docs/progress/`](docs/progress/),
one file per report, never edited after the fact. Most recent:
[2026-09-23 09:29 PDT](docs/progress/2026-09-23-0929.md).

One row per document in `docs/00-OVERVIEW.md` through `docs/15-generation-discovery-intake.md`
— 16 files, numbered 00–15. (If "docs 01–16" in a milestone brief meant a
different range: this table covers all 16 numbered documents plus the two
unnumbered ones at the bottom, so nothing in `docs/` is left unclassified
either way.)

**Status legend:** Implemented · Partial · Interface-only · Not started · Out of scope by design · N/A (not an implementation doc)

| Doc | Title | Status | Package(s) |
|---|---|---|---|
| 00 | Overview & Master Index | N/A (index document) | — |
| 01 | Requirements & Use Cases | N/A (requirements spec) | — |
| 02 | System Architecture | Partial | `pkg/types`, `pkg/mcp`, `pkg/policy`, `pkg/audit`; the 4-plane split and Action Record model are real, but only the Data-Plane/Governance slice needed by the Tool Gateway exists. Control Plane §2 has four real components — Tool Registry (`pkg/toolregistry`, Milestone M2), Agent Registry (`pkg/agentregistry`), Model Catalog (`pkg/modelcatalog`), each enforcing "undeclared identifiers are refused" for a different join key, and Policy Service (`pkg/policyservice`), which implements only the "distribution of signed policy bundles" part of its §2 responsibility line: an Ed25519-signed manifest covering a bundle's complete file inventory, refused at load and at every reload if it doesn't match, with the verified bundle's version and digest stamped into every Action Record. Its authoring, compilation, and testing responsibilities, its Postgres/OCI stores, per-session bundle pinning, and key rotation do not exist — see `docs/adr/0014-policy-service.md`. Trust Engine, Approval Service, Eval Service, and Tenant/Org Service remain the one `pkg/controlplane` interface. Observability Plane is `pkg/audit` only (no trace collector, metrics/cost engine, or replay service). See `docs/adr/0012-agent-registry.md` and `docs/adr/0013-model-catalog.md`. |
| 03 | Agent Runtime & Isolation | Interface-only | `pkg/sandbox` |
| 04 | Identity & Authorization | Partial (Milestone M1, `scope` claim added in M2) | `pkg/identity.JWTResolver` verifies real, signed Session Identity Tokens (agent hop cryptographically verified; human hop asserted, not independently verified — no real IdP exists), including a `scope` claim (Milestone M2, once `pkg/toolregistry`'s required-scope comparisons gave it a reader); `pkg/identity.HTTPCredentialBroker` + `internal/devissuer` mint per-call, scoped, short-lived upstream credentials (doc 04 §3, self-contained since no upstream implements RFC 8693 itself). `internal/gateway.StaticTokenResolver` remains as a dev/test fallback, now gated behind `-dev-insecure`, and also carries `Scopes` for the same reason. Not done: SPIRE integration, revocation, RBAC/ABAC/ReBAC layering beyond existing policy, Enterprise IdP sync (doc 04 §6). See `docs/adr/0007-credential-broker-scope.md` and `docs/adr/0008-tool-registry.md` for the full list of what's simplified and why. |
| 05 | Tool Gateway & Connectors | Partial (Milestones M2, M3) | `internal/gateway`, `pkg/mcp`, `pkg/policy`, `pkg/audit`, `pkg/toolregistry`, `cmd/helmdeep-gateway`. Core request handling (MCP listener/client, PDP, hash-chained ledger, credential brokering, taint tracking, aggregate rate limits, obligations) is real. The Tool Registry (§1) enforces "no tool executes without being registered; undeclared tools are refused," both in `tools/call` and `tools/list`, with a narrow schema: risk rating, data classes, required scopes. Egress (§3, Milestone M3) enforces a per-tool upstream allowlist (`pkg/toolregistry.Entry.Upstream`, checked against `pkg/mcp.Registry`'s actual routing) and an absolute block on link-local addresses (cloud metadata services) at the TCP layer, DNS-rebinding-safe (`pkg/mcp/egress.go`). Not done: JSON Schema input/output validation, the reversibility taxonomy (§1.1), DNS pinning, TLS pinning, outbound DLP, per-session egress byte budget, registry-sourced per-tool limits (§1's `limits` block). See `docs/adr/0008-tool-registry.md` and `docs/adr/0009-egress-default-deny.md`. |
| 06 | Orchestration Engine | Interface-only | `pkg/scheduler` |
| 07 | Model Gateway | Partial (Milestones M5, Model Catalog) | `pkg/modelgw`, `pkg/modelcatalog`. Two real providers (Azure OpenAI, Anthropic), policy-gated via the same `pkg/policy.Decider` and audited to the same `pkg/audit.Store` the Tool Gateway uses. Explicit version pinning enforced at config time (`"latest"` rejected); fallback between providers is recorded in the ledger, never silent. A route's provider+model must be an approved Model Catalog entry or the call is refused (`modelcatalog.unapproved`) — verified against the real Azure OpenAI resource, not just stubs. Not done: eval-scorecard-derived approval, purpose/quality/cost/residency-based routing, per-request data-class/residency enforcement, the full safety pipeline (PII, prompt-injection detection, structured-output validation, content-safety, leak checks, budget admission), any caching layer, an HTTP/MCP-facing server. See `docs/adr/0011-model-gateway.md` and `docs/adr/0013-model-catalog.md`. |
| 08 | Memory & Knowledge | **Not started** | none — no package, not even a stub |
| 09 | Governance & Progressive Trust | Partial | `pkg/policy` + `internal/gateway/obligations.go` implement policy decisions and obligations (one piece of governance). §2.2's "compiled to signed bundles, versioned" is real: `pkg/policyservice` signs a bundle's complete file inventory and refuses one that doesn't match, at startup and at every reload, and §4's "policy bundles are versioned artifacts" is enforced at the ledger (every record names the bundle version + digest that decided it). Not done from §2.2: per-session bundle pinning, dry-run against historical Action Records, emergency force-invalidation of running sessions. Not done from §2.1: the five policy layers (platform/tenant/BU/agent/overlays, deny wins) — policy is still one flat bundle. Not done from §2.3: `require_approval`, `require_dual_control`, and `log_only` (shadow mode) as expressible outcomes — only allow/deny/allow_with_obligations exist. Trust Engine, autonomy levels (L0–L3), and Approval Service do not exist in any form. See `docs/adr/0014-policy-service.md`. |
| 10 | Observability & Evaluation | Partial | `pkg/audit` implements the Action Ledger piece (hash-chained, tamper-evident), and a record now identifies the policy bundle version + digest behind its decision, not just the rule id (`pkg/policyservice.StampingStore`); OTel tracing, the Eval Service, replay/counterfactual diffing, cost attribution, and drift detection do not exist |
| 11 | Security Architecture & Threat Model | Partial | No dedicated package — mitigations are spread across what already exists. Concretely, of the doc's 12 threats (T1–T12): **T3** (confused deputy) is substantially addressed — the agent never holds upstream credentials, the Gateway does (`internal/gateway`, `pkg/mcp/client.go`); **T11** (audit narrative manipulation) is addressed as designed — Action Records are written only by the platform, never by the agent (`pkg/audit`); **T1** (prompt injection) has its one decisive control partially present — taint-based policy exists (`internal/gateway/provenance.go`, `examples/policies/taint.rego`) but session-level taint, injection detection, and action-level verification do not; **T8** (resource exhaustion) has basic call-rate limiting (`internal/gateway/usage.go`) but not hierarchical budgets or cost-based limits; **T10** (insider misuse) has the audit trail and a `DelegationChain` field, but the chain is always empty (identity is a stub) and there's no anomaly detection. **T5** (supply chain) now has one of its listed mitigations — "signed artifacts + digest pinning" — for exactly one artifact class: policy bundles (`pkg/policyservice`, opt-in via `policy.signature`). Packages, models, and MCP server schemas are still unsigned and unpinned, so the threat itself is not addressed. **T2, T4, T6, T7, T9, T12 are not started.** See `SECURITY.md` for the plain-language version of this same gap list. |
| 12 | Platform Operations | **Not started** | none — single-tenant, single-process; no BYOC, no capacity/unit-cost model, no DR |
| 13 | Roadmap & Execution | N/A (planning doc) | see `ROADMAP.md` for this repo's own phasing, which supersedes this doc's phase numbering for anything actually in progress |
| 14 | Competitive Teardown: Sycamore | N/A (market analysis, not implementable) | — |
| 15 | Generation, Discovery & Intake | Out of scope by design | `docs/01-requirements.md` §6 lists natural-language agent generation (FR-B5) and shadow-agent discovery (FR-G7) as explicitly out of scope for v1 |

## Unnumbered docs

| Doc | Status | Notes |
|---|---|---|
| `docs/policy-guide.md` | Implemented | Describes the actual, current Rego input contract (`pkg/types/decision.go`'s JSON tags), the bundle in `examples/policies/`, and how to sign one (`policy keygen`/`sign`/`verify`) — this one is real documentation of shipped behavior, not a design doc. |
| `docs/threat-model.md` | **Not started** | Placeholder only ("content to be supplied"). This is meant to be the Tool-Gateway-*component*-level threat model, narrower than doc 11's platform-wide one — see its own text. Distinct from doc 11 above; do not conflate the two when closing this gap. |

## How to keep this current

Update the relevant row in the same PR that changes a status — a stale
status table is worse than none, because it's actively misleading rather
than just absent.

Editing a row here destroys the previous answer, which is the right
trade-off for a "what is true now" document and the wrong one for
answering "what did we think was true in September, and how much was
left?" That second question is what [`docs/progress/`](docs/progress/) is
for: a new dated file per report, never an edit to an old one. Write one
whenever it's worth being able to check back — after a milestone lands,
before a planning decision — and add its row to
[`docs/progress/README.md`](docs/progress/README.md) in the same commit.
