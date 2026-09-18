# Example policies

Four files, one bundle (all in `package helmdeep.authz`), demonstrating the
three policy types the Tool Gateway is designed for. See
`docs/policy-guide.md` for the full input contract and
`docs/adr/0002-policy-engine-choice.md` for why one `decision` rule combines
them rather than separate allow/deny rules.

- **`scope.rego`** — which tools each agent may call
  (`agent:support-bot` → `kb.search`; `agent:finance-bot` → the
  supplier/email/payments tools).
- **`taint.rego`** — `payments.wire_transfer`'s `account_number` argument
  must derive from a trusted source. Call it with a value the gateway
  traced back to `supplier.get_account` (trusted) and it's allowed; call it
  with a value traced back to `email.read_latest` (untrusted) and it's
  denied, regardless of scope.
- **`limits.rego`** — `kb.search` is capped at 3 calls per rolling minute
  per agent.
- **`decision.rego`** — combines the three into the single `decision` rule
  the gateway queries, with scope checked before taint, taint before
  limits.

This bundle is what `config.yaml` in `examples/quickstart/` points
`policy.path` at.
