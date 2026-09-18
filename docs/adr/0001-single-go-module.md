# ADR 0001 — Single Go module for the monorepo

## Context

HelmDeep is a monorepo housing one fully-implemented component (the Tool
Gateway) and several interface-only stubs that will grow into real
components over an unknown timeline. There's a solo maintainer today.

## Decision

One `go.mod` at the repo root, covering every package under `pkg/`,
`internal/`, and `cmd/`.

## Alternatives considered

- **Multi-module (one `go.mod` per component)** — gives each future
  component independent versioning and release cadence, and would let a
  consumer depend on, say, `pkg/policy` without pulling in everything else.
  Rejected for now: with one maintainer and one real component, the
  coordination overhead (keeping `go.work`, cross-module version bumps, and
  separate CI matrices in sync) outweighs a benefit that only matters once
  there are multiple independently-versioned things to release.
- **Separate repos per component** — clean ownership boundary, but the
  stubs in `pkg/identity`, `pkg/modelgw`, `pkg/sandbox`, `pkg/scheduler`,
  and `pkg/controlplane` exist specifically so later work "plugs in without
  restructuring." Splitting repos now would mean re-establishing that
  plumbing later instead of just filling in a package that's already there.

## Consequences

- Everything in this repo currently shares one version number and one
  release artifact (`helmdeep-gateway`).
- If a future component (e.g. a real Model Gateway) grows large enough to
  need independent versioning or a separate release cycle, splitting it into
  its own module is a mechanical `go mod init` plus import-path update, not
  a redesign — the package boundaries already exist.
- CI runs `go build ./...` / `go test ./...` once for the whole repo, which
  is simpler but means a compile break anywhere blocks everything.
