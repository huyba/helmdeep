# Status: design docs vs. code

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
| 02 | System Architecture | Partial | `pkg/types`, `pkg/mcp`, `pkg/policy`, `pkg/audit`; the 4-plane split and Action Record model are real, but only the Data-Plane/Governance slice needed by the Tool Gateway exists — Control Plane and Observability Plane are each one interface, not a plane |
| 03 | Agent Runtime & Isolation | Interface-only | `pkg/sandbox` |
| 04 | Identity & Authorization | Partial (Milestone M1) | `pkg/identity.JWTResolver` verifies real, signed Session Identity Tokens (agent hop cryptographically verified; human hop asserted, not independently verified — no real IdP exists); `pkg/identity.HTTPCredentialBroker` + `internal/devissuer` mint per-call, scoped, short-lived upstream credentials (doc 04 §3, self-contained since no upstream implements RFC 8693 itself). `internal/gateway.StaticTokenResolver` remains as a dev/test fallback, now gated behind `-dev-insecure`. Not done: SPIRE integration, revocation, RBAC/ABAC/ReBAC layering beyond existing policy, Enterprise IdP sync (doc 04 §6). See `docs/adr/0007-credential-broker-scope.md` for the full list of what's simplified and why. |
| 05 | Tool Gateway & Connectors | **Implemented** | `internal/gateway`, `pkg/mcp`, `pkg/policy`, `pkg/audit`, `cmd/helmdeep-gateway` |
| 06 | Orchestration Engine | Interface-only | `pkg/scheduler` |
| 07 | Model Gateway | Interface-only | `pkg/modelgw` |
| 08 | Memory & Knowledge | **Not started** | none — no package, not even a stub |
| 09 | Governance & Progressive Trust | Partial | `pkg/policy` + `internal/gateway/obligations.go` implement policy decisions and obligations (one piece of governance); Trust Engine, autonomy levels (L0–L3), and Approval Service do not exist in any form |
| 10 | Observability & Evaluation | Partial | `pkg/audit` implements the Action Ledger piece (hash-chained, tamper-evident); OTel tracing, the Eval Service, replay/counterfactual diffing, cost attribution, and drift detection do not exist |
| 11 | Security Architecture & Threat Model | Partial | No dedicated package — mitigations are spread across what already exists. Concretely, of the doc's 12 threats (T1–T12): **T3** (confused deputy) is substantially addressed — the agent never holds upstream credentials, the Gateway does (`internal/gateway`, `pkg/mcp/client.go`); **T11** (audit narrative manipulation) is addressed as designed — Action Records are written only by the platform, never by the agent (`pkg/audit`); **T1** (prompt injection) has its one decisive control partially present — taint-based policy exists (`internal/gateway/provenance.go`, `examples/policies/taint.rego`) but session-level taint, injection detection, and action-level verification do not; **T8** (resource exhaustion) has basic call-rate limiting (`internal/gateway/usage.go`) but not hierarchical budgets or cost-based limits; **T10** (insider misuse) has the audit trail and a `DelegationChain` field, but the chain is always empty (identity is a stub) and there's no anomaly detection. **T2, T4, T5, T6, T7, T9, T12 are not started.** See `SECURITY.md` for the plain-language version of this same gap list. |
| 12 | Platform Operations | **Not started** | none — single-tenant, single-process; no BYOC, no capacity/unit-cost model, no DR |
| 13 | Roadmap & Execution | N/A (planning doc) | see `ROADMAP.md` for this repo's own phasing, which supersedes this doc's phase numbering for anything actually in progress |
| 14 | Competitive Teardown: Sycamore | N/A (market analysis, not implementable) | — |
| 15 | Generation, Discovery & Intake | Out of scope by design | `docs/01-requirements.md` §6 lists natural-language agent generation (FR-B5) and shadow-agent discovery (FR-G7) as explicitly out of scope for v1 |

## Unnumbered docs

| Doc | Status | Notes |
|---|---|---|
| `docs/policy-guide.md` | Implemented | Describes the actual, current Rego input contract (`pkg/types/decision.go`'s JSON tags) and the bundle in `examples/policies/` — this one is real documentation of shipped behavior, not a design doc. |
| `docs/threat-model.md` | **Not started** | Placeholder only ("content to be supplied"). This is meant to be the Tool-Gateway-*component*-level threat model, narrower than doc 11's platform-wide one — see its own text. Distinct from doc 11 above; do not conflate the two when closing this gap. |

## How to keep this current

Update the relevant row in the same PR that changes a status — a stale
status table is worse than none, because it's actively misleading rather
than just absent.
