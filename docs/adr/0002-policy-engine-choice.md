# ADR 0002 — Policy engine: OPA/Rego, embedded

## Context

The PDP (`pkg/policy`) needs a policy engine that can express three policy
shapes — scope (which tools/resources an agent may touch), provenance/taint
(a field must derive from a trusted source), and aggregate limits (rate,
cost, budget, evaluated against counters the gateway supplies) — and that
runs *inside* the gateway process as a library, not as a network call,
because it's on the hot path of every tool call (sub-5ms target, see
`ROADMAP.md`). The task also constrains the gateway to Go, a single static
binary, and minimal dependencies. We are not writing a policy engine
ourselves.

The two realistic candidates are **Open Policy Agent (OPA) / Rego** and
**AWS Cedar**.

## Decision

Embed OPA's Rego evaluator (`github.com/open-policy-agent/opa/rego`)
directly in the gateway binary.

## Alternatives considered

- **Cedar.** Cedar has real advantages on paper: it's purpose-built for
  authorization (not general policy), has a more constrained, more
  formally-analyzable grammar, and is explicitly named as an option in
  `docs/02-architecture.md`'s technology choices. But Cedar's reference
  implementation is Rust. The Go story is a community port
  (`cedar-policy/cedar-go`) or CGO bindings to the Rust crate. CGO breaks
  "single static binary, easy distribution" outright — cross-compiling a
  CGO binary for `linux/amd64` and `linux/arm64` from one CI matrix is real
  friction we don't need. A pure-Go community port is safer on that count
  but is not the reference implementation, which means we'd be shipping a
  security-critical evaluation path built on a reimplementation with a much
  smaller install base and audit history than OPA's.
- **Writing our own scope/taint/budget checker in Go, no engine at all.**
  Rejected by the task explicitly ("do not write a policy engine"), and for
  good reason: policy-as-code with dry-run, versioning, and testability
  (`FR-G2` in `docs/01-requirements.md`) is exactly the kind of thing that
  looks simple until someone needs to express a fourth policy shape we
  didn't anticipate.

## Why OPA wins here specifically

- **Native Go.** OPA is written in Go and its `rego` package is designed to
  be embedded as a library — this is the common case for OPA, not a
  workaround. No CGO, no FFI, no second toolchain in the build.
- **Mature and boring.** OPA is a CNCF project with years of production use
  as an embedded authorization engine (Envoy, Kubernetes admission control,
  Kong, etc.). For a security component "read by strangers," boring and
  widely-audited beats novel. Embedding via `rego.PrepareForEval` is a
  well-worn path, not something we're pioneering, and `opa test` gives us
  policy unit testing for free — we don't have to build a policy test
  harness alongside the policy language.
- **Ecosystem fit for the target user.** This gateway's buyer runs agents on
  Kubernetes. Many of those users already know Rego from Gatekeeper/OPA
  admission control in the same clusters. Cedar's syntax is arguably
  cleaner in isolation, but it's unfamiliar to this specific audience.
  Choosing OPA means choosing the policy language the users already speak,
  which matters more for adoption than syntactic elegance.
- **Expressive enough for all three policy shapes.** Rego is a general
  Datalog-family language; scope rules, taint propagation checks, and
  threshold comparisons against supplied counters are all straightforward
  Rego, not stretches of the language.
- **Hot reload fits our model.** OPA supports loading a compiled policy
  bundle and swapping it atomically, which maps directly onto
  `policy.Loader.Reload` without us inventing our own bundle format.

## Alternative also considered: CEL

[CEL](https://github.com/google/cel-go) (`cel-go`) is pure Go, no CGO, and
is what Kubernetes `ValidatingAdmissionPolicy` uses — a real point in its
favor for the same Kubernetes-native audience OPA appeals to. Rejected
anyway: CEL is an expression language, not a policy framework. It evaluates
one boolean (or CEL-typed) expression against an input; it has no native
concept of a policy *bundle*, precedence between rules, or a structured
decision object with obligations. Getting from "one CEL expression" to "a
combinable, hot-reloadable set of scope/taint/limit rules that produce an
`allow` / `deny` / `allow_with_obligations` decision with a policy ID and
reason" means building that structure ourselves in Go around CEL's
evaluator — which is close enough to writing a policy engine that it fails
the task's explicit constraint ("do not write a policy engine"). Rego
already *is* that structure.

## Implementation requirement: compile once, not per request

Policies are compiled with `rego.PrepareForEval` once, at load and on each
hot reload, and the resulting prepared query is reused for every
evaluation. The gateway does **not** parse or compile Rego on the request
path — doing so would alone be enough to miss the sub-5ms decision-latency
target (`ROADMAP.md`), independent of anything else the request handler
does. `pkg/policy`'s OPA-backed `Decider` holds the prepared query behind a
mutex that's only taken to swap it during `Reload`; `Decide` calls read the
current prepared query without blocking on reload except for the instant of
the swap itself.

## Consequences

- The gateway takes on `github.com/open-policy-agent/opa` as its first (and,
  for the foreseeable future, only major) third-party dependency. It's a
  large module; we depend only on the `rego` evaluation package, not OPA's
  HTTP server or CLI, to keep the binary's dependency surface as narrow as
  the import graph allows.
- Policy is authored in Rego. Example policies (Step 2 deliverable) will be
  `.rego` files, not Cedar's policy language.
- If Cedar's Go support matures substantially (a first-party, non-CGO
  implementation with production track record), this decision is worth
  revisiting — but that's a future ADR, not a reason to block on it now.
