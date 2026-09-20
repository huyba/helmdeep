# ADR 0007 — Milestone M1's credential broker: what's real, what's simplified, and why

## Context

`docs/04-identity-authz.md` describes a full identity and credential-broker
system: Session Identity Tokens (SITs) with a rich claim set (`agent_version`,
`scope`, `constraints` with `data_classes`/`row_filter`/`trust_level`/
`residency`), delegation chains of structured per-hop objects, SPIFFE/SPIRE
workload identity, an Enterprise IdP integration (§6), and a credential
ladder whose best rung is a real OAuth on-behalf-of token exchange (RFC
8693) against a downstream system's own authorization server.

Milestone M1 ("Phase 0" completion plan, per the working rules that
requested this ADR) needs to close the biggest gap — an unverifiable static
dev token as every caller's identity — without building all of the above.
This ADR records exactly which parts are real now, which are deliberately
narrower than the doc, and why, per those working rules' instruction to
record doc deviations here rather than editing the doc itself.

## Decisions

### 1. SIT claims are a minimal subset: `sub`, `agent`, `delegation_chain`, `aud`, `iat`, `exp`

Doc 04 §1.1's full SIT also carries `agent_version` and a `constraints`
object (`data_classes`, `row_filter`, `trust_level`, `residency`). None of
these are included.

**Why:** nothing in this repo consumes them yet, and adding claims with no
behavior behind them is exactly the anti-pattern `docs/adr/0004-action-record-format.md`
already rejected once for the Action Record schema, for the same reason:
a field with no consumer is speculative surface, not documentation of
intent. `agent_version` matters once agent artifact digests are checked
against a registry (`pkg/controlplane`, a stub); `constraints` matters once
row-level filtering or residency enforcement exist (neither does). Add each
claim in the same change that gives it a reader.

**Consequence:** `types.Subject` needed no schema change — `AgentVersion`
and `TrustLevel` already exist on it (unused by this milestone) and
`DelegationChain` already exists as `[]string`.

### 2. Delegation chain is `[]string` (principal IDs only), not doc 04's per-hop objects

Doc 04 §1.1's `delegation_chain` is an array of objects — `{principal,
auth_time, method}` for the human hop, `{principal, version, instance}` for
agent hops. `pkg/types.Subject.DelegationChain` stays `[]string`: an
ordered list of principal identifiers, outermost first.

**Why:** same reasoning as above — `auth_time`/`method`/`version`/`instance`
per hop have no consumer today (no anomaly detection reads `auth_time`, no
replay tooling reads `instance`). A list of identifiers is exactly what
`docs/01-requirements.md`'s journey J2 and doc 11's T10 (insider misuse)
actually need: "which human's authority ultimately backed this write."

**Consequence:** if a consumer needs the richer per-hop metadata later,
`DelegationChain`'s element type changes from `string` to a struct — a
breaking change to every policy bundle that reads it via
`input.subject.delegation_chain[_]` as a bare string. Documented here so
that migration isn't a surprise.

### 3. The human hop is asserted, not independently verified

Per the working rules' first confirmed answer: the agent hop is
cryptographically verified (JWT signature via `pkg/identity.JWTResolver`).
The human/user hop in `delegation_chain` is whatever
`internal/devissuer.Issuer.IssueSIT` was told to put there — a claim the
issuer signs, not a claim checked against a real Enterprise IdP, because no
such integration exists (doc 04 §6 is explicitly future work, not part of
this milestone).

**Consequence:** "verified principal and delegation chain," this
milestone's own exit criterion, means the *agent* principal is verified and
the *chain* is carried with integrity (nobody can tamper with it without
invalidating the signature) — not that every hop's claim about itself has
been independently confirmed. That distinction belongs in `SECURITY.md`
and `STATUS.md`, not only in this ADR.

### 4. On-behalf-of token exchange is self-contained: one issuer plays both IdP and authorization server

Doc 04 §3's credential ladder wants rung 1 (OBO exchange) to produce "a
downstream token carrying the end user's identity" from the *downstream
system's own* authorization server. No upstream this repo controls — the
mock fixtures or any real system — implements RFC 8693 token exchange.

**Decision:** `internal/devissuer` exposes its own `/token-exchange`
endpoint. The Gateway calls it with the caller's SIT as `subject_token` and
gets back a new token, signed by the same issuer, scoped to exactly one
upstream and one tool, with its own short TTL — proving the *mechanism*
(mint short-lived, per-call, narrowly-scoped credentials; never forward the
agent's own SIT upstream) without a real external AS to exchange against.

**Alternatives considered:** standing up a second, separate fake
authorization server just to have two parties instead of one. Rejected:
it would add a network hop and a service boundary with no additional
verification value — the trust relationship is identical either way
(the Gateway trusts whatever `internal/devissuer` signs), and doc 04's own
credential ladder rung 3 ("shared connector credential... flagged in the
tool registry as elevated risk") already acknowledges most real upstreams
won't offer rung 1 for a long time regardless.

**Consequence:** `internal/mockupstream` does not verify the minted token
— it already ignores auth headers entirely (see its own doc comment). "Done"
for M1 is checked gateway-side and issuer-side (the token that was minted
is well-formed, scoped, and short-lived — see
`test/e2e/identity_test.go`'s `TestValidSITAllowsCallAndRecordsVerifiedPrincipal`),
not by upstream enforcement. A milestone that needs upstream-side
verification (proving a real system rejects a stale or wrongly-scoped
token) is real, future work, not silently assumed here.

### 5. `lestrrat-go/jwx/v3`, ES256

Already a transitive dependency (OPA's `io.jwt.*` Rego builtins use it —
see `docs/adr/0002-policy-engine-choice.md`), so using it for SITs and the
Credential Broker adds zero net dependency surface, matching this repo's
existing dependency-reuse pattern (`sigs.k8s.io/yaml` for config is the
other example). ES256 is asymmetric — the Gateway holds only a public key,
never the issuer's private key — and matches SPIFFE JWT-SVID's own
conventions, keeping "SPIFFE-compatible" (doc 04's own stated direction)
true in more than name.

### 6. `-dev-insecure` is the only path to static-token identity, enforced in one place

`cmd/helmdeep-gateway`'s `checkDevInsecureGate` is the single function that
decides whether static-token identity is allowed to start at all — kept
separate from the JWKS-fetching and resolver-construction I/O around it
specifically so it's unit-testable without network access
(`cmd/helmdeep-gateway/main_test.go`). `loadConfig` separately refuses
`identity.static_tokens` and `identity.jwt` configured together, so the two
identity sources can never be ambiguous about which one is actually active.

## What this milestone does not claim

- No SPIRE integration — `JWTResolver`'s claim shape is compatible with a
  future SPIRE-backed issuer, but SPIRE itself is not wired in.
- No revocation (doc 04 §5's bloom filter, sub-5-second propagation target)
  — a compromised SIT is valid until it expires, full stop, in this
  milestone.
- No RBAC/ABAC/ReBAC layering (doc 04 §4) beyond what `pkg/policy`'s Rego
  bundle already expressed before this milestone.
- No quarterly access review export, no joiner/mover/leaver sync (doc 04 §6)
  — there is no real IdP to sync from.

None of these are silently deferred; each is a real gap, tracked in
`STATUS.md`'s doc 04 row.
