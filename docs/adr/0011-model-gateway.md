# ADR 0011 — Milestone M5's Model Gateway: what's real, what's simplified, and why

## Context

`docs/07-model-gateway.md` describes a full Model Gateway: a normalized
request schema (purpose, typed message content, tool schemas, structured
output contracts, routing hints, tenant/residency context), a Model
Catalog-driven router (data-class and residency filters, quality/cost/
latency ranking, canary traffic splits), mandatory version pinning with
recorded fallback, a nine-step inbound/outbound safety pipeline (data-class
checks, PII detection, prompt-injection detection, structured-output
validation, content-safety classification, leak checks, budget admission),
and four caching layers (provider prompt cache, exact-match, semantic,
embedding).

Milestone M5 ("Phase 0" completion plan, per the working rules that
requested this ADR) needs "Model Gateway, 2 providers, on the same
ledger" — real calls to two real model providers, policy-gated and
audited exactly like the Tool Gateway — without building the Model
Catalog, the safety pipeline, or any caching layer, none of which
anything else in this repo needs yet. This ADR records exactly which
parts are real now, which are deliberately narrower than the doc, and
why.

## Decisions

### 1. Two real providers: Azure OpenAI and Anthropic

Both `pkg/modelgw.AzureOpenAIProvider` and `pkg/modelgw.AnthropicProvider`
make real HTTP calls to their real APIs — no mock/fake provider ships in
the package (fakes exist only in tests, via `httptest.NewServer`).
Verified directly, both providers: `AzureOpenAIProvider` was exercised
against a real Azure OpenAI resource (`helmdeep-openai`, resource group
`helmdeep-rg`, `gpt-4o-2024-11-20` deployed under the `Standard` SKU —
`GlobalStandard` had zero quota in this subscription).
`AnthropicProvider` was exercised against the real Anthropic Messages
API with a real `claude-haiku-4-5-20251001` call. See
`pkg/modelgw/integration_test.go` for the (opt-in, skipped by default —
decision 5) real-API tests for both.

One real-world wrinkle `AnthropicProvider` had to account for: an
API key that isn't scoped to a single workspace (an org-level key) gets
rejected outright — HTTP 400, "This API key is not scoped to a
workspace" — unless the request also carries an `anthropic-workspace-id`
header. `AnthropicProvider.WorkspaceID` is optional and empty by default
(a normal, workspace-scoped key needs no such header); it exists because
a real key this project tested against needed it, not speculatively.

**Why these two:** Azure OpenAI reuses the same Azure subscription and
resource group this project's AKS deployment already lives in — no new
account, no new billing relationship. Anthropic is the second, to prove
the abstraction is genuinely provider-agnostic rather than
Azure-OpenAI-shaped in disguise; it also has the most different request
shape of any mainstream provider (a top-level `system` field instead of a
`system`-role message, `max_tokens` mandatory with no server default),
which is exactly the kind of difference a two-provider minimum is
supposed to surface.

### 2. `Provider.Complete` reports the resolved model version from the response, never assumes the request

Both providers parse `model` out of the actual API response body for
`Response.Model`, rather than echoing back whatever `Route.Model` was
passed in. Confirmed this is a real distinction, not defensive
boilerplate: calling Azure OpenAI's `gpt-4o-chat` *deployment* returns
`"model": "gpt-4o-2024-11-20"` in the body — the deployment name and the
underlying model version are different strings by construction in
Azure's API.

**Why:** doc 07 §3.1's whole point is that "every call resolves to an
explicit version, recorded in the Action Record" — a provider or a
misconfigured deployment silently serving a different version than
requested must be visible in the ledger, not hidden by a provider type
that trusts its own request back.

### 3. Version pinning is enforced by `Route`/`New`, not left to convention

`modelgw.New` rejects any `Route` whose `Model` or `FallbackModel` is
`"latest"` (or empty) at construction — a misconfigured Model Gateway
fails at startup, matching `pkg/toolregistry.New`'s and
`pkg/policy.OPADecider.Load`'s own "fail loudly, not silently" convention
for this repo's registries and loaders.

**Consequence:** this only catches the literal string `"latest"`. A
provider's own alias scheme (Anthropic's `claude-*-latest` forms, if they
existed) isn't pattern-matched — the check is intentionally narrow rather
than trying to enumerate every provider's own aliasing convention.

