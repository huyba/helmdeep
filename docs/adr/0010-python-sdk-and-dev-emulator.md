# ADR 0010 — Milestone M4's Python SDK and dev emulator: what's real, what's simplified, and why

## Context

`docs/01-requirements.md` FR-B2 wants an SDK in Python *and* TypeScript,
framework-agnostic, for writing agent logic. FR-B3 wants a local emulator
"running the same runtime contract as production (tools mocked or proxied
through the real gateway with dev credentials)," and doc 00's persona
table names the exit bar for it directly: "Ana — Agent Developer... SDK,
local emulator, fast deploy, traces, evals... Time-to-first-production-agent."

Milestone M4 ("Phase 0" completion plan, per the working rules that
requested this ADR) needs a real Python client an agent author can
actually import, and a real local stack that gives it genuine identity and
genuine enforcement to develop against — without building a TypeScript
SDK, a higher-level "agent" runtime abstraction, or framework adapters
(LangGraph/OpenAI Agents SDK/CrewAI, FR-B4, explicitly P1/out of this
milestone). This ADR records exactly which parts are real now, which are
deliberately narrower than the docs, and why.

## Decisions

### 1. Python only; no TypeScript SDK

FR-B2 asks for both. `sdk/python/` exists; no `sdk/typescript/` does.

**Why:** the same don't-build-the-second-thing-until-something-needs-it
principle every prior ADR in this repo has used. Nothing in this repo or
its examples is written in TypeScript, and a second language SDK doubles
the wire-protocol surface to keep in sync with `pkg/mcp.Server` for zero
current benefit. If a real TypeScript-writing agent author shows up, that
SDK gets built against a proven, already-tested wire contract — the
Python SDK's own tests (`sdk/python/tests/test_client.py`) are exactly
the header/body shape a TypeScript client would also need to get right.

### 2. The SDK is a correct wire-protocol client, not an "agent framework"

`helmdeep.Client` has exactly two operations — `list_tools` and
`call_tool` (plus `call`, a thin convenience wrapper that raises instead
of returning a flag) — matching `pkg/mcp.Handler`'s own two methods
one-for-one. No retries, no connection pooling beyond what
`urllib.request` already does, no SSE/streaming response handling (matching
`pkg/mcp.HTTPUpstream`, which doesn't support SSE upstreams either — see
its own doc comment), no memory, no model-calling, no orchestration.

**Why:** none of that exists anywhere else in this repo to integrate
with (`pkg/modelgw` and a memory service are both stubs), so building
agent-framework features into the SDK now would be designing against
guesses, not a real consumer. FR-B2's own phrase — "framework-agnostic
host interface" — is a reason *not* to build a framework here: an
opinionated agent loop is precisely what an agent author brings, or
imports from LangGraph/CrewAI/etc. (FR-B4, not this milestone). This SDK's
job is only to make a spec-correct tool call, exactly like a hand-rolled
HTTP client would, so nothing it does can silently diverge from
`pkg/mcp.Server`'s actual contract.

**Consequence:** `Client._request` builds the same headers
(`MCP-Protocol-Version`, `Mcp-Method`, `Mcp-Name`) and `_meta` fields the
Go server's `validateStandardHeaders` requires (see ADR 0005) by hand,
every call — there is no MCP client library dependency doing this for us,
because none of the general-purpose ones speak this project's exact
2026-07-28 stateless dialect yet.

### 3. Standard library only, zero third-party runtime dependencies

`urllib.request`/`json`/`dataclasses`, nothing else, for the SDK itself
(`pytest` is a *test*-only dependency, declared under
`[project.optional-dependencies].test`, never installed for someone who
just wants to call a tool).

**Why:** the same reasoning ADR 0001 already used for the Go module
(minimal deps, each justified) applied to Python: an agent author pulling
in this SDK to make one kind of HTTP call should not also inherit
`requests`'s own dependency tree and version-pinning obligations for
functionality `urllib` already provides correctly.

### 4. `cmd/dev-issuer` wraps `internal/devissuer`, unchanged internally

