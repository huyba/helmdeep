# ADR 0009 — Milestone M3's egress default-deny: what's real, what's simplified, and why

## Context

`docs/05-tool-gateway.md` §3 describes a full Egress Control surface: a
per-tool FQDN allowlist (resolved through the gateway's own DNS, with
pinning, no raw IPs), TLS certificate pinning for first-party connectors,
outbound DLP scanning (PII/secrets/data-class violations blocked or
redacted per policy), per-tool size and rate caps, a per-session
cumulative egress byte budget as an exfiltration circuit breaker,
HTTPS-only with no arbitrary sockets, and an absolute block on cloud
metadata endpoints, internal admin planes, and the HelmDeep control plane
itself.

Milestone M3 ("Phase 0" completion plan, per the working rules that
requested this ADR) needs the "default-deny, not everything reachable by
default" half of that — a real destination control an agent cannot get
past — without building DNS-level pinning, TLS pinning, DLP, byte
budgets, or rate/size caps, none of which anything else in this repo
consumes yet. This ADR records exactly which parts are real now, which
are deliberately narrower than the doc, and why.

## Decisions

### 1. The "per-tool FQDN allowlist" is a per-tool upstream-name allowlist, not raw FQDNs

Doc 05 §3 wants a per-tool allowlist of destination hostnames. This repo
never routes by raw hostname at the tool-call layer — `pkg/mcp.Registry`
already resolves every tool call to one of a fixed, operator-configured
set of named `Upstream`s (`cmd/helmdeep-gateway/config.go`'s `upstreams:`
list), and `pkg/toolregistry.Entry` already carries an `Upstream` field
naming which one a tool is expected to come from (Milestone M2, left
descriptive-only — see ADR 0008).

**Decision:** `internal/gateway.Gateway.CallTool` now enforces
`Entry.Upstream`: if `pkg/mcp.Registry.Resolve` would route the call to an
upstream whose name doesn't match the tool's registered `Upstream`, the
call is refused (`toolregistry.upstream_mismatch`), the same
policy-shaped denial as an undeclared tool. An entry with `Upstream`
left empty skips the check — backward compatible with every M2-era
config and test that never set it, matching the "don't add enforcement
where nothing opted in yet" principle ADR 0008 already used for the field
itself.

**Why an upstream *name*, not a raw destination host:** the upstream's
actual URL is operator config, not something a tool call's arguments can
influence (`pkg/mcp.HTTPUpstream`'s `baseURL` is fixed at construction) —
there is no dynamic destination for an FQDN allowlist to constrain in this
codebase today. The real risk this closes is a *config* or *upstream
catalog* drift: a tool silently rerouted to a different upstream (a typo,
a renamed service, a compromised config management step) without anyone
updating what the tool is *supposed* to reach. That is a real, if
narrower, instance of doc 05's "declared destinations only."

**Consequence:** this is not a defense against a tool call argument
redirecting the gateway's own request to an attacker-chosen host — no
such redirection is possible in the current architecture, so there is
nothing there to defend against yet. If a future connector type
introduces caller-influenced destinations (e.g. a generic HTTP-fetch
tool), that is new attack surface needing its own control, not something
this ADR's mechanism already covers.

### 2. "Blocked always: cloud metadata endpoints" is real, checked at the TCP layer, not the config layer

Doc 05 §3's one absolute, unconditional rule — metadata endpoints,
internal admin planes, and the control plane are always blocked,
regardless of any allowlist — gets its own, separate mechanism:
`pkg/mcp.HTTPUpstream`'s `http.Client` now dials through `blockedDial`
(`pkg/mcp/egress.go`), which refuses any connection whose *resolved*
remote address is link-local (`169.254.0.0/16`, `fe80::/10`) — the address
space AWS's, Azure's, and GCP's instance metadata services all live in,
specifically because it is always reachable and never routed off the
host.

**Why checked on the live connection, not a separate DNS lookup of the
configured hostname:** resolving the hostname ourselves first and dialing
separately would leave a window for the two lookups to disagree — a
classic DNS-rebinding gap, where the name resolves somewhere safe for a
pre-flight check and somewhere else for the real connection. Checking
`net.Conn.RemoteAddr()` on the connection that was actually opened has
nothing left to rebind: whatever IP the OS actually connected the socket
to is what gets checked, unconditionally, every call, not only at config
load.

**Why link-local generally, not a hardcoded `169.254.169.254`:** every
major cloud's IMDS lives in that reserved space precisely because nothing
else legitimate does — a private-network upstream (a Kubernetes Service
DNS name, an internal `10.0.0.0/8`/`172.16.0.0/12`/`192.168.0.0/16`
address) is never link-local, so this has no legitimate deployment to
break, unlike a broader "block all private IPs" rule would (which would
break exactly the internal upstreams a real deployment needs).

**Consequence:** "internal admin planes" and "the HelmDeep control plane
itself" are not covered — those aren't a fixed, universal address range
the way cloud metadata is; blocking them needs an operator-supplied
denylist this milestone doesn't add (nothing configures one yet). HTTPS
enforcement, TLS pinning, and websocket restriction are also untouched:
`pkg/mcp.HTTPUpstream` already only ever does plain `http.Client.Do` to
whatever scheme the configured `baseURL` uses — no new restriction was
added, and none is claimed.

## What this milestone does not claim

- No DNS-level pinning of resolved upstream addresses across calls (doc 05
  §3's "resolved through our DNS with pinning") — each call re-resolves
  independently; `blockedDial`'s per-connection check is the only guard
  against a bad resolution, not a cache of a previously-known-good one.
- No TLS certificate pinning for first-party connectors.
- No outbound DLP scan, no data-class-aware egress blocking.
- No per-tool payload size or rate caps sourced from the registry, and no
  per-session cumulative egress byte budget (the exfiltration circuit
  breaker doc 05 calls out by name) — T2 in `docs/11-security-threat-model.md`
  ("data exfiltration via legitimate tools") is explicitly *not*
  mitigated by this milestone.
- No protocol restriction beyond what already existed (`pkg/mcp.HTTPUpstream`
  was always HTTP-only; nothing here changes that or enforces it anew).
- No denylist for internal admin planes or the control plane itself —
  only the universal, address-space-based cloud-metadata block.

None of these are silently deferred; each is a real gap, tracked in
`STATUS.md`'s doc 05 row.
