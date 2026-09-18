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
- MCP listener over Streamable HTTP and stdio, targeting the current
  stateless spec (2026-07-28) with a legacy session-based compatibility
  shim (see `docs/adr/0005-mcp-protocol-compatibility.md`)
- Embedded OPA/Rego PDP, hot-reloadable from a YAML/Rego policy bundle
- File-backed, hash-chained audit store and a `verify-chain` command
- A minimal static token→identity resolver, local to `internal/gateway`
  (not `pkg/identity` — see `ARCHITECTURE.md`)
- Support for multiple upstream MCP servers behind one gateway endpoint
- Example policies covering scope, provenance/taint, and aggregate limits
- Benchmark demonstrating the sub-5ms added-latency target for a cached
  policy decision

## Phase 2 — Identity & credential exchange

Fills in `pkg/identity` for real: SPIFFE/SPIRE workload identity, OBO token
exchange, and a credential broker minting just-in-time scoped credentials
(`docs/04-identity-authz.md`). Replaces Phase 1's static token map in
`internal/gateway` with this package, behind the same `Resolver` interface —
the gateway's calling code should not need to change.

## Phase 3 — Model Gateway

Fills in `pkg/modelgw`: routing, version pinning, caching, quotas,
redaction, and provider fallback (`docs/07-model-gateway.md`). Only matters
once something in this repo makes model calls, which the Tool Gateway does
not.

## Phase 4 — Agent sandbox runtime

Fills in `pkg/sandbox`: isolated execution for agent code, per
`docs/03-agent-runtime.md`'s hybrid microVM / pooled-container model. This
is the component that would actually run agent code in this platform; the
Tool Gateway deliberately runs outside it.

## Phase 5 — Scheduler & durable orchestration

Fills in `pkg/scheduler`: placing sessions onto runtime capacity, durable
session lifecycle, multi-agent coordination (`docs/06-orchestration.md`).

## Phase 6 — Control plane

Fills in `pkg/controlplane`: agent registry, tool registry, model catalog,
tenant/org service (`docs/02-architecture.md` §2). Almost certainly where
multi-tenancy and any UI would eventually live — neither exists today.

## What's explicitly not planned here

Natural-language agent scaffolding, shadow-agent discovery, and a policy
authoring GUI are platform-level ideas in `docs/`, not commitments for this
repo. They'd only make sense once the phases above exist to generate
scaffolding against, discover agents on top of, or author policy for.
