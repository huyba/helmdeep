# Doc 04 — Identity & Authorization

**The hardest unsolved problem in enterprise agents.** Existing IAM assumes a principal is either a human or a static service. An agent is a *delegated, ephemeral, semi-autonomous* principal whose authority must be narrower than the human who invoked it and must shrink further as it delegates onward.

---

## 1. Identity Model

Four principal types, all first-class:

| Type | Example | Issued by | Lifetime |
|---|---|---|---|
| **Human** | `user:marcus@corp.com` | Enterprise IdP (OIDC) | Session-bound |
| **Agent Definition** | `agent:invoice_recon` | Agent Registry | Permanent (versioned) |
| **Agent Instance** | `agent_instance:ai_01J...` (one running session) | Runtime, at session start | Session-bound, minutes–hours |
| **Workload** | `spiffe://agentos/runtime/us-west-2/node-17` | SPIRE | Rotating (hours) |

**Key idea:** authorization decisions are made against the *agent instance*, evaluated in the context of its **delegation chain**, not against a shared service account.

### 1.1 Agent identity issuance
At session start, the runtime attests the sandbox (workload identity via SPIRE, plus a measurement of the agent artifact digest) and receives a short-lived **Session Identity Token (SIT)**:

```jsonc
{
  "sub": "agent_instance:ai_01J8XK...",
  "agent": "agent:invoice_recon",
  "agent_version": "sha256:9f3a...",       // binds identity to exact code+prompt
  "delegation_chain": [
    {"principal": "user:marcus@corp.com", "auth_time": "...", "method": "oidc+mfa"},
    {"principal": "agent:workflow_onboarding", "version": "sha256:11c...", "instance": "ai_01J8XA..."}
  ],
  "scope": ["netsuite.journal:write", "kb.search:read"],   // attenuated union
  "constraints": {
    "data_classes": ["financial.ledger"],
    "row_filter": "entity_id IN (ent_44, ent_45)",
    "trust_level": 2,
    "residency": "us"
  },
  "aud": "agentos-gateways",
  "exp": "<= 15 min, refreshed by runtime while session live>"
}
```

The SIT is **audience-restricted to our own gateways** and is worthless outside them. It is never a credential for a downstream system.

---

## 2. Delegation & Attenuation

### 2.1 The attenuation rule (invariant)
> A delegated principal's effective authority is always a **subset** of the delegator's, intersected with the agent definition's declared maximum scope, intersected with the trust-level ceiling.

```
effective = human_permissions
          ∩ parent_agent_effective        (if spawned by an agent)
          ∩ agent_definition.max_scope    (declared in agent.yaml, reviewed at registration)
          ∩ trust_level.allowed_actions
          ∩ policy_constraints            (contextual: time, geo, risk score)
```

Monotonic narrowing only. There is no mechanism to *widen* authority mid-chain — privilege escalation is structurally impossible rather than policy-prevented.

### 2.2 Delegation depth & fan-out
- `max_delegation_depth` (default 3) — prevents unbounded agent recursion.
- Each spawn requires explicit `scope_attenuation` from the parent; a parent cannot pass its full scope implicitly.
- The chain is carried in every Action Record, so an auditor can answer "which human's authority ultimately backed this write?" — the question compliance actually asks.

### 2.3 Autonomous / unattended agents
Scheduled or event-triggered agents have no human in the chain at runtime. They instead run under a **Delegated Authority Grant**: a human owner explicitly grants a bounded, expiring authority ("this agent may act with these scopes on this schedule until 2027-01-01, reviewed quarterly"). The grant is itself an approved, audited artifact — the same shape as a service-account approval, but expiring and attributable.

---

## 3. Credential Broker

**Principle: the agent never sees a downstream credential.**

```
Agent → host.tool.call("netsuite.create_journal_entry", args)
      → Tool Gateway: PDP decision (allow_with_approval)
      → Approval obtained
      → Credential Broker: mint JIT credential for THIS call
          - Prefer: OAuth token exchange (RFC 8693) producing a downstream token
            carrying the end user's identity + reduced scope
          - Else:   short-lived per-tenant connector credential from vault,
                    used only inside the gateway process
      → Gateway executes call, strips credential from any logged artifact
      → Credential invalidated/discarded; usage recorded
```

