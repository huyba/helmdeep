# Roadmap

Phased by component, not by date — this is a solo-maintainer project and
dates would be fiction. Each phase's exit criterion is a thing that works,
not a thing that's started.

See `docs/13-roadmap.md` for the full platform's roadmap and team-shape
assumptions; this file is scoped to what actually gets built in *this*
repo, starting from where it is today.

## Phase 1 — Tool Gateway (current)

The only phase with real implementation detail, because it's the only one
approved to build. Turns the interfaces in `pkg/mcp`, `pkg/policy`, and
`pkg/audit` into working code, and `internal/gateway` from a package comment
into the thing that wires them together.

Exit criterion: an agent, unmodified, can point at `helmdeep-gateway` as its
MCP server, call a tool that policy allows, get denied on a tool policy
forbids, and a stranger can run `verify-chain` on the resulting audit log
and see that it's intact.

Scope:
- MCP listener over Streamable HTTP, targeting the current stateless spec
  (2026-07-28) exclusively, with only the minimal legacy tolerance the spec
  itself prescribes for a modern-only server (see
  `docs/adr/0005-mcp-protocol-compatibility.md`). stdio transport is not
  implemented in Phase 1.
- Embedded OPA/Rego PDP, hot-reloadable from a YAML/Rego policy bundle
- File-backed, hash-chained audit store and a `verify-chain` command
- A minimal static token→identity resolver, local to `internal/gateway`
  (not `pkg/identity` — see `ARCHITECTURE.md`)
- Support for multiple upstream MCP servers behind one gateway endpoint
- Example policies covering scope, provenance/taint, and aggregate limits
- Benchmark demonstrating the sub-5ms added-latency target for a cached
  policy decision

## Phase 2 — Identity & credential exchange

**Partially delivered (Milestone M1).** `pkg/identity.JWTResolver` verifies
signed Session Identity Tokens; `pkg/identity.HTTPCredentialBroker` mints
per-call scoped credentials via `internal/devissuer`'s self-contained
token exchange. Exactly the "behind the same `Resolver` interface, the
gateway's calling code should not need to change" property this section
originally called for held — see `docs/adr/0007-credential-broker-scope.md`.

Still open: real SPIFFE/SPIRE workload identity (the current
`JWTResolver` is SPIFFE-*compatible*, not SPIFFE-*backed*), revocation
(`docs/04-identity-authz.md` §5's bloom filter and <5s propagation target),
and Enterprise IdP integration (§6) — the human hop in a delegation chain
is asserted by the dev issuer today, not independently verified.

**Egress default-deny partially delivered ahead of full Phase 6 scope
(Milestone M3).** A per-tool upstream allowlist and an absolute,
DNS-rebinding-safe block on cloud-metadata (link-local) addresses are
enforced in `internal/gateway` and `pkg/mcp` — see
`docs/adr/0009-egress-default-deny.md` for what doc 05 §3 still describes
that this does not implement (DNS/TLS pinning, DLP, egress byte budget).

**Python SDK and dev emulator partially delivered (Milestone M4).**
`sdk/python` is a minimal, dependency-free MCP client (docs/01-requirements.md
FR-B2, Python only — no TypeScript SDK); `cmd/dev-issuer` and
`examples/dev-emulator/` are the "local emulator running the same runtime
contract as production" FR-B3 asks for. See
`docs/adr/0010-python-sdk-and-dev-emulator.md` for what's not implemented
(TypeScript, framework adapters, agent artifacts, traces/evals).

## Phase 3 — Model Gateway

Fills in `pkg/modelgw`: routing, version pinning, caching, quotas,
redaction, and provider fallback (`docs/07-model-gateway.md`). Only matters
once something in this repo makes model calls, which the Tool Gateway does
not.

**Model Gateway partially delivered ahead of full Phase 3 scope
(Milestone M5).** Two real providers (Azure OpenAI, Anthropic),
policy-gated and audited on the same ledger the Tool Gateway writes to.
See `docs/adr/0011-model-gateway.md` for what doc 07 still describes that
this does not implement (Model Catalog, routing policy, safety pipeline,
caching).

## Phase 4 — Agent sandbox runtime

Fills in `pkg/sandbox`: isolated execution for agent code, per
`docs/03-agent-runtime.md`'s hybrid microVM / pooled-container model. This
is the component that would actually run agent code in this platform; the
Tool Gateway deliberately runs outside it.

## Phase 5 — Scheduler & durable orchestration

Fills in `pkg/scheduler`: placing sessions onto runtime capacity, durable
session lifecycle, multi-agent coordination (`docs/06-orchestration.md`).

## Phase 6 — Control plane

Fills in `pkg/controlplane`: agent registry, model catalog, tenant/org
service (`docs/02-architecture.md` §2). Almost certainly where
multi-tenancy and any UI would eventually live — neither exists today.

**Tool registry partially delivered ahead of this phase (Milestone M2).**
`pkg/toolregistry` implements the governance half of
`docs/05-tool-gateway.md` §1's tool model — risk rating, data classes,
required scopes, and the "undeclared tools are refused" rule enforced in
`internal/gateway` — as its own package rather than living inside the
`pkg/controlplane` stub, because it got a real implementation before any
other control-plane service did. See `docs/adr/0008-tool-registry.md` for
what's still missing (JSON Schema validation, reversibility taxonomy,
egress control, registry-sourced limits).

**Agent registry partially delivered ahead of this phase.**
`pkg/agentregistry` implements the same "undeclared identities are
refused" rule one level up, applied to *who* is calling rather than
*what* is being called — an agent id absent from it is refused, and an
optional per-agent version pin denies a calling instance asserting a
different one. See `docs/adr/0012-agent-registry.md` for what's still
missing (no persistent store, no owning-group/lifecycle governance from
`docs/09-governance-trust.md` §6, no autonomy-ceiling enforcement).

## What's explicitly not planned here

Natural-language agent scaffolding, shadow-agent discovery, and a policy
authoring GUI are platform-level ideas in `docs/`, not commitments for this
repo. They'd only make sense once the phases above exist to generate
scaffolding against, discover agents on top of, or author policy for.

**Full legacy MCP protocol support** (2025-11-25 and earlier — the
`initialize`-handshake, session-based era) is also a deliberate non-goal,
not a gap to fill in a later phase by default. See
`docs/adr/0005-mcp-protocol-compatibility.md`: supporting both eras means
two paths for resolving caller identity inside the one component whose job
is to get that exactly right, and this project has no user yet who's
actually blocked by it. Revisit only if a real user reports a real agent
framework that cannot migrate — the spec gives deprecated features a
twelve-month runway, so there's no urgency to pre-build this.
