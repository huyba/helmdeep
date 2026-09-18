# Example policies

Placeholder. Step 2 will add three Rego policies here, one per policy type
the Tool Gateway supports — see `docs/policy-guide.md` and
`ARCHITECTURE.md`:

- a **scope** policy (which tools/resources an agent may call)
- a **provenance/taint** policy (a field must derive from a trusted source —
  e.g. bank account details from the supplier master record, never from an
  inbound email)
- an **aggregate limits** policy (call rate, cumulative cost, per-session
  budget)
