# Policy Guide

Placeholder for Step 2. This will document how to write HelmDeep policy in
Rego (see `docs/adr/0002-policy-engine-choice.md`) against the
`types.DecisionRequest` shape defined in `pkg/types/decision.go`, covering
each of the three policy types the Tool Gateway is designed for:

- **Scope** — which tools an agent may call, on which resources
- **Provenance / taint** — requiring a field to derive from a trusted
  `types.Provenance.Source`
- **Aggregate limits** — call rate, cumulative cost, per-session budget,
  evaluated against `types.Usage`

Worked examples will live in `examples/policies/` once Step 2 lands; this
file will explain them.