### 4. Reuses `pkg/types.Action`/`pkg/policy.Decider`/`pkg/audit.Store` directly — "on the same ledger" is literal

A model call is `types.Action{Type: "model_call", Tool: <purpose>}`,
evaluated through the exact same `policy.Decider` interface the Tool
Gateway uses (an embedded OPA bundle can gate model calls with the same
Rego a policy author already knows), and written to the exact same
`audit.Store`. `pkg/types/decision.go`'s own doc comment already
promised this ("field names mirror the platform's broader Action Record
model... so this type can later cover model calls... without a breaking
rename") — Milestone M5 is that promise's first real reader.

**Proven directly**, not just asserted:
`test/e2e/modelgw_test.go`'s `TestToolCallsAndModelCallsShareOneVerifiableLedger`
drives one real `internal/gateway.Gateway` tool call and one
`modelgw.Gateway` model call into the *same* `*audit.FileStore` instance
and calls `Verify` — proving the hash chain accepts both record kinds
mixed together, not merely that each type marshals to JSON.

**Consequence:** `Gateway.Complete` writes two audit records for an
allowed call (the decision, then the outcome — mirroring
`internal/gateway.Gateway.CallTool`'s identical split, for the identical
reason: token usage and the actually-resolved model are only known after
the call completes) and follows the exact same fail-closed rule ADR 0003
established: an audit-write failure denies the call, before any provider
is ever reached.

### 5. Real-API tests are opt-in via environment variables, never wired into CI

`pkg/modelgw/integration_test.go`'s two tests skip themselves unless
`AZURE_OPENAI_ENDPOINT`/`AZURE_OPENAI_KEY`/`AZURE_OPENAI_DEPLOYMENT` or
`ANTHROPIC_API_KEY`/`ANTHROPIC_MODEL` are set. `.github/workflows/ci.yml`
sets none of them.

**Why:** every other "verify against the real artifact" test this
project has added (the AKS deployment, `sdk/python/tests/test_integration.py`'s
real Go binaries) costs nothing extra to run in CI. A real call to a
paid model API is different — every CI run would spend real money against
someone's API key, for a fake-server unit test suite that already proves
the same request/response handling deterministically. The real-API tests
exist for a human with real credentials to run locally when actually
changing provider-facing code, not for every push.

### 6. No HTTP or MCP-facing server — `pkg/modelgw` is a Go library, called in-process

Unlike the Tool Gateway (`cmd/helmdeep-gateway`, a real network listener),
there is no `cmd/` binary wrapping `modelgw.Gateway`.

**Why:** doc 07 doesn't specify a wire protocol for the Model Gateway the
way MCP already exists for tools, and nothing in this repo is an agent
runtime that would call one — `pkg/sandbox` remains an interface-only
stub. Building a server and a wire format now would be designing against
a caller that doesn't exist yet, the same reasoning ADR 0010 used to keep
the Python SDK to exactly the two operations `pkg/mcp.Handler` already
has real callers for.

**Consequence:** `sdk/python`'s `Client` has no model-calling method —
there is nothing on the wire yet for it to call.

## What this milestone does not claim

- No Model Catalog (approved-status tracking, deprecation dates, eval
  scorecards, canary/traffic-split rules) — `Route` is a static,
  operator-supplied config value, not a governed, versioned catalog
  entry.
- No routing by purpose/quality-floor/latency-class/data-class/residency/
  cost-budget-state/provider-health (doc 07 §3) — `Route` selection is a
  flat map keyed by `Purpose` string, with no ranking logic at all.
- No safety pipeline: no data-class egress check, no PII detection/
  redaction/tokenization, no untrusted-content wrapping, no
  prompt-injection detection, no structured-output validation or
  auto-repair, no content-safety classification, no leak check, no
  budget/quota admission (doc 07 §4's nine numbered steps — none of them
  exist here).
- No caching of any kind (doc 07 §5's four layers) — every `Complete`
  call reaches a real provider.
- No normalized `ModelRequest` schema — `Request` is messages plus
  `max_tokens`/`temperature`, not typed content parts, tool schemas, or a
  `response_format` contract.
- No HTTP/MCP server exposing this to a caller outside the same Go
  process (decision 6).

None of these are silently deferred; each is a real gap, tracked in
`STATUS.md`'s doc 07 row.
