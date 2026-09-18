# HelmDeep

A policy enforcement gateway for AI agent tool calls, and — longer-term — a
trusted, secure, and governed runtime environment for enterprises to build,
deploy, and orchestrate AI agents at scale.

**Status: pre-alpha, Step 1 (scaffold and architecture).** Nothing here
enforces policy yet. See [`ARCHITECTURE.md`](ARCHITECTURE.md) for the
component decomposition and [`ROADMAP.md`](ROADMAP.md) for what's actually
being built and in what order. A usage README with a quickstart lands once
the Tool Gateway (Step 2) is implemented.

## Why

The model is not a security boundary: indirect prompt injection can make an
agent attempt actions its operator never intended. The agent's own code is
not a security boundary either: a compromised dependency can bypass any
check that lives inside the agent process. So enforcement has to sit outside
the agent, on a path it cannot route around — that's the Tool Gateway this
repo is building. See [`ARCHITECTURE.md`](ARCHITECTURE.md) for the full
argument and [`docs/`](docs/) for the platform-level design this component
implements a piece of.

## Repo layout

- `ARCHITECTURE.md` — component decomposition, interfaces, request flow
- `ROADMAP.md` — phased plan
- `docs/adr/` — architecture decision records
- `docs/` — the platform-level design documents (00–15) this repo implements
  against, plus this component's own docs (`threat-model.md`, `policy-guide.md`)
- `pkg/` — the shared contracts (`types`) and component interfaces
- `internal/gateway`, `cmd/helmdeep-gateway` — the Tool Gateway (Step 2)

## Contributing

See [`CONTRIBUTING.md`](CONTRIBUTING.md) — in particular, the component
status table there before starting work on anything under `pkg/`.

## License

Apache 2.0, Copyright (c) 2026 Overarching AI LLC. See [`LICENSE`](LICENSE).
