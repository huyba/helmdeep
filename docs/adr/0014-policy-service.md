# ADR 0014 — The Policy Service: signed bundles, and what that leaves out

## Context

`docs/02-architecture.md` §2 lists the Policy Service as a Control Plane
component: "Authoring, compilation, testing, distribution of signed policy
bundles," stored in "Postgres + OCI registry for bundles."
`docs/09-governance-trust.md` §2.2 states the operational rule —  policy is
"written in Cedar/Rego, stored in git, reviewed via PR, tested with unit
tests and dry-run against historical Action Records... compiled to signed
bundles, versioned, distributed to sidecar PDPs, pinned per session" — and
§4 lists policy bundles among the inputs that must be versioned artifacts
under change control.

What existed before this ADR: `pkg/policy.OPADecider` compiles whatever
`.rego` files happen to be in the configured directory at the moment it
reads them, at startup and again on every `SIGHUP`. Nothing established
that those files were the ones anyone reviewed, and the Action Ledger
recorded which *rule* fired (`decision.policy_id`) but not which *bundle*
that rule came from. Two consequences, both real:

1. Bundle content was trusted by location. Anything that can write to the
   policy directory — a compromised config-management run, a mistaken
   `kubectl cp`, an operator editing a mounted file "just to test
   something" — silently became policy on the next reload. This is the one
   input where that matters most: policy is what the gateway consults
   before every single tool and model call.
2. The ledger could not answer "what policy was in force when this
   action was allowed?" A rule id is not stable across edits to the bundle
   that defines it, so `"policy_id": "scope.deny"` in two records is not
   evidence that the same rule decided both.

This ADR implements the "signed bundles, versioned" half of doc 02's
responsibility line, and only that half.

## Decisions

### 1. The unit of signing is the bundle's complete file inventory, not each file

`pkg/policyservice.Manifest` lists every file in the bundle with its
SHA-256 digest, plus an operator-chosen version, a creation timestamp, a
key id, and one Ed25519 signature over all of it. `Verify` checks the
signature first, then compares the manifest against the filesystem as a
*set*: a changed digest, a file the manifest doesn't cover, and a file the
manifest covers that isn't there are three distinct, individually-reported
failures.

**Why set equality and not just per-file digests:** the attack that a
per-file check misses is addition. Dropping a new
`zzz-allow-everything.rego` into the bundle directory changes what OPA
compiles without modifying a single signed file — and because this repo's
bundles combine rules into one `decision` rule
(`docs/adr/0002-policy-engine-choice.md`), a bundle with an extra file is
not merely "extra policy," it can change the meaning of the rule the
gateway queries. `TestVerify_AddedFileFails` pins this.

### 2. The signed file set is exactly the compiled file set

`inventory` mirrors `pkg/policy`'s `hiddenFileFilter` — it skips anything
below the bundle root whose name starts with `.` — so what gets signed and
what OPA compiles cannot diverge. Two things fall out of that rather than
needing special cases: a Kubernetes ConfigMap volume's hidden
`..data`/`..<timestamp>` indirection is skipped here just as it is in the
compiler (the visible per-key symlinks are what get hashed, and reads
follow them), and the manifest's own default location — `.bundle.json`
*inside* the bundle — is excluded from its own inventory.

A verifier whose file set didn't match the compiler's would be worse than
none: it would report a bundle as intact while the compiler read something
else. The walk is rooted with `os.OpenRoot`, so a symlink pointing out of
the bundle cannot cause a file the bundle doesn't contain to be hashed,
and a symlinked *directory* (which `WalkDir` would not descend into,
leaving files inside the signed tree unhashed) is refused outright rather
than silently half-inventoried.

### 3. Verification decorates the loader; it is not a mode inside the PDP

`VerifyingLoader` implements `pkg/policy.Loader` around the real one. A
deployment either has one wired in or doesn't; there is no "skip
verification" path through it, and no boolean inside `OPADecider` that
could be false in production by accident.

It also preserves, rather than weakens, the existing reload contract: a
bundle that fails verification at `SIGHUP` time leaves the previously
loaded policy active and returns an error, exactly as a bundle that fails
to *compile* already did (`pkg/policy.OPADecider.Reload`). The alternative
— dropping policy and failing closed — turns one bad edit into a total
outage, which is how a fail-closed system gets routed around.
`TestVerifyingLoader_WithARealOPADecider` verifies the whole path against
the real PDP: an unsigned allow-everything edit is refused at reload and
the signed bundle's deny is still what gets decided afterwards.

A bundle that verifies but doesn't compile leaves `Active()` reporting what
the PDP really holds, not what we tried to give it.

### 4. The bundle reference is stamped onto records by an audit-store decorator

`StampingStore` wraps `pkg/audit.Store` and fills in
`types.ActionRecord.PolicyBundle` (version + manifest digest) on every
append.

