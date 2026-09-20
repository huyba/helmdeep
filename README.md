# HelmDeep

An open-source policy enforcement gateway for AI agent tool calls — and,
longer-term, a trusted runtime environment for enterprises to build,
deploy, and orchestrate AI agents at scale. This repo currently implements
one piece of that: the **Tool Gateway**.

**Status: working, pre-1.0.** The Tool Gateway is implemented and tested
(unit, end-to-end, benchmarked). Everything else — identity & credential
exchange, the Model Gateway, the sandbox runtime, the scheduler, the control
plane — is an interface-only stub. See [`ARCHITECTURE.md`](ARCHITECTURE.md)
for the full decomposition and [`ROADMAP.md`](ROADMAP.md) for what's next.

## Why enforcement has to sit outside the agent

The model is not a security boundary: indirect prompt injection — text in a
fetched web page, an email, a document the agent reads — can make an agent
attempt actions its operator never intended. The agent's own code is not a
security boundary either: a compromised dependency can bypass any check
that lives inside the agent process. If the policy check runs *inside* the
same process an attacker can already influence, it isn't a check.

So enforcement has to sit outside the agent, on a path it cannot route
around. HelmDeep's Tool Gateway speaks MCP to the agent — the agent believes
it's talking directly to the real tool servers — evaluates every
`tools/call` against policy, records what it decided (allow or deny) in a
tamper-evident log, and only then forwards the allowed ones upstream, using
credentials the agent never holds. An agent fully hijacked by a malicious
tool result still cannot do anything the gateway's policy hasn't
authorized, because the authorization check isn't inside the process the
injection controls.

This targets the current MCP spec (2026-07-28, stateless) exclusively —
see [ADR 0005](docs/adr/0005-mcp-protocol-compatibility.md) for why
supporting the prior session-based protocol alongside it was rejected: two
ways to resolve caller identity inside a policy enforcement point is a
bypass waiting to happen, not a compatibility feature. **Statelessness here
is a scalability property of the protocol, not a security one** — the
security property that matters (identity resolved fresh, from a source the
gateway controls, on every single request) was achievable under the old
spec too. What the new spec removes is the temptation to do it any other
way.

## Quickstart

```sh
cd examples/quickstart
docker compose up --build
```

Five minutes to a policy allow, a scope-based deny, and a taint-based deny
against mock upstream systems — see
[`examples/quickstart/README.md`](examples/quickstart/README.md) for the
full walkthrough with `curl` commands.

## A worked policy example

Three policy types, one combined decision — see
[`docs/policy-guide.md`](docs/policy-guide.md) for the full input contract
and [`examples/policies/`](examples/policies/) for the actual bundle.

**Scope** — which tools an agent may call:

```rego
allowed_tools := {
	"agent:support-bot": {"kb.search"},
	"agent:finance-bot": {"supplier.get_account", "email.read_latest", "payments.wire_transfer"},
}

scope_allowed if { input.action.tool in allowed_tools[input.subject.id] }
```

**Provenance / taint** — a wire transfer's account number must derive from
the supplier master record, never from an inbound email, regardless of who's
asking:

```rego
tainted_requirements := {"payments.wire_transfer": {"argument": "account_number"}}

taint_violation if {
	req := tainted_requirements[input.action.tool]
	arg := input.action.arguments[req.argument]
	not arg.provenance.trusted
}
```

The gateway derives that `provenance.trusted` label itself, from having
directly observed which upstream produced the value — it never trusts the
agent's own claim about where a value came from (a prompt-injected agent
would just lie). See `internal/gateway/provenance.go` and
`docs/policy-guide.md`.

**Aggregate limits** — call-rate ceilings, using a counter the gateway
maintains and the policy only reads:

```rego
rate_limits := {"kb.search": 3}

limit_exceeded if {
	limit := rate_limits[input.action.tool]
	input.context.usage.calls_in_window > limit
}
```

Run `go test ./pkg/policy/... -run TestExamplePolicies -v` to see all three
exercised together, or `go test ./test/e2e/...` to see them exercised over
real HTTP against mock upstream servers.

## Performance

All numbers below: Apple M1 Max, macOS, local SSD, `go test -bench`. Real
production numbers will differ, especially the audit write (disk/volume
dependent) and the upstream call (network-dependent) — rerun these on your
own hardware before trusting them for capacity planning.

**Policy decision latency** (`go test ./pkg/policy/... -bench BenchmarkDecide$`)
— the number `ROADMAP.md`'s sub-5ms target is actually about, in isolation
from HTTP, identity, and I/O:

```text
BenchmarkDecide-10    29874    79625 ns/op    30831 B/op    622 allocs/op
```

~80µs against the real three-policy-type bundle (scope + taint + limits
combined) — about 60x under target.

**Policy set scaling** (`go test ./pkg/policy/... -bench BenchmarkDecideScaling`)
— does decision latency degrade as a deployment's rule count grows? Each
case decides against the *last* rule in a synthetic bundle of 10, 100, or
1000 independent scope rules:

