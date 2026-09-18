# ADR 0004 — Action record format: append-only, hash-chained, file-backed

## Context

Every decision the gateway makes — allow or deny — must be recorded in a way
that's tamper-evident: if someone edits or deletes a past record, that must
be detectable without trusting the store itself. `docs/02-architecture.md`
already sketches a richer Action Record shape (trace IDs, egress targets,
approvers, compensation handles) for the full platform. This repo's Step 2
scope is narrower: timestamp, agent identity, delegation chain (empty for
now), action, parameters with provenance labels, decision, governing policy
ID, and outcome.

## Decision

`types.ActionRecord` (see `pkg/types/record.go`) holds that narrower field
set plus `PrevHash` and `Hash`. Each record's `Hash` is computed over its own
contents concatenated with `PrevHash`, forming a hash chain — the same
construction as a blockchain's block-linking, minus consensus (there's one
writer: the gateway process). Step 2's store implementation
(`pkg/audit.Store`) is file-backed: records are appended to a local file,
one per line, and a `verify-chain` command walks the file recomputing hashes
to report the first break.

## Alternatives considered

- **The full Action Record schema from `docs/02-architecture.md` now.**
  Rejected for Step 2: fields like `egress_targets`, `compensation_handle`,
  and `approver` describe capabilities (egress control, saga compensation,
  human approval routing) that don't exist yet in this repo. Adding the
  fields without the behavior behind them is exactly the kind of
  half-finished implementation the project explicitly avoids. `ActionRecord`
  can grow those fields later, additively, once the gateway actually
  produces them.
- **A database (Postgres, SQLite) instead of a flat file.** A database gives
  indexed queries "for free," which matters a lot for the eventual Audit
  Ledger described in `docs/02-architecture.md`. Rejected for *this*
  component's Step 2 scope: it's a new runtime dependency for a component
  whose only hard requirement right now is "append durably, verify
  sequentially." A flat, hash-chained file is auditable by a stranger with
  `cat` and no schema knowledge — appropriate for a security tool's first
  real artifact. `pkg/audit.Store` is an interface specifically so a
  database-backed implementation (or a WORM-bucket-backed one, matching the
  platform's eventual Observability Plane) can replace the file-backed one
  later without the gateway changing.
- **Signing every record with a runtime key**, as `docs/02-architecture.md`
  describes for the full platform. Out of scope here: signing only means
  something once there's a key management story (whose key, how rotated,
  how a verifier obtains the public key), which is squarely
  identity/credential-exchange territory — currently a stub. The hash chain
  alone still catches tampering-after-the-fact by anyone without write
  access to append a new, correctly-chained tail; it does not prove the
  gateway process itself wasn't compromised at write time. That's a real
  limitation, stated here rather than glossed over: hash-chaining is
  tamper-*evident*, not tamper-*proof*, until signing lands.

## Consequences

- `pkg/types.ActionRecord` is intentionally leaner than the platform's
  eventual Action Record. Anyone extending it should add fields only when
  the behavior producing them exists, not speculatively.
- The file-backed store's `Verify` is an O(n) sequential scan. That's fine
  at the volumes a single gateway instance produces during Step 2's target
  use cases; it stops being fine long before the platform's 10k
  concurrent-session design ceiling (`docs/01-requirements.md`), at which
  point the database-backed implementation this ADR anticipates is the
  answer, not a rewrite of the interface.
- Because there's no signing yet, the audit trail's integrity guarantee
  today rests entirely on "nobody but the gateway process has write access
  to the record file." That assumption belongs in `SECURITY.md`, not left
  implicit.
