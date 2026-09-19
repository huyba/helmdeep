# ADR 0006 — Classifying operational failures inside `CallTool`

## Context

Two failure conditions inside `internal/gateway.Gateway.CallTool` don't fit
neatly into "allow" or "deny" the way a policy decision does, and each
needed an explicit choice: what an upstream MCP server being unreachable
looks like to the calling agent, and what happens when the audit log itself
can't be written.

## Decision 1 — Upstream call failures are tool execution errors, not protocol errors

When policy allows a call but the upstream MCP server then fails (network
error, non-2xx, malformed response), the gateway returns a normal
`types.ToolResult` with `IsError: true` and a text explanation — the same
shape a policy denial uses — rather than a JSON-RPC protocol error
(`-32603`).

**Alternatives considered:** returning a JSON-RPC protocol error, which is
what an earlier version of this code did. Protocol errors are for requests
malformed at the JSON-RPC/MCP level (bad params, unknown method); an
upstream timeout is neither — the request was well-formed and authorized,
and the failure is environmental. The MCP spec's own distinction (protocol
errors vs. tool execution errors) exists specifically so a calling model
can tell "your request doesn't make sense" apart from "your request was
fine but didn't work this time," and can retry or adapt in the second case.
Classifying an upstream failure as a protocol error told the model the
wrong one of those two things.

**Consequences:** a denial and an upstream failure now look the same shape
to the agent (`isError: true`, explanatory text) and are distinguished only
by the text and, for anyone inspecting the audit trail, by
`RecordOutcome` (`denied` vs. `failed`). An integrator parsing only
`isError` cannot tell a policy denial from an infrastructure blip without
reading the text — acceptable, since both cases call for the same
immediate action from the agent (stop, don't retry blindly) and the
distinction matters more to a human investigating later than to the model
in the moment.

## Decision 2 — Audit-write failure denies

If `auditLog.Append` fails for the decision record — for whatever reason,
disk full, permission error, anything — `CallTool` returns a denial and
does not proceed to the upstream call, regardless of what the PDP decided.

**Alternatives considered:** proceeding anyway and logging the audit
failure out-of-band (e.g. to stderr only). Rejected: this is a policy
enforcement gateway whose entire value proposition is "every action is
authorized *and recorded*." An action the gateway cannot prove it recorded
is functionally identical, from an auditor's or incident responder's
perspective, to an action that bypassed the gateway entirely — see
`SECURITY.md`'s statement of what this project defends against. Consistent
with `docs/adr/0003-fail-closed-behavior.md`: an unauditable action is not
an acceptable one, so the failure mode already established for the PDP
(any error is a deny) extends to the audit write too, not just the
decision itself.

**Consequences:** an audit storage outage becomes a full gateway outage for
write-requiring calls, by design — the same tradeoff Decision-3 in ADR 0003
makes for the PDP. This is tested directly (`TestAuditAppendFailureDenies...`
in `internal/gateway/gateway_test.go`): a fake `audit.Store` that always
errors on `Append` must produce a denial and never reach the upstream.
