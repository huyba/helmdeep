# Contributing

## Component status — read this before starting work

This is a monorepo for several components at very different stages. Please
don't start implementation work on a component this table marks as a stub —
it means the interface was deliberately left unimplemented, not that it was
overlooked.

| Component | Status | Package |
|---|---|---|
| Tool Gateway | **Build now** — full implementation | `internal/gateway`, `cmd/helmdeep-gateway` |
| Policy Decision Point | Interface + embedded OPA/Rego engine | `pkg/policy` |
| Audit / Action Record store | Interface + file-backed impl | `pkg/audit` |
| MCP protocol handling | Interface + wire implementation | `pkg/mcp` |
| Identity & credential exchange | **Interface + stub only** — no implementation | `pkg/identity` |
| Model Gateway | **Interface + stub only** — no implementation | `pkg/modelgw` |
| Agent sandbox runtime | **Interface + stub only** — no implementation | `pkg/sandbox` |
| Scheduler | **Interface + stub only** — no implementation | `pkg/scheduler` |
| Control plane | **Interface + stub only** — no implementation | `pkg/controlplane` |

Every stub package carries a `// STATUS: interface only. Not implemented.`
header. If you want to work on one of those components, please open an issue
first — see `ROADMAP.md` for the intended phase order and rationale.

## Building and testing

```sh
make build   # go build ./...
make test    # go test -race ./... — includes test/e2e and test/adversarial
make lint    # golangci-lint run
make bench   # go test -run '^$' -bench . ./...
make fuzz    # bounded fuzzing of the untrusted-input surfaces; FUZZTIME=30s make fuzz to run longer
```

`test/adversarial/` deserves a look before you touch policy or identity
code: it names each test after a real threat rather than the mechanism
that defeats it, and doubles as documentation of what this gateway
actually defends against — see its own README.

All four should pass before opening a PR. CI runs them on every push (see
`.github/workflows/ci.yml`), plus `govulncheck`.

## Code style

Boring, obvious code over clever code — this is a security component and
will be read by strangers auditing what it does. In particular:

- No cleverness in the fail-closed path (`docs/adr/0003-fail-closed-behavior.md`).
  If you're not sure whether an error should deny, it should deny.
- Prefer an explicit `if err != nil { return deny }` over a helper that
  hides the failure mode.
- New exported types belong in `pkg/types` only if more than one package
  needs them. If only `internal/gateway` needs a type, it stays there.

## License headers

Every source file is Apache 2.0, Copyright (c) 2026 Overarching AI LLC. CI
checks for the header; `make build` will not add it for you.

## Architecture decisions

Non-trivial design choices are recorded in `docs/adr/` as numbered
Architecture Decision Records (Context → Decision → Alternatives considered
→ Consequences). If your PR makes a decision with real trade-offs, add one
rather than leaving the reasoning only in the PR description — PR
discussions are harder to find later than a file in the repo.

## Commit and PR expectations

- Keep PRs scoped to one component's status row above.
- If a PR would move a component from stub to implemented, say so explicitly
  in the description and expect closer review — that's exactly the boundary
  this table exists to protect.
