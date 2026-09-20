# Example policy — Tool Registry risk & data-class governance

A second, self-contained bundle (`package helmdeep.authz`, one file) that
demonstrates Milestone M2's addition to the policy input contract:
`input.action.risk`, `input.action.data_classes`, and
`input.action.required_scopes`, all populated by the gateway from the tool's
`pkg/toolregistry.Entry` — never asserted by the caller. See
`docs/adr/0008-tool-registry.md`.

This is separate from `examples/policies/`, which the quickstart and most
of the e2e suite already exercise and demonstrates scope/taint/rate-limit
policy instead. Point `policy.path` at this directory instead of
`examples/policies/` to see this bundle in action; the two aren't meant to
be merged.

## Rules

1. A tool whose `data_classes` includes `"pii"` requires the caller to hold
   every one of the tool's `required_scopes` (`input.subject.scopes`,
   sourced from the caller's Session Identity Token's `scope` claim). Missing
   even one scope denies the call, regardless of risk tier.
2. A tool rated `"high"` or `"critical"` risk is allowed but its result is
   redacted (an `allow_with_obligations` decision with a `redact`
   obligation) — the enforcement `docs/05-tool-gateway.md` §1.1's
   not-yet-built approval workflow gets in Phase 0.
3. Everything else — low/medium risk, no PII — is allowed outright.

## Try it

Register a tool with `data_classes: ["pii"]` and `scopes: ["crm.pii_read"]`
in your config's `tools:` section, then call it with a caller that has
`crm.pii_read` in its granted scopes (a JWT SIT's `scope` claim, or a
static-token identity's `scopes` field — see
`internal/gateway.StaticIdentity`) versus one that doesn't.
