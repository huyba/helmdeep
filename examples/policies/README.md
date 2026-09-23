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

It ships unsigned, and no signing key is checked into this repo — a
committed private key would be a worse artifact to leave lying around than
an unsigned example. To sign it yourself:

```bash
helmdeep-gateway policy keygen -out-dir keys
helmdeep-gateway policy sign -bundle examples/policies -key keys/policy.key -version v1
helmdeep-gateway policy verify -bundle examples/policies -public-key keys/policy.pub
```

(`keys/` and `*.key` are gitignored, so a key generated this way can't be
committed by accident.)

Then add `policy.signature.public_key: keys/policy.pub` to the config and
the gateway will refuse this bundle if anything in it changes without
being re-signed. See `docs/policy-guide.md` ("Signing a bundle") and
`docs/adr/0014-policy-service.md`.
