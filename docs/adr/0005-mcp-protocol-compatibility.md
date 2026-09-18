# ADR 0005 — MCP protocol: target the current stateless spec only

## Context

The MCP spec went stateless in the finalized **2026-07-28** revision: no
`initialize`/`notifications/initialized` handshake, no `Mcp-Session-Id`,
every request carries its own protocol version, client info, and
capabilities in `_meta`. The prior revision, **2025-11-25** ("legacy" in the
spec's own terminology), used a session established by an `initialize`
handshake, and is what most currently-deployed MCP clients still speak.

An earlier draft of this ADR chose to support both eras. That was wrong, and
not for a code-volume reason.

## Decision

Target the current stateless spec (2026-07-28) only. Implement the minimal
legacy *tolerance* the spec itself prescribes for a modern-only server (see
below) — not a legacy protocol implementation.

**Exactly one identity path**: caller identity is derived from per-request
`_meta` (`io.modelcontextprotocol/protocolVersion`,
`io.modelcontextprotocol/clientCapabilities`) and the request's
`Authorization` header. Never from a session. This is a hard invariant,
tested explicitly (`internal/gateway/identity_test.go` asserts there is no
code path that resolves identity from anything but the current request).

## Why: this is a security argument, not a code-volume one

This gateway uses caller identity as an input to every policy decision.
"Support both eras" means two ways to resolve identity: one read fresh from
`_meta` on every request, one established once at a legacy `initialize`
handshake and then implicitly trusted for every subsequent request on that
session. Two identity-resolution paths inside a Policy Enforcement Point is
a classic source of confusion bugs — the two paths agreeing on the common
case and diverging on some edge case (a session outliving the credential
that created it, a session ID reused across a connection that changed
peers, a race between session teardown and an in-flight request) is exactly
the kind of gap an attacker looks for. In an ordinary API gateway, that
divergence is a nuisance. In a PEP, it's a bypass: an attacker only needs
the *cheaper* of the two identity paths to be wrong.

Secondary arguments, worth stating but not decisive on their own:

- This project has no users yet. A full legacy shim pays a real
  implementation and testing cost for compatibility with a hypothetical
  client, not a demonstrated one.
- Supporting both eras roughly doubles the wire-protocol surface, and the
  test plan would have to cover every deny path twice — once per era.

## What "minimal legacy tolerance" means, concretely

This is what the 2026-07-28 spec itself requires of a modern-only server —
not a design choice we're adding on top:

- No `Mcp-Session-Id` handling at all: if a legacy client sends the header,
  ignore it. Never mint or echo a session ID.
- HTTP `GET` or `DELETE` to the MCP endpoint → `405 Method Not Allowed`
  (there's no GET stream or session to delete in this revision).
- A legacy `initialize` request — which lacks the required
  `_meta.io.modelcontextprotocol/protocolVersion` field and the
  `MCP-Protocol-Version` / `Mcp-Method` headers this revision requires on
  every POST — fails header/`_meta` validation like any other malformed
  request (`400 Bad Request`, `HeaderMismatch` or Invalid Params as
  appropriate) rather than crashing or being silently accepted. See the
  spec's own compatibility matrix: "Legacy client, Modern server" is
  documented as a failure case, and the guidance for a modern-only server is
  to name its supported versions in the error so the legacy client's user
  gets an actionable message instead of a mystery.

None of this constitutes a second identity path. A legacy client simply
gets a clean, correctly-coded rejection instead of being served.

## Alternatives considered

- **Support both eras (the original decision).** Rejected for the security
  reason above.
- **Target only the legacy protocol, treat statelessness as future work.**
  Rejected: it means shipping against a spec revision already superseded,
  guaranteeing rework, and it forfeits the stateless model's genuine fit for
  a policy gateway — per-request identity resolution is architecturally
  *more* correct for this threat model than a session ever was, not merely
  newer.

## Also adopted, because we're on the current spec

- **`Mcp-Method` / `Mcp-Name` request headers are validated and used for
  routing before the body is parsed.** These mirror `method` and
  `params.name` into headers specifically so a gateway can route and filter
  cheaply — directly useful for the sub-5ms latency target (`ROADMAP.md`).
  The gateway validates header/body agreement per spec (`HeaderMismatch`,
  `-32020`) rather than trusting either source alone.
- **Server-minted state handles are ordinary tool arguments now**, not
  transport state. Because of this, a handle is just another `arguments`
  value and flows through `types.DecisionRequest` like any other parameter —
  including provenance labeling. A handle an agent got from a prior tool
  result carries that tool's provenance, not an implicit "trusted because
  it's transport state" exemption. This is called out explicitly because
  it's an easy thing to special-case by accident.

## Recorded as a non-goal

Full legacy (2025-11-25 and earlier) protocol support is a **deliberate
non-goal** of this project — see `ROADMAP.md`. It is revisited only if a
real user reports a real agent framework that cannot migrate. Context: the
spec's own feature-lifecycle policy gives deprecated features a minimum
twelve-month deprecation window, so the ecosystem has runway to move off the
session-based model before this becomes a practical problem for anyone.

## Consequences

- `pkg/mcp`'s implementation is one request-handling path, not two. Simpler
  than the original decision, and it removes an entire class of "which era
  is this client" detection logic that would otherwise need its own tests.
- The README must not imply the new spec makes anything *more secure*.
  Statelessness here is a scalability and implementation-simplicity change
  in the protocol; the security property that matters — identity resolved
  fresh, per request, from a source the gateway controls — was achievable
  under the old spec too (bearer tokens were always per-request). What
  changed is that the stateless model makes the *correct* design also the
  *only* design, removing the temptation to cache identity on a connection.
- A real user reporting a legacy-only framework is a product decision to
  revisit this ADR, not a bug report against this implementation.