ADR 0007 and `ARCHITECTURE.md` both named this "Milestone M4's job" when
M1 built the library — `internal/devissuer.Issuer`'s HTTP server
(`NewServer`) had no standalone binary to run it. `cmd/dev-issuer` is a
thin wrapper: parse flags, construct the same `Issuer`/`Server` M1 already
built and tested, run it, handle `SIGINT`/`SIGTERM`. No new identity
logic — the "DEVELOPMENT AND TEST ONLY" caveats in ADR 0007 (fresh,
unpersisted signing key every restart; the human hop in
`delegation_chain` asserted, not verified) apply exactly as before,
restated in the new binary's own doc comment so they travel with it.

### 5. A second example (`examples/dev-emulator/`), not a change to `examples/quickstart/`

`examples/quickstart/` uses `identity.static_tokens` + `-dev-insecure`,
documented in three places (its own README, `deploy/k8s/README.md`,
`docs/adr/0007-credential-broker-scope.md`) as a deliberate placeholder.
Milestone M4 does not retrofit it to JWT identity; `examples/dev-emulator/`
is a new, otherwise-identical docker-compose stack (same policies, same
mock upstreams, same Tool Registry entries) with `identity.jwt` against a
`dev-issuer` service instead.

**Why:** the two examples serve different readers. Quickstart's whole
point is "see the entire system work with zero setup" — anyone evaluating
the project, no credential-minting step required. Doc 01's own framing of
the emulator is different: FR-B3's "her own delegated identity" describes
an individual agent developer's daily loop, where each person (or each
of an SDK's automated tests) mints their own Session Identity Token via
`POST /issue` rather than sharing one of a few hardcoded tokens. Collapsing
these into one example would make quickstart slower to try (a
credential-minting step before the first successful call) for a benefit
irrelevant to that reader.

**Consequence:** two `docker-compose.yml` files with real, if small,
duplication (four mock upstream services, the gateway service). Accepted
rather than introducing a shared compose fragment or Makefile-generated
config for two files this size — see `examples/dev-emulator/README.md`'s
"What's different" section, which exists specifically so the duplication
stays visible and easy to keep in sync by hand.

### 6. The integration test suite builds and runs the real Go binaries

`sdk/python/tests/test_integration.py` calls `go build` on
`cmd/helmdeep-gateway`, `cmd/dev-issuer`, and `cmd/mock-upstream`, starts
all three as real subprocesses, and drives `helmdeep.Client` against them
— real JWT identity, real OPA policy evaluation
(`examples/policies/scope.rego`), a real upstream MCP server, including
one call to a tool that upstream genuinely exposes but the Tool Registry
has never declared (Milestone M2's refusal, exercised from a completely
different language's client for the first time).

**Why:** `test_client.py`'s fake-gateway unit tests prove the SDK sends
the request shape *this codebase's authors* believe the wire protocol
requires; they cannot catch the SDK and the real server silently
disagreeing about something neither side's tests happen to exercise. This
project's own established practice — verify against the real artifact,
not only against a model of it (the Docker builds, the AKS deployment, and
`internal/gateway`'s own e2e suite all do this) — applies here too, and a
Python client is exactly the kind of consumer most likely to reveal an
assumption baked into the Go-only test suite.

**Consequence:** `.github/workflows/ci.yml`'s new `python-sdk` job needs
both the Go toolchain and Python — `test_integration.py` is skipped, not
failed, if `go` isn't on `PATH`, so `test_client.py`'s fast unit tests
still run in a Python-only environment.

## What this milestone does not claim

- No TypeScript SDK (FR-B2's other half).
- No framework adapters for LangGraph/OpenAI Agents SDK/CrewAI (FR-B4,
  explicitly P1).
- No `agent.yaml` declarative agent definition, Agent Registry, or
  versioned/digested agent artifacts (FR-B1, FR-B6) — nothing in this
  repo defines what an "agent," as an artifact, is yet; the SDK only
  calls tools *for* whatever code an author writes around it.
- No traces or eval suites for agent-side development (doc 00's
  "traces, evals" for the Ana persona) — `pkg/audit`'s ledger is
  gateway-side and already exists (Milestone M0/M1), but nothing surfaces
  it back to an agent developer's local loop.
- `cmd/dev-issuer` adds no new identity capability beyond what
  `internal/devissuer` already had — no persistence, no key rotation, no
  real IdP integration. Every gap ADR 0007 already named remains exactly
  as open.

None of these are silently deferred; each is a real gap, tracked in
`STATUS.md`.
