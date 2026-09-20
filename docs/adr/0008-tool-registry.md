# ADR 0008 — Milestone M2's Tool Registry: what's real, what's simplified, and why

## Context

`docs/05-tool-gateway.md` §1 describes a full Tool Model: JSON Schema
input/output validation, a reversibility taxonomy (`reversible` /
`compensable` / `irreversible`) that drives runtime tier and approval
policy, split read/write data classes, egress destinations and payload
limits, rate/timeout/concurrency limits, and redaction fields — all
mandatory registration metadata, with one absolute rule: **"no tool
executes without a registered schema and reversibility classification.
Undeclared tools are how governance quietly dies; the platform refuses
them."**

Milestone M2 ("Phase 0" completion plan, per the working rules that
requested this ADR) needs the governance half of that rule — a real,
enforced Tool Registry an undeclared tool cannot get past — without
building schema validation, reversibility-driven runtime tiering, egress
control, or per-tool limits, none of which anything else in this repo
consumes yet. This ADR records exactly which parts are real now, which are
deliberately narrower than the doc, and why, per those working rules'
instruction to record doc deviations here rather than editing the doc
itself.

## Decisions

### 1. `pkg/toolregistry.Entry` carries five fields, not doc 05's full schema

Doc 05 §1's tool record has `version`, `owner`, `schema` (JSON Schema
input/output), `semantics` (reversibility, compensation, idempotency,
side_effect_scope), `risk` (rating, `data_classes_read`,
`data_classes_write`, `pii`), `access` (credential_mode, required_scopes),
`egress` (destinations, max_payload_bytes), `limits` (rate, timeout,
max_concurrent), and `observability` (redact_fields). `Entry` has: `ToolID`,
`Upstream`, `Risk` (rating only), `DataClasses` (one list, not
read/write-split), and `Scopes`.

**Why:** the same don't-add-fields-nothing-consumes-them principle
`docs/adr/0004-action-record-format.md` and `docs/adr/0007-credential-broker-scope.md`
already established. Nothing in this repo validates a JSON Schema against
tool arguments, drives a runtime tier from reversibility (there is one
runtime, not tiered), manages a compensation saga, enforces egress
destinations, or enforces per-tool rate/timeout/concurrency limits (the
gateway's own aggregate rate limiting, `internal/gateway/usage.go`, is
per-agent-per-tool from `examples/policies/limits.rego`, not sourced from
the registry). Read/write data classes are collapsed to one list because
no policy in this repo distinguishes them yet — see `Entry.DataClasses`'s
own doc comment.

**Consequence:** a config that tries to express doc 05's richer schema
(JSON Schema refs, reversibility, egress) has nowhere to put it yet. That's
a real gap, not a silent one — see "What this milestone does not claim"
below.

### 2. Undeclared tools are refused as a policy decision, not a protocol error

`internal/gateway.Gateway.CallTool` already had a protocol-error branch for
a tool name `mcp.Registry` can't route to at all (`-32602`, "unknown
tool"). Milestone M2 adds a second, distinct check: even when `mcp.Registry`
*can* route to a tool (a live upstream really exposes it), if
`pkg/toolregistry.Registry.Lookup` doesn't know it, the call is denied —
`isError: true`, an audit record with a known subject and a
`toolregistry.undeclared` policy ID — exactly like any other policy denial.
`ListTools` filters the same way: an undeclared tool never appears in
`tools/list`, matching how an out-of-scope tool was already invisible
before this tool existed.

**Why:** a tool "existing" at the MCP transport level and a tool being
*governed* are different facts. Doc 05's rule is about the second one — an
upstream (especially a compromised or misconfigured one) exposing a tool
over `tools/list` must never be enough, on its own, to make that tool
callable. Treating it as a protocol error would conflate "this doesn't
exist" with "this exists but isn't governed," and would deny a compromised
upstream the useful signal of "your existence is not disputed, your
governance is" — the same reasoning `docs/adr/0006-operational-failure-classification.md`
already used to distinguish a denial from a protocol-level rejection.

**Consequence:** the Tool Registry is required (`gateway.New`'s `toolReg`
parameter is not nil-able the way `broker` is) — an empty registry is a
valid configuration, and it denies every tool call, which is the correct
fail-closed default per `docs/adr/0003-fail-closed-behavior.md`, not a
silently-disabled check. `cmd/helmdeep-gateway`'s `tools:` config section
is what an operator populates to get any tool call to succeed at all — see
`examples/quickstart/config.yaml`.

### 3. `types.Subject` gains a `Scopes` field, and SITs gain a `scope` claim

