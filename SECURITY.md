# Security Policy

HelmDeep is a policy enforcement gateway. Its entire job is to be a security
boundary an agent cannot route around, so vulnerabilities here are taken
seriously and triaged ahead of feature work.

## Reporting a vulnerability

Please report suspected vulnerabilities privately rather than opening a
public issue: use
[GitHub's private vulnerability reporting](https://github.com/huyba/helmdeep/security/advisories/new)
for this repository, or email **anhhuybk@gmail.com** with `[HelmDeep security]`
in the subject line if you'd rather not use GitHub.

Please include:
- What component is affected (`cmd/helmdeep-gateway`, a specific `pkg/`, etc.)
- A minimal reproduction, if you have one
- What you believe the impact is (e.g. "an agent can bypass a scope denial
  by...", not just "this looks wrong")

You should get an acknowledgment within a few days. This is a solo-maintainer
project as of this writing, so response time is best-effort, not SLA-backed —
if it's urgent and you've heard nothing, a follow-up email is welcome.

## Supported versions

Pre-1.0: there is no supported-versions table yet, because there is no
tagged release yet. Once tagged releases exist, this section will name which
lines receive security fixes.

## What this project defends against (today)

As of Phase 1 (the Tool Gateway — see `ROADMAP.md`), once implemented:

- An agent process fully compromised by indirect prompt injection or a
  malicious dependency attempting to call a tool, on a resource, or with
  arguments its policy does not permit.
- A tool-call argument whose value traces back to an untrusted source (an
  inbound email, fetched web content, prior tool output) being used where
  policy requires it to derive from a trusted source instead (provenance /
  taint enforcement — see `ARCHITECTURE.md`).
- An agent exceeding a configured call-rate, cost, or per-session budget
  limit.
- The Policy Decision Point being unreachable or erroring: this is treated
  as an explicit deny, never as an implicit allow (see
  `docs/adr/0003-fail-closed-behavior.md`).
- Undetected retroactive tampering with the audit log: records are
  hash-chained so an edited or deleted past record breaks the chain in a way
  `verify-chain` reports.

## What this project does not defend against (yet, or at all)

Be precise about these — a false sense of coverage is worse than a known gap:

- **The gateway process itself being compromised.** Hash-chaining the audit
  log makes tampering *evident*, not *impossible*: an attacker with control
  of the running gateway process could, in principle, produce a
  self-consistent forged chain from that point forward. Cryptographic
  signing of records (tying them to a runtime identity a compromised process
  can't forge) is future work — see `docs/adr/0004-action-record-format.md`.
- **Compromise of an upstream MCP server.** The gateway controls what an
  agent can ask an upstream to do; it does not harden or sandbox the
  upstream server itself.
- **Anything inside the model or the agent process.** By design — see
  `ARCHITECTURE.md`'s opening paragraph. If you're looking for prompt
  injection *mitigation* inside the model loop, that's out of scope for this
  component; this component assumes injection succeeds sometimes and limits
  the blast radius from outside.
- **Identity and credential exchange are not implemented.** `pkg/identity`
  is an interface-only stub (see `ARCHITECTURE.md`); Phase 1's identity
  resolution is a static token→identity map suitable for development and
  evaluation, explicitly not for production use, until Phase 2 replaces it.
- **Confidentiality of policy bundle contents or the audit log at rest.**
  Neither is encrypted by this project; that's a deployment-environment
  responsibility today.

## Dependencies

CI runs `govulncheck` on every build. Dependency count is kept deliberately
small (see `docs/adr/0002-policy-engine-choice.md` for the one major
dependency taken on so far and why) specifically to keep this surface
reviewable.
