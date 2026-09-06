# Doc 11 — Security Architecture & Threat Model

**Framing:** an AI agent is a confused-deputy machine by construction. It holds delegated authority, consumes untrusted input, and takes actions — which is the textbook setup for privilege abuse. The platform's job is to ensure that *when* (not if) the model is manipulated, the damage is bounded, detected, and reversible.

---

## 1. Trust Boundaries

```
[Untrusted]  Web content · Tool responses · Retrieved documents · Third-party MCP servers
             · User-supplied prompts · Model outputs  ← YES, model output is untrusted
      ║
      ║ (all crosses a gateway with classification + taint marking)
      ▼
[Semi-trusted]  Agent code (tenant-authored) · Agent prompts · Sandbox interior
      ║
      ║ (host ABI — no direct I/O, no credentials, no network)
      ▼
[Trusted]  Gateways · PDP/PEP · Credential Broker · Orchestrator · Control plane · Ledger
```

**The rule that follows:** no security decision is ever made inside the semi-trusted zone. Not by the model, not by the agent code, not by a prompt instruction. Anything the model "decides" about permissions is advisory at best.

---

## 2. Threat Catalog

| ID | Threat | Impact | Primary controls |
|---|---|---|---|
| **T1** | **Indirect prompt injection** — malicious instructions in a fetched page, email, ticket, or document | Agent performs attacker-chosen actions with real authority | Untrusted-content wrapping; injection detection → session taint; taint-restricted policy; approval gates for writes after taint; egress budget |
| **T2** | **Data exfiltration via legitimate tools** — authorized reads streamed out through an allowed channel | Mass data loss without any policy violation | Per-session egress byte budget; DLP on outbound; data-class-aware egress rules; anomaly detection vs. agent baseline; no broad egress tool after restricted read |
| **T3** | **Confused deputy / privilege escalation** — agent leveraged to act beyond the requester's rights | Unauthorized access; compliance breach | Strict attenuation (doc 04 §2.1); OBO tokens; permission-aware retrieval; no ambient credentials |
| **T4** | **Sandbox escape** — code-executing agent breaks isolation | Host/tenant compromise | microVM per session; seccomp; no IMDS; read-only rootfs; recycle-never-reuse for Tier A; continuous escape testing |
| **T5** | **Supply chain** — malicious package, model, or MCP server | Backdoor with agent authority | Signed artifacts + digest pinning; dependency allowlist and private mirror; MCP schema pinning + change detection; SBOM |
| **T6** | **Memory poisoning** — attacker plants content that persists into episodic/semantic memory | Long-lived, cross-session influence | Explicit-write-only memory; provenance per item; quarantine of memory written during tainted sessions; review of high-influence items |
| **T7** | **Agent identity theft / token replay** | Impersonation | Short-lived audience-bound SITs; workload attestation (SPIFFE); revocation bloom filter; mTLS everywhere |
| **T8** | **Resource exhaustion / cost attack** — loop or adversarial input burning budget | Financial DoS | Hierarchical budgets; loop detection; spend velocity alarms; admission control; per-tenant fair queuing |
| **T9** | **Multi-agent manipulation** — one compromised agent influences another via shared state or messages | Lateral movement in agent space | Typed contracts between agents; taint propagation across spawn; no raw episodic sharing; delegation depth caps |
| **T10** | **Insider misuse** — an employee uses an agent to exceed their own access, or to obscure attribution | Undetected policy circumvention | Delegation chain in every record; approver ≠ initiator; anomaly detection on human-agent pairs; access reviews |
| **T11** | **Model output manipulation of the audit narrative** — agent writes misleading logs/summaries | Corrupted evidence | Audit records are written by the platform, never by the agent; agent-authored text is stored as *claims*, distinguished from *facts* |
| **T12** | **Control-plane compromise** | Total | Separate blast radius, strict RBAC, break-glass with dual approval, immutable ledger anchoring, separate key custody |

---

## 3. Prompt Injection: Layered Defense (no single control works)

