# ADR 0003 — Fail closed on every PDP or audit failure

## Context

The Tool Gateway sits between an agent and real systems specifically because
neither the model nor the agent's code can be trusted to self-limit. That
premise only holds if the gateway itself degrades toward *less* access, not
more, whenever something goes wrong. The two failure modes that matter most:
the PDP (`pkg/policy.Decider`) is unreachable or returns an error, and the
audit store (`pkg/audit.Recorder`) cannot durably write a record.

## Decision

Any error from `Decide` — timeout, panic recovered at the boundary,
malformed response, anything — is treated identically to an explicit `deny`.
The gateway never proceeds with a tool call it could not get a decision for.

Separately: for actions the gateway cannot cheaply undo, the action record
must be durably appended *before* the upstream call is issued. If the append
itself fails, that's also a deny — an action we can't prove we authorized is
not one we let through.

## Alternatives considered

- **Fail open on PDP timeout, to protect latency/availability.** This is the
  standard argument for degrading gracefully under load. Rejected: the
  entire value proposition of this gateway is that it's a boundary the agent
  cannot route around. A fail-open mode is a documented, discoverable way to
  route around it — anyone who can cause the PDP to time out (a resource
  exhaustion attack is not exotic) gets unauthorized access as a side
  effect. "Available but sometimes doesn't enforce policy" is not a
  weaker version of this product; it's a different, worse product.
- **Fail open for read-only tool calls, fail closed for writes.** More
  nuanced, and arguably defensible — `docs/02-architecture.md`'s async vs.
  sync record-write split already draws exactly this reversible/irreversible
  line for a different tradeoff (latency of the audit write, not whether to
  enforce at all). We didn't adopt it here because "read-only" is a property
  the *tool* asserts about itself (via MCP tool annotations), and the spec
  is explicit that annotations are untrusted unless they come from a server
  we trust — see `docs/05-tool-gateway.md` and the MCP tools spec's warning
  that annotations must be treated as untrusted metadata. Trusting a
  self-reported "this is safe to allow through on faith" flag is exactly the
  kind of ambient trust this project exists to remove. Revisit only once
  read/write classification comes from the Tool Registry's own risk rating,
  not from the tool's own metadata.
- **Retry with backoff before deciding.** Reasonable as an addition, not an
  alternative — a bounded retry can happen before the fail-closed fallback
  kicks in, but the *fallback* is still deny, not allow.

## Consequences

- A PDP outage is an availability incident for the whole gateway, by design.
  This means the PDP's own availability and the sub-5ms decision latency
  budget (`ROADMAP.md`) are load-bearing for the product being usable at
  all, not just for user experience — there's no soft-degradation mode to
  hide behind.
- Every `tools/call` path has exactly one line where "did we get a real
  decision back" is checked, and the failure branch of that check is
  identical to the deny branch. There is no separate "policy unavailable"
  response type an integrator could accidentally treat as success.
- This is a testable invariant: Step 2's test suite must include a case that
  kills or errors the PDP mid-request and asserts the call is denied, not
  just "the request failed."
