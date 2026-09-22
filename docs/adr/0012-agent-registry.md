# ADR 0012 — The Agent Registry: what's real, what's simplified, and why

## Context

`docs/02-architecture.md` §2 lists the Agent Registry as a Control Plane
component: "Agent definitions, versions, owners, dependencies, autonomy
ceiling," backed by Postgres in the full platform design.
`docs/04-identity-authz.md` treats an Agent Definition as "Permanent
(versioned)" data the Agent Registry owns. `docs/09-governance-trust.md`
§6 goes further: every agent has an owning group, a business purpose, a
risk classification, a data-class inventory, a review date, and a
decommission plan, governed by onboarding review, periodic
recertification, decommissioning, and orphan detection.

This milestone needs the same governance question the Tool Registry
(Milestone M2) already answered for *what* is being called, applied to
*who* is calling: an agent identity absent from a declared catalog is
refused, not silently trusted because its credential happened to verify —
without building a Postgres-backed service, the full lifecycle model, or
any of doc 09 §6's governance rituals. This ADR records exactly which
parts are real now, which are deliberately narrower than the docs, and
why, following the same pattern ADRs 0008 and 0009 already used for the
Tool Registry and egress control.

## Decisions

### 1. `pkg/agentregistry` mirrors `pkg/toolregistry` almost exactly, on purpose

`Entry{AgentID, Version, Owner, Risk, DataClasses}`, `Registry.New`/`Lookup`
— the same shape, the same validation-at-construction discipline, the
same "empty means skip" convention for `Version` that
`pkg/toolregistry.Entry.Upstream` already established for its own optional
cross-check.

**Why:** the underlying question is identical — "is this identifier one a
human operator actually declared, or just one a credential happened to
assert?" — applied to a tool name in M2 and to an agent id here. Reusing
the exact shape means anyone who already understands the Tool Registry's
enforcement (and its ADR) already understands this one; there is no new
concept to learn, only a new join key.

### 2. Enforcement gates identity itself, before any tool-specific check

`internal/gateway.Gateway.agentDenyReason` runs immediately after identity
resolution, in both `ListTools` and `CallTool`, before the Tool Registry
or upstream-mismatch checks even look at which tool was requested. An
undeclared or wrong-version agent gets `ListTools` back empty (the same
treatment an unresolvable credential already got) and `CallTool` denied
with `agentregistry.undeclared` — a policy-shaped denial, not a protocol
error, matching `denyUndeclaredTool`'s exact reasoning: the credential
verified fine, the caller is simply ungoverned.

**Why gate here, not only in policy:** the same reasoning ADR 0008 used
for the Tool Registry — an OPA policy bundle *could* express "reject
unknown agent ids," but that makes it something every policy author must
remember to write, for every deployment, rather than a platform
invariant. Gating it in `internal/gateway` makes "no agent may act
without being registered" true regardless of what any particular policy
bundle says, the same way "no tool executes without being registered" is
true regardless of policy content.

**Consequence:** `internal/gateway.Gateway.New`'s `agentReg` parameter
must not be nil, exactly like `toolReg` — an empty registry is a valid,
if useless, configuration that denies every call (the fail-closed
default), not a silently-disabled check. Every existing `gateway.New`
call site in this repo (`cmd/helmdeep-gateway`, `test/e2e`,
`test/adversarial`, `internal/gateway`'s own tests) now constructs one;
test helpers that register "everything the upstream topology already
exposes" for the Tool Registry (`registerAllTools`) gained an exact
counterpart (`registerAllAgents`) for the same test-only reason: most of
those suites are testing something other than this gate.

### 3. `agent_version` is a new SIT claim, added the same way `scope` was in Milestone M2

ADR 0007 deliberately left `agent_version` out of the Session Identity
Token in Milestone M1: "nothing in this repo consumes them yet... add
each claim in the same change that gives it a reader." This milestone is
that claim's reader. `internal/devissuer.Issuer.IssueSIT` gained an
`agentVersion string` parameter; `pkg/identity.JWTResolver.Resolve`
extracts it into `types.Subject.AgentVersion` (a field that already
existed, unused, for exactly this reason — see that field's own doc
comment, which named this dependency explicitly before this milestone
existed).

**Consequence:** every `IssueSIT` call site in the repo (production and
test) gained a fifth argument — the same mechanical change ADR 0008
documented when `scope` was added, verified the same way: a real SIT was
minted with a real `agent_version` claim, signed, parsed, and resolved
into `Subject.AgentVersion` end to end
(`internal/devissuer/devissuer_test.go`'s `TestIssueSITRoundTrips`,
`pkg/identity/jwt_test.go`'s `TestJWTResolver_ValidTokenResolves`), and
verified again against a real running stack: `examples/dev-emulator`,
minting a SIT for an undeclared agent id, was refused with `"agent is not
registered"` by a live gateway process — not simulated.

### 4. Version pinning is optional per agent, and only checked when both sides assert one

`Registry.Entry.Version` may be empty (no pin — any asserted version, or
none, is accepted); `types.Subject.AgentVersion` may be empty (the caller
didn't assert one — every credential minted before this milestone, and
every static-token identity that doesn't opt in). The mismatch check only
fires when *both* are non-empty and disagree.

**Why:** the alternative — requiring every registered agent to also pin a
version, or every credential to assert one — would make this milestone a
breaking change for every existing deployment and test fixture, for a
property (build reproducibility across agent versions) nothing yet
depends on beyond doc 09 §6's own stated intent to catch it eventually. A
deployment that wants version pinning opts in by setting `Version` on the
registry entry and having its issuer populate `agent_version`; one that
doesn't, isn't affected at all.

### 5. `pkg/controlplane`'s remaining scope shrinks again, the same way it did for the Tool Registry

`pkg/controlplane`'s doc comment (and `ARCHITECTURE.md`'s "Repo layout
notes") already recorded that Tool Registry moved out of the Control
Plane stub in Milestone M2. This ADR is the second entry in that same
pattern: Agent Registry moves out too, leaving Model Catalog, Policy
Service, Trust Engine, Approval Service, Eval Service, and Tenant/Org
Service as the only Control Plane components still unimplemented. The
illustrative `Registry` interface `pkg/controlplane` used to carry
(`RegisterAgent(ctx, agentID, version string) error`) is removed outright
rather than kept as dead scaffolding — `pkg/agentregistry` is now the
real thing that interface was standing in for.

## What this milestone does not claim

- No Postgres or any persistent store — `Registry` is built once, from
  static config, at process startup, exactly like `pkg/toolregistry`.
- No owning-group/business-purpose/data-class-inventory governance beyond
  the `Owner`, `Risk`, and `DataClasses` fields carried as descriptive
  metadata — nothing in `internal/gateway` currently acts on any of the
  three.
- No onboarding review, periodic recertification, decommissioning, or
  orphan detection (doc 09 §6's four governance rituals) — none of that
  exists in any form; a "registered" agent stays registered forever,
  until an operator edits the config.
- No dependency tracking between agents (doc 02 §2's "dependencies") and
  no autonomy ceiling enforcement (doc 02 §2's "autonomy ceiling," doc
  09's L0–L3 trust levels) — `types.Subject.TrustLevel` remains unused by
  anything this registry adds.
- No cross-check between an agent's declared `Risk`/`DataClasses` and
  what any tool it calls actually touches — these are recorded, not
  compared against anything yet.

None of these are silently deferred; each is a real gap, tracked in
`STATUS.md`'s doc 02 row.