**Credential ladder, best to worst — always take the highest available:**
1. **On-behalf-of token exchange** — downstream system sees the real end user; its own RBAC applies; audit lines up. Best possible outcome.
2. **Agent-specific service principal** with narrow scopes, per (tenant, agent, connector), rotated automatically.
3. **Shared connector credential** with gateway-enforced row/field filtering — last resort, flagged in the tool registry as elevated risk, and requires compensating controls (mandatory approval or read-only).

We publish this ladder to customers because it drives *their* integration work: pushing connectors from level 3 to level 1 is the highest-leverage security improvement they can make.

---

## 4. Authorization Model

Layered, because no single model covers it:

| Layer | Model | Example |
|---|---|---|
| **Coarse capability** | RBAC | "agents in `finance-ops` may bind `netsuite.*` tools" |
| **Contextual** | ABAC | "deny writes outside 06:00–20:00 local, or if session risk score > 0.7, or if injection detector fired" |
| **Relationship** | ReBAC (Zanzibar-style) | "may read documents where user is in `viewers` of the parent folder" — required for permission-aware RAG (doc 08) |
| **Data-level** | Row/column filters + masking | "row_filter: entity_id IN (…)", "mask SSN" |
| **Temporal** | JIT + expiring grants | "elevated scope for 30 min, tied to approval `apr_…`" |

### 4.1 Policy evaluation
Cedar/OPA policies compiled into signed bundles, evaluated by a sidecar PDP (<10ms p99). ReBAC checks hit a Zanzibar-style relation store with an aggressive local cache and consistency tokens (`zookie`) so we do not authorize on stale relations after a permission revocation.

### 4.2 Example policy (illustrative)
```cedar
// Irreversible financial writes: dual control below trust level 3
forbid (
  principal in AgentInstance,
  action == Action::"netsuite.create_journal_entry",
  resource
) unless {
  principal.trust_level >= 3 ||
  context.approval.status == "approved" &&
  context.approval.approver != principal.delegation_chain.root
};

// Never let an agent read restricted data after it has touched untrusted web content
forbid (principal, action == Action::"memory.query", resource)
when { resource.data_class == "restricted" &&
       principal.session.tainted_by_untrusted_content };
```

The second rule is the **taint-tracking** control — see doc 11. It is the single most effective structural defense against injection-driven exfiltration, and it must live in the authorization layer, not in a prompt.

---

## 5. Revocation & Containment

Revocation must be **fast and complete**, since agents act in seconds:

- SIT lifetime ≤ 15 min, but revocation cannot wait for expiry → gateways consult a **revocation bloom filter** pushed over pub/sub, checked in-process (sub-ms).
- Containment scopes: single session · agent version · agent · tenant-wide · connector-wide (kill all access to Salesforce across all agents).
- Target: **< 5 seconds** from "revoke" click to last gateway enforcing it. Measured continuously as an SLO, because it is the number a CISO asks about.
- Revocation triggers automatic compensation evaluation for in-flight compensable actions (doc 06).

---

## 6. Integration with Enterprise IdP

- Human auth: OIDC / SAML to Okta, Entra ID, Ping. SCIM for group sync (groups drive RBAC).
- Agent registration requires an owning group, not an individual — orphaned agents are a real operational problem when people leave.
- Quarterly **access review** export: every agent, its effective scopes, its grants, its last use — in the format access-review tooling ingests (this is a purchase criterion for regulated buyers, not a nice-to-have).
- Joiner/mover/leaver: when a human's permissions shrink, all agents whose chains root at that human are re-evaluated within one sync cycle; agents acting under a departed employee's grant are suspended, not silently continued.

---

## 7. Open Questions

1. **Standardization risk.** Agent identity standards are emerging fast. Do we bet on an emerging spec or ship our own SIT format with a translation layer? **Leaning:** ship ours, keep it a thin, mappable JWT profile.
2. **Downstream SaaS support for token exchange is uneven.** Many enterprise SaaS products cannot represent "user X via agent Y." Do we invest in per-vendor adapters, or accept level-2 credentials with gateway-side filtering for the long tail?
3. **Attenuation for natural-language scopes.** When an agent spawns a sub-agent for a task described in text, how do we derive the minimal scope? Static declaration is safe but rigid; LLM-derived scope is flexible but untrustworthy. **Leaning:** static declaration in v1; LLM *proposes*, human/policy approves, in v2.
4. **Break-glass.** Emergency access for humans is standard; is there ever a break-glass for agents? **Leaning:** no. Humans break glass, agents do not.
