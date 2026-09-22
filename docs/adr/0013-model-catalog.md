# ADR 0013 — The Model Catalog: what's real, what's simplified, and why

## Context

`docs/02-architecture.md` §2 lists the Model Catalog as a Control Plane
component: "Approved models, versions, pinning rules, per-model policy
(PII allowed? residency?)," backed by Postgres in the full platform
design. `docs/07-model-gateway.md` §3 makes it a routing hard constraint
— step 2 of `route(request)`: "Filter: models approved in the tenant's
Model Catalog" — and describes a rich entry: approved status, version
pinning rule, allowed data classes, residency, cost per token, quality
tier per purpose, deprecation date, and a link to the eval scorecard that
justified approval, with the absolute rule that "a model is not usable
until it has an eval scorecard for the purposes it is approved for."

Milestone M5 (`pkg/modelgw`) shipped without this: a `Route` maps a
purpose directly to a provider+model pair with no governance check
beyond "is this string `\"latest\"`" (ADR 0011, decision 3). This ADR adds
the governance question the Tool Registry and Agent Registry already
answer for tools and agent identities — "is this identifier one an
operator actually declared, or just one a config value asserts?" —
applied a third time, to the model a `Route` resolves to.

## Decisions

### 1. `pkg/modelcatalog` mirrors the two existing registries' shape and reasoning exactly

`Entry{Provider, Model, Approved, AllowedDataClasses, Residency}`,
`Registry.New`/`Lookup` — the same construction-time validation, the same
non-nil-required contract in the consuming `Gateway`. The join key is
`(Provider, Model)`, not `Model` alone, because two providers can
coincidentally use the same model string (verified directly:
`TestNew_AllowsSameModelNameAcrossDifferentProviders`).

**Why:** by this milestone, "is this identifier one an operator declared"
is a pattern this repo has already solved twice (ADRs 0008, 0012).
Reusing the exact shape means no new concept to learn, and the
enforcement code in `pkg/modelgw.Gateway.Complete` reads as the same
check as `internal/gateway.Gateway.CallTool`'s tool-registry gate, just
against a different registry.

### 2. `Approved` is an operator's direct assertion, not derived from an eval scorecard

Doc 07 §3's absolute rule — "not usable until it has an eval scorecard" —
is not implemented: `Entry.Approved` is a bare boolean an operator sets
in config. There is no Eval Service in this repo (still an unimplemented
Control Plane component — see `STATUS.md`'s doc 02 row) to derive it
from.

**Why not defer `Approved` entirely until Eval Service exists:** unlike a
field with no reader (which this repo's convention says to leave out),
`Approved` has a real, immediate reader today — the gate itself. The gap
is upstream of this field (what justifies setting it to `true`), not in
whether the field does anything. Recording that gap honestly here, rather
than not building the gate until the justification pipeline exists,
matches how `pkg/toolregistry.Entry.Risk` and `pkg/agentregistry.Entry.Risk`
are both also operator-asserted, not derived from any scoring system.

### 3. `AllowedDataClasses` and `Residency` are descriptive metadata only, not enforced

Doc 07 §3's routing filter wants "models permitted for these data_classes
& residency" as a *hard constraint* — checked against what a specific
request actually contains. `pkg/modelgw.Request` carries no data-class or
residency information about its own content, because nothing in this
repo classifies it: there is no PII/content-classification pipeline (ADR
0011's own list of gaps already named this), and doc 07 §4's inbound
safety pipeline (steps 1–5, including "data-class check" and PII
detection) is entirely unimplemented.

**Why carry the fields at all, then, if nothing is enforced against
them:** the same reasoning ADR 0008 used for `pkg/toolregistry.Entry.DataClasses`
(which has the identical status — descriptive, not enforced against
argument content). An operator populating a Model Catalog entry today
should be able to *record* "this model is approved for public data only"
even before the platform can check a given request against that claim —
the field documents intent for a human reader (and a future enforcement
point) without pretending to enforce something it can't yet verify. Doc
07 itself distinguishes the routing filter (hard constraint on the
*model*) from the safety pipeline (per-request content check) as two
different mechanisms; only the model-level one — "is this model approved
at all" — is what `Approved` implements.

### 4. The catalog check runs before policy, and — separately — before a fallback is actually taken

`Gateway.Complete` checks `catalog.Lookup(route.Provider, route.Model)`
immediately after resolving the route, before building the policy
decision request at all: the same ordering `internal/gateway.Gateway.CallTool`
uses for the Tool Registry (registry check before policy evaluation).
The fallback pair is checked separately, inside `call`, only when the
primary provider has actually failed and a fallback would actually be
used — not upfront for every call, which would deny requests over a
fallback path they never need. Verified directly:
`TestComplete_FallbackToAnUnapprovedModelFails` confirms an unapproved
fallback pair fails the call (not silently reached) without ever
touching the fallback provider.

**Consequence:** `modelgw.New`'s new `catalog` parameter must not be nil,
for the identical fail-closed reason `toolReg`/`agentReg` must not be —
an empty catalog is a valid, if useless, configuration that denies every
call. Every existing `modelgw.New` call site (`pkg/modelgw`'s own tests,
`test/e2e/modelgw_test.go`) now constructs one.

### 5. Verified against a real provider, not only against stubs

`pkg/modelgw`'s own tests use `stubProvider`, matching the fake-server
discipline already established for the two real `Provider`
implementations' own unit tests (ADR 0011). This ADR's gate was also
checked against the real, already-provisioned Azure OpenAI resource
(`helmdeep-openai` — see `reference_azure_aks_deploy.md`): a route
pointing at `gpt-4o-chat` with an empty catalog was denied
(`modelcatalog.unapproved`) without any HTTP request being made; the same
route with a catalog entry declaring it `Approved: true` reached the real
API and returned `model: "gpt-4o-2024-11-20"` — the API's own resolved
version, exactly as `AzureOpenAIProvider` was already proven to report in
ADR 0011.

## What this milestone does not claim

- No persistent store — `Registry` is built once, from static config, at
  process startup, exactly like the two prior registries.
- No eval-scorecard-derived approval, no deprecation dates, no quality
  tier per purpose, no cost-per-token tracking (doc 07 §3's remaining
  catalog fields) — none of this repo's code reads or produces any of
  them.
- No per-request data-class or residency enforcement (decision 3) — doc
  07 §4's inbound safety pipeline this would depend on does not exist.
- No routing *ranking* by cost/quality/latency/provider-health (doc 07
  §3's steps 3–4) — `Route` selection remains the flat, operator-authored
  map ADR 0011 already described; the catalog only answers "is this
  pair usable at all," never "which pair is best."
- No config-file wiring in `cmd/helmdeep-gateway` — `pkg/modelgw` remains
  a Go library with no server of its own (ADR 0011, decision 6), so there
  is no YAML schema for this catalog yet either; a caller constructs
  `modelcatalog.Registry` directly, the same way it constructs
  `modelgw.Gateway` itself.

None of these are silently deferred; each is a real gap, tracked in
`STATUS.md`'s doc 02 and doc 07 rows.