1. **Structural separation** — instructions and data travel in different channels; untrusted spans are delimited and labeled at the gateway, never string-concatenated by agent code (the SDK makes the safe path the default path).
2. **Detection** — classifier + heuristics on newly introduced untrusted content; on hit, set `session.tainted_by_untrusted_content`.
3. **Taint-based policy** — the decisive control. A tainted session loses access to restricted data classes, loses broad-egress tools, and requires approval for any write (doc 04 §4.2). This converts a model-level attack into a policy-level non-event.
4. **Capability minimization** — an agent that only needs read access never holds write scope, so a successful injection has nothing to steer.
5. **Action-level verification** — for high-risk actions, an independent verifier (different model/prompt, no exposure to the untrusted content) checks that the proposed action matches the original task intent. Expensive; reserved for `irreversible`.
6. **Egress budget** — even a fully-fooled agent cannot move more than N bytes to the outside.
7. **Detection & response** — anomalous action sequences vs. the agent's behavioral baseline trigger auto-containment.

**Honest position for the security review:** we do not claim to prevent prompt injection. We claim to bound its blast radius, detect it, and make it reversible. Any vendor claiming prevention should be distrusted; state this in the sales conversation because CISOs already know it.

---

## 4. Secrets & Key Management

- No secrets in agent code, prompts, environment, or memory — enforced by scanning at registration and at runtime write paths.
- Vault-backed connector credentials, never exposed outside the gateway process; automatic rotation.
- Per-tenant encryption keys (BYOK/HYOK for regulated tiers) for payloads, memory, and journals.
- Runtime signing keys held in enclave/HSM; Action Record signatures verifiable without platform cooperation.

---

## 5. Network & Infrastructure

- Zero-trust internal: mTLS + SPIFFE identity between all services; no network-position-based trust.
- Sandbox network namespace with default-deny egress; only vsock/unix socket to the host gateway.
- Control plane in a separate network and account/subscription boundary from data plane; one-way telemetry flow.
- Cloud metadata endpoints blocked at multiple layers (this is the most common cloud escape path and deserves redundant blocking).

---

## 6. Detection & Response

**Behavioral baselining per agent version:** typical tool sequences, data classes touched, egress volume, retrieval patterns, timing. Deviations score into a session risk value that policy consumes in real time (a high-risk session gets stricter rules mid-flight).

**Automated containment ladder:**
```
anomaly → increase scrutiny (force approval for writes)
        → suspend session
        → freeze agent version (all sessions)
        → revoke connector credentials (blast-radius scope)
        → tenant-wide kill switch
```
Target: automated containment within **60 seconds** of detection; credential revocation propagated in **< 5 seconds** (doc 04 §5).

**Incident playbooks (pre-written, exercised quarterly):**
- Suspected injection → identify tainted sessions, enumerate actions taken, evaluate compensation, notify data owners.
- Credential compromise → revoke, rotate, replay Action Records to enumerate exposure.
- Sandbox escape → isolate host, snapshot for forensics, rotate node identities, assume tenant data exposure until proven otherwise.
- Rogue agent behavior → freeze version, replay against previous version to characterize the difference, roll back.

**Forensic advantage:** because every action is recorded with its delegation chain and the journal is deterministic, incident scoping is a *query*, not an investigation. This is genuinely differentiating — "we can tell you within minutes exactly what that agent touched" is a strong claim in a market where most teams cannot.

---

## 7. Secure SDLC for the Platform Itself

Signed builds and reproducible artifacts; SBOM per release; dependency pinning with a private mirror; mandatory review for policy-engine and credential-broker changes; red-team exercises against the sandbox and the injection defenses each release; bug bounty with a specific agent-security scope; annual third-party pen test with published summary (a purchase requirement for our buyer).

---

## 8. Open Questions

1. **Independent action verification** (§3.5) roughly doubles cost on high-risk actions. Which action classes justify it — probably `irreversible` above a value threshold, but the threshold needs customer input.
2. **Taint granularity.** Session-level taint is coarse and will annoy users (one bad web page degrades a whole session). Span-level taint tracking through model reasoning is not reliably possible today. **Leaning:** session-level with an explicit "sanitize and continue" step that requires human confirmation.
3. **Tenant-authored code in Tier A** — how much do we allow? Arbitrary code broadens the escape surface enormously. **Leaning:** allow, with microVM + strict egress, and price it to reflect the isolation cost.
4. **Adversarial evaluation cadence** — injection techniques evolve weekly. Continuous adversarial suite from a maintained corpus, refreshed how often, and by whom? This needs an owner, not just a process.
