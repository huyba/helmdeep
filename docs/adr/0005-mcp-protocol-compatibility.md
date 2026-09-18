# ADR 0005 — MCP protocol compatibility: support both the current and prior spec revisions

## Context

The MCP spec changed substantially seven weeks before this ADR was written.
The current finalized revision, **2026-07-28**, made the protocol
stateless: no `initialize`/`notifications/initialized` handshake, no
`Mcp-Session-Id`, no server-push GET stream — every request carries its own
protocol version and client identity in `_meta`
(`io.modelcontextprotocol/protocolVersion`, `io.modelcontextprotocol/clientInfo`,
`io.modelcontextprotocol/clientCapabilities`), and `tools/list` results may
now vary by the authorization presented on that specific request rather than
being fixed for the life of a connection. Servers must implement a new
`server/discover` RPC for capability advertisement. The prior revision,
**2025-11-25**, used the older session-based model most currently-deployed
MCP clients and agent frameworks still speak, seven weeks being nowhere near
enough time for the ecosystem to migrate.

This is a real product-shaping trade-off, not a detail: it determines
whether the gateway's `pkg/mcp.Listener` needs a session-state concept at
all, and whether agents built against today's popular MCP clients can talk
to it without modification — which is a hard requirement
("framework-agnostic... no agent-side SDK required, no code changes in the
agent").

## Decision

Target the current stateless spec (2026-07-28) as the primary wire format,
and implement a documented compatibility shim for the prior session-based
protocol (2025-11-25 and earlier) so agents that haven't upgraded yet still
work. `pkg/mcp.ProtocolVersionCurrent` and `pkg/mcp.ProtocolVersionLegacy`
name the two revisions the `Listener` implementation (Step 2) must
understand.

## Alternatives considered

- **Target only the current spec.** Smallest, most spec-correct surface
  area — no session state to manage, `tools/list` filtering by per-request
  credentials falls out of the stateless model almost for free, which fits
  this gateway's threat model unusually well (every request re-proves who's
  calling; there's no session to hijack or fixate). Rejected as the *only*
  target: as of today, most real agent clients in the wild still speak the
  session-based protocol, and rejecting them until they upgrade would mean
  the gateway can't do its job — sit transparently in front of an agent
  that speaks MCP — for the majority of agents that exist right now.
- **Target only the prior, currently-dominant protocol.** Maximizes
  compatibility with today's agents with less implementation work up front.
  Rejected as the *only* target: it means shipping against a spec revision
  that was already superseded before Step 2 was written, guaranteeing
  rework, and it forfeits the stateless model's genuine architectural fit
  for a policy gateway (per-request identity is *more* correct for this
  use case than a long-lived session identity would be).
- **Detect and negotiate per the spec's own documented backward-compatibility
  matrix, dynamically, per connection/request.** This is close to what "support
  both" means in practice and is not really a rejected alternative — it's
  the shape the compatibility shim takes. Called out separately because it's
  the part of this decision with the most implementation risk: detecting
  which era a given client speaks (presence of `_meta.io.modelcontextprotocol/*`
  fields vs. an `initialize` request; presence vs. absence of
  `Mcp-Session-Id`) has to be exactly right, or a legitimate current-spec
  client gets treated as legacy or vice versa. Step 2 must test both paths
  explicitly, not just the one path development happens to exercise first.

## Consequences

- `pkg/mcp.Listener`'s Step 2 implementation is two request-handling paths
  behind one interface, not one. This is more code than targeting a single
  revision, mirroring the same two-tier tradeoff the platform design already
  accepts elsewhere (see `docs/02-architecture.md` ADR-1, hybrid microVM +
  pooled container) in exchange for not forcing a compatibility cost onto
  every integrator.
- Because the current spec's statelessness maps naturally onto "resolve
  identity fresh on every request," the identity-resolution and
  policy-decision code paths in `internal/gateway` should be written
  session-agnostic from the start, with the legacy shim adapting *into*
  that model (extracting an identity from the legacy session at each
  request) rather than the reverse. Building the stateless path as primary
  and the legacy path as an adapter, rather than the other way around, keeps
  the code that will matter long-term from being contaminated by a
  transitional compatibility concern.
- This decision should be revisited once ecosystem adoption of 2026-07-28 is
  clear (SDK Tier 1 support, per the spec's own release process, was
  expected within ten weeks of the RC lock) — at that point the legacy shim
  becomes a maintenance cost with shrinking benefit, and dropping it is a
  deletion, not a redesign.
