# Policy Guide

Policy is Rego, embedded via OPA (`docs/adr/0002-policy-engine-choice.md`).
This document is the input contract every policy bundle is evaluated
against, and a walkthrough of the worked example in `examples/policies/`.

## The contract: one rule, one input shape

The gateway compiles your bundle once (and again on every `SIGHUP`, see
`ROADMAP.md`) and queries exactly one rule for every `tools/call` and
`tools/list` visibility check:

```
data.helmdeep.authz.decision
```

Your bundle must define `decision` so that it always evaluates to a
complete object, never undefined — an undefined result is treated
identically to an explicit deny (`docs/adr/0003-fail-closed-behavior.md`),
so a rule that's merely silent on some input is a bug, not a safe default.
The idiomatic way to guarantee that is a `default` clause:

```rego
package helmdeep.authz

default decision := {
	"outcome": "deny",
	"policy_id": "default.deny",
	"reason": "no matching rule (fail closed)",
}
```

### `decision`'s shape

| Field | Type | Required | Notes |
|---|---|---|---|
| `outcome` | `"allow"` \| `"deny"` \| `"allow_with_obligations"` | yes | Anything else is treated as a deny. |
| `policy_id` | string | yes | Identifies which rule fired — this ends up in the audit trail, so make it something a reviewer can trace back to a line of Rego. |
| `reason` | string | no | Returned to the calling agent on a deny (as a tool execution error, so the model can see why) and recorded in the audit trail either way. |
| `obligations` | array of `{type, target}` | only with `allow_with_obligations` | `type` is currently only `"redact"`. `target` is a dot-separated path into the tool result — see `internal/gateway/obligations.go`. |

### `input`'s shape

`input` is `types.DecisionRequest` (`pkg/types/decision.go`) marshaled to
JSON. Field names below are the actual JSON tags — this is the real,
current contract, not a summary of it:

```jsonc
{
  "subject": {
    "id": "agent:finance-bot",       // the resolved caller identity
    "kind": "agent",
    "trust_level": 0,
    "delegation_chain": []           // always empty in this repo today — see pkg/identity
  },
  "action": {
    "type": "tool_call",
    "tool": "payments.wire_transfer",
    "arguments": {
      "account_number": {
        "data": "ACC-TRUSTED-0001",
        "provenance": { "source": "suppliermaster", "trusted": true }
      },
      "amount_usd": {
        "data": 500,
        "provenance": { "source": "unspecified", "trusted": false }
      }
    }
  },
  "context": {
    "session_id": "",
    "request_id": "req_...",
    "time": "2026-09-18T00:00:00Z",
    "usage": {
      "calls_in_window": 1,
      "window_seconds": 60,
      "cumulative_cost_usd": 0,      // always 0 in v1 — no cost model yet, see internal/gateway/usage.go
      "session_budget_usd": 0
    }
  }
}
```

Every argument arrives wrapped as `{data, provenance}`, never as a bare
value — this is deliberate and is exactly what makes the taint policy type
possible. See "How provenance actually gets populated" below before writing
a taint policy; it is not what most people guess on first read.

## The three policy types, worked

### Scope — `examples/policies/scope.rego`

A straightforward allowlist keyed by `input.subject.id` and
`input.action.tool`. This is the simplest of the three and reads exactly
like what it does.

### Provenance / taint — `examples/policies/taint.rego`

```rego
tainted_requirements := {"payments.wire_transfer": {"argument": "account_number"}}

taint_violation if {
	req := tainted_requirements[input.action.tool]
	arg := input.action.arguments[req.argument]
	not arg.provenance.trusted
}
```

**How provenance actually gets populated — read this before trusting it.**
The gateway does not ask the agent where a value came from (an agent
compromised by prompt injection would simply lie), and MCP gives the
gateway no visibility into the model's reasoning — only the final
`tools/call` arguments. So provenance in v1 comes from something the
gateway *did* observe directly: after an allowed call to an upstream
returns, the gateway records the top-level string values in that result,
tagged with the calling upstream's configured trust label
(`upstreams[].provenance` in `config.yaml`). If a later argument's value
exactly matches one of those recorded values, it inherits that upstream's
provenance. Anything the gateway has no record of — including every
argument on an agent's very first call — is untrusted by default. See
`internal/gateway/provenance.go` for the exact mechanism and its stated
scope limits (top-level string values only, a bounded per-subject cache,
15-minute TTL).

Practically: to see `payments.wire_transfer` succeed, an agent must first
call `supplier.get_account` (a trusted upstream) and pass the
`account_number` it got back, byte-for-byte, into the transfer call. Making
up a value, or copying one from `email.read_latest` (an untrusted upstream),
gets denied.

### Aggregate limits — `examples/policies/limits.rego`

```rego
rate_limits := {"kb.search": 3}

limit_exceeded if {
	limit := rate_limits[input.action.tool]
	input.context.usage.calls_in_window > limit
}
```

The gateway maintains `calls_in_window` itself (a rolling window per
subject, `internal/gateway/usage.go`) and increments it *before* asking for
a decision — a denied call still counts against the rate. Policy never
tracks its own state; it only compares a number the gateway computed
against a threshold the policy defines. This split is deliberate — see
`docs/adr/0002-policy-engine-choice.md`.

### Combining them — `examples/policies/decision.rego`

The three checks above are combined into `decision` with explicit
precedence: scope is checked first (an agent outside scope is denied for
that reason alone), then taint, then limits. See the file itself — it's
short enough to read directly rather than duplicate here.

## Testing a policy change

`opa test examples/policies/` runs OPA's own test framework against your
`.rego` files (write `_test.rego` files alongside them, per OPA's
convention — none are checked in yet, since the example bundle is small
enough that `pkg/policy/examples_test.go`'s Go-level scenarios cover it for
now). For a bundle of real size, `opa test` is the faster loop.