```text
BenchmarkDecideScaling/10_rules-10      37843    29832 ns/op
BenchmarkDecideScaling/100_rules-10     38320    33657 ns/op
BenchmarkDecideScaling/1000_rules-10    34581    32977 ns/op
```

Flat. A 100x increase in rule count moved latency by roughly noise — OPA
indexes the equality comparisons these rules are built from, so this
isn't a linear scan, and shouldn't be a scaling concern until a bundle's
shape looks very different from "many independent equality rules."

**Gateway overhead vs. calling the mock upstream directly**
(`go test ./test/e2e/... -bench .`) — the full round trip (identity
resolution, a real policy decision, a durable audit write, the forwarded
call) against the same call with nothing in front of it:

```text
BenchmarkDirectUpstreamCall-10    14391    81784 ns/op
BenchmarkGatewayOverhead-10         274   5822871 ns/op
```

~82µs direct, ~5.8ms through the gateway — roughly 70x, which sounds far
worse than the 15% platform-overhead target in `ROADMAP.md` until you find
out where the time actually goes. Isolating the audit write
(`go test ./pkg/audit/... -bench BenchmarkAppend`):

```text
BenchmarkAppend-10    310    3770145 ns/op
```

~3.8ms — the large majority of the gateway's overhead is the `fsync` in
every durable audit write, not policy evaluation (~80µs) or HTTP handling.
This is the tradeoff `docs/adr/0003-fail-closed-behavior.md` and
`docs/adr/0004-action-record-format.md` describe on purpose: the gateway
does not proceed with a call until the decision recording it is durable on
disk. It is a real cost, stated honestly here rather than buried in an
aggregate number — and it's why the 15% target in `ROADMAP.md` is
expressed relative to a real agent's model+compute spend (typically
hundreds of milliseconds to seconds per call), not relative to a
microsecond-scale mock upstream: a few milliseconds of durable-write
latency is a rounding error against an LLM call and a real network hop to
a production API, and a 70x multiple against an 82µs baseline is not the
same claim as a 70x multiple against a real call's actual latency.
Keeping the audit file descriptor open across calls (rather than
open-close on every append) would trim some fixed overhead; the `fsync`
itself is the floor unless the durability guarantee changes.

## Dependencies and binary size

Three direct dependencies, two of which add *zero* net dependency surface
because they're already compiled in as OPA's own transitive dependencies:
`github.com/open-policy-agent/opa` (the embedded Rego evaluator — see
[ADR 0002](docs/adr/0002-policy-engine-choice.md)), `sigs.k8s.io/yaml`
(config parsing — OPA's Rego builtins use it), and
`github.com/lestrrat-go/jwx/v3` (Session Identity Tokens and the credential
broker, Milestone M1 — OPA's `io.jwt.*` Rego builtins use it; see
[ADR 0007](docs/adr/0007-credential-broker-scope.md)). No CGO, no OPA
server, no OPA CLI: only the `rego` evaluation package is imported.

The static `linux/amd64` binary is **~23MB**, `-trimpath -ldflags="-s -w"`.
Most of that is OPA's `topdown` evaluator and its built-in function library
(JWT, glob, SQL, GraphQL builtins are registered unconditionally, whether or
not your policies use them) — a known, real cost of embedding OPA, stated
here rather than glossed over.

## Repo layout

- `STATUS.md` — what's implemented vs. not, one row per design doc
- `ARCHITECTURE.md` — component decomposition, interfaces, request flow
- `ROADMAP.md` — phased plan
- `docs/adr/` — architecture decision records
- `docs/` — the platform-level design documents (00–15) this repo implements
  against, plus this component's own docs (`threat-model.md`, `policy-guide.md`)
- `pkg/types` — shared contracts every component depends on
- `pkg/mcp` — the MCP wire protocol (JSON-RPC, `_meta`, headers, transport)
- `pkg/policy`, `pkg/audit` — the PDP and the hash-chained audit log
- `pkg/identity` — SIT verification and the credential broker (Milestone M1); `internal/devissuer` is the dev-only issuer/exchange server it talks to
- `pkg/modelgw`, `pkg/sandbox`, `pkg/scheduler`, `pkg/controlplane` — interface-only stubs, not implemented
- `internal/gateway` — the Tool Gateway's business logic
- `cmd/helmdeep-gateway` — the product binary
- `internal/mockupstream`, `cmd/mock-upstream` — test/demo fixtures, not part of the product
- `examples/policies`, `examples/quickstart` — the worked example above, runnable
- `test/e2e` — end-to-end tests against the fixtures above (including identity/credential-broker tests)
- `test/adversarial` — tests named after real threats, mapped to OWASP ASI codes

## Contributing

See [`CONTRIBUTING.md`](CONTRIBUTING.md) — in particular, the component
status table there before starting work on anything under `pkg/`.

## Security

See [`SECURITY.md`](SECURITY.md) for the vulnerability reporting process
and an explicit statement of what this gateway does and does not defend
against.

## License

Apache 2.0, Copyright (c) 2026 Overarching AI LLC. See [`LICENSE`](LICENSE).