**Why a decorator rather than a parameter on each gateway:** the stamp is
the same fact for every record regardless of which component wrote it, so
`internal/gateway` and `pkg/modelgw` both get it from one wiring change in
`cmd/helmdeep-gateway` with no signature change to either — and
`internal/gateway.New` already takes nine parameters. It also keeps the
property `docs/adr/0004-action-record-format.md` relies on: record
contents are asserted by the platform, never contributed by a caller.
`Append` discards any `PolicyBundle` it receives before setting its own.

The stamp is inside the hash chain (`pkg/audit`'s `hashable` carries it),
so rewriting a record's bundle reference after the fact breaks
verification. It is a pointer with `omitempty` specifically so that a
record written without one hashes to exactly the bytes it always did:
ledgers written by earlier builds still verify against this code.

### 5. `policy.signature` is opt-in, and its absence is logged, not fatal

With `policy.signature.public_key` configured, an unsigned or modified
bundle stops the gateway from starting and stops every reload. Without
that section, the bundle loads unverified and startup logs a warning.

**Why not mandatory, given this repo's fail-closed habits:** the
comparable gate — unverifiable static-token identity — is refused unless
`-dev-insecure` is passed, and that shape works there because the safe
alternative (a real JWT issuer) exists in-repo. Here it doesn't. Making
signing mandatory would require a signing key to be present wherever the
gateway runs, and this repo has no key management to get one from: no KMS,
no OCI registry, no secret store, and `deploy/k8s`'s bundle is a ConfigMap
an operator edits directly. The honest options were "ship the check
opt-in" or "commit a dev private key to make the examples work" — the
second is a worse artifact to leave in a security-focused repo than a
documented gap. This matches `pkg/toolregistry.Entry.Upstream`'s existing
precedent (an empty value skips the egress check, documented as a
compatibility default and not a bypass — ADR 0009).

Consequence to be explicit about: a deployment that does not set
`policy.signature` is exactly as exposed as it was before this ADR, and
its records carry no bundle reference. Nothing here improves a deployment
that doesn't opt in.

### 6. Keys and CLI are stdlib-only

`policy keygen`, `policy sign`, and `policy verify` use `crypto/ed25519`
and `crypto/x509` PEM encoding — no new dependency, and nothing to install
alongside the binary to make a bundle. `keygen` refuses to overwrite an
existing keypair: overwriting the private key invalidates every manifest
already signed with it, and the previous key is unrecoverable.

Ed25519 rather than a certificate chain or a Sigstore/cosign-style
keyless flow because those introduce an identity and trust-root problem
(who issues, who revokes, who verifies the chain) that this repo has no
component to answer today — the same reason `pkg/identity`'s SPIFFE
support is "compatible with" rather than "backed by" (ADR 0007).

## What this milestone does not claim

- **No authoring, compilation, or testing service.** Doc 02's first three
  responsibilities are untouched: no policy authoring surface, no bundle
  test runner, and specifically no "dry-run against historical Action
  Records" (doc 09 §2.2's "this new rule would have blocked 43 actions
  last month") — even though the hash-chained ledger that would make it
  possible now exists.
- **No distribution.** There is no bundle registry, no OCI push/pull, no
  Postgres, and no staged rollout (doc 09 §4's "PR + dry-run + staged
  rollout"). Distribution is still "put the files where the gateway can
  read them"; this ADR only makes the gateway check what it found there.
- **No per-session pinning.** Doc 09 §2.2 asks for a bundle "pinned per
  session": a session that starts under bundle v3 should keep deciding
  under v3 even if v4 loads mid-session. A reload here swaps policy for
  everyone at once, and the ledger records which bundle decided each
  action after the fact rather than pinning one in advance.
- **No key rotation, revocation, or multiple signers.** One public key,
  configured by path, checked by key id. Rotating means re-signing every
  bundle and restarting with the new key; there is no overlap window and
  no way to say "this key was valid until T."
- **No policy layering.** Doc 09 §2.1's five layers (platform baseline,
  tenant, business unit, agent, contextual overlays, "deny wins") are
  still one flat bundle — a signature over that bundle says nothing about
  who was entitled to author which part of it.
- **No trust root for the key itself.** The public key is trusted because
  an operator put its path in the config. Nothing attests to *that*, and
  an attacker who can rewrite the gateway's config can point it at their
  own key — signing raises the bar from "write one file" to "write the
  config and hold a key," which is a real improvement and not a
  boundary.
- **No signature on the audit ledger.** Action Records remain
  hash-chained but unsigned (ADR 0004's own gap list); this ADR signs
  policy bundles only.

None of these are silently deferred; each is a real gap, tracked in
`STATUS.md`'s doc 02 and doc 09 rows.