Doc 04 §1.1's SIT already specifies a `scope` claim; ADR 0007 deliberately
left it out of Milestone M1 because nothing consumed it. Milestone M2's own
worked example — "a tool handling data class `pii` is denied to an agent
without the required scope" — is exactly the reader that claim was waiting
for: `input.action.required_scopes` (from the tool's registry entry) has
nothing to compare against without `input.subject.scopes` (from the
caller's own credential).

**Decision:** `internal/devissuer.Issuer.IssueSIT` takes a `scope []string`
parameter and signs it as the SIT's `scope` claim;
`pkg/identity.JWTResolver.Resolve` extracts it into `types.Subject.Scopes`,
mirroring the existing `delegation_chain` extraction pattern exactly
(`jwt.WithTypedClaim`, an optional `tok.Get`). `internal/gateway.StaticIdentity`
also gained a `Scopes` field, so a static-token deployment (or a test) can
exercise scope-gated policy without standing up JWT identity — the
pre-Milestone-M1 bootstrap resolver was always meant to be a strict subset
of what real identity can express, never a second, divergent shape.

**Why now, not earlier:** same "add each claim in the same change that
gives it a reader" principle ADR 0007 §1 already used to justify *leaving
it out* of M1. The reader now exists.

**Consequence:** every `IssueSIT` call site in the repo (production and
test) gained a fourth argument. `types.Subject.Scopes` and a tool's
`required_scopes` are deliberately separate fields on separate types (the
caller's grant vs. the tool's requirement) — a policy compares the two;
neither `pkg/types` nor `internal/gateway` does the comparison itself,
matching how every other policy-relevant field in this repo works (see
`Action.RequiredScopes`'s own doc comment).

### 4. A second, focused example policy bundle, not changes to the existing one

`examples/policies/` (scope/taint/rate-limit) is what the quickstart and
most of the e2e suite already exercise, and its own `scope.rego` comment
already says a real deployment would source scope from the Tool Registry
rather than hardcoding it — this milestone doesn't retrofit that bundle to
prove the comment right. Instead, `examples/policies-tool-registry/` is a
new, self-contained bundle demonstrating `input.action.risk`,
`input.action.data_classes`, and `input.action.required_scopes`: a `pii`
data class requires every one of the tool's required scopes
(`input.subject.scopes`), and a `high`/`critical` risk tool is allowed but
its result carries a `redact` obligation, standing in for the approval
workflow doc 05 §1.1 describes.

**Why:** changing the well-tested default bundle to demonstrate one more
milestone's feature risks the exact kind of regression
`docs/adr/0002-policy-engine-choice.md`'s "one rule producing a complete
decision object" design was meant to make easy to avoid — every existing
e2e assertion against `examples/policies/` would need re-verification for
no behavioral reason. A second bundle costs one more directory and proves
the same thing without that risk.

**A Rego subtlety worth recording:** the obvious `not s in
input.subject.scopes` (checking a required scope against a possibly-absent
`scopes` claim) is wrong — `s in <undefined>` is itself undefined, not
false, and negating an undefined membership check inside a comprehension
silently drops that candidate instead of counting it as missing. The
bundle uses `object.get(input.subject, "scopes", [])` to force a concrete
empty array first, so an agent with no granted scopes at all is correctly
treated as missing every required one, not exempted from the check
entirely. See `examples/policies-tool-registry/decision.rego`'s comment.

### 5. Test-only tool registries are built by declaring everything the upstream topology already exposes

`test/e2e/gateway_test.go`'s `registerAllTools` and
`test/adversarial/adversarial_test.go`'s equivalent inline helper both
build a `*toolregistry.Registry` from `mcp.Registry.Tools()` — i.e.,
whatever the mock upstream topology already declares — at `RiskLow`, no
data classes or scopes.

**Why:** almost all of those suites are testing something other than the
Tool Registry gate itself (scope policy, taint tracking, rate limits,
prompt-injection resistance, hash-chain integrity). Requiring every one of
those tests to also hand-declare a matching registry entry would be
churn with no test value, and — this is the important part — it is *only*
safe as test convenience: `pkg/toolregistry`'s own doc comment is explicit
that the registry is "the governed catalog of tools a deployment has
actually reviewed and declared, **as distinct from** whatever an upstream
MCP server happens to expose." Auto-deriving the registry from upstream
`tools/list` output is exactly the shortcut production code (and
`cmd/helmdeep-gateway`'s `tools:` config section) must never take — a
compromised upstream could declare its way into governance. Doing it in
test scaffolding, deliberately, for tests not about that property, is a
different and lower-stakes thing.

**Consequence:** the two dedicated tests that *are* about the gate itself —
`TestUndeclaredToolIsDeniedNotProtocolError` and
`TestUnregisteredToolNeverAppearsInToolsList`
(`internal/gateway/gateway_test.go`), plus the tool-registry example
bundle's own e2e tests (`test/e2e/toolregistry_test.go`) — build their
registries by hand, entry by entry, the way a real deployment's config
would.

## What this milestone does not claim

- No JSON Schema validation of tool call arguments (doc 05 §1's `schema`
  field) — a malformed argument reaches the upstream exactly as before this
  milestone; only risk/data-class/scope metadata is new.
- No reversibility taxonomy, runtime tiering, or compensation/saga
  machinery (doc 05 §1.1) — one runtime, no tiers, no rollback.
- No egress destination or payload-size enforcement (doc 05 §3) — that's
  Milestone M3.
- No per-tool rate/timeout/concurrency limits sourced from the registry
  (doc 05 §1's `limits` block) — the gateway's existing aggregate rate
  limiting is policy-configured, not registry-configured, and unchanged by
  this milestone.
- No `observability.redact_fields` sourced from the registry — obligations
  remain entirely policy-authored (`decision.obligations`), as they were
  before this milestone; the example bundle's redaction obligation is
  written by the policy author, not derived from a registry field.

None of these are silently deferred; each is a real gap, tracked in
`STATUS.md`'s doc 05 row.
