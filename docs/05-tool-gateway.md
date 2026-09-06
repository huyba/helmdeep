# Doc 05 — Tool Gateway & Connectors

**The gateway is the only path from an agent to the outside world.** Everything the agent can affect passes through here, which makes it the primary enforcement surface and the primary performance risk.

---

## 1. Tool Model

A **Tool** is a governed, versioned, typed capability. Registration metadata (all mandatory):

```yaml
tool: netsuite.create_journal_entry
version: 3
owner: group:finance-platform
schema:
  input:  { $ref: "./schemas/je_input.json" }   # JSON Schema, strict, no additionalProperties
  output: { $ref: "./schemas/je_output.json" }
semantics:
  reversibility: compensable          # reversible | compensable | irreversible
  compensation: netsuite.void_journal_entry
  idempotency: key_supported          # key_supported | natural | none
  side_effect_scope: "financial.ledger"
risk:
  rating: high                        # drives default approval + tier-A runtime
  data_classes_read:  ["financial.ledger"]
  data_classes_write: ["financial.ledger"]
  pii: false
access:
  credential_mode: obo                # obo | agent_principal | shared  (doc 04 ladder)
  required_scopes: ["netsuite.journal:write"]
egress:
  destinations: ["*.netsuite.com"]
  max_payload_bytes: 262144
limits:
  rate: "60/min per agent"
  timeout_ms: 15000
  max_concurrent: 10
observability:
  redact_fields: ["$.memo", "$.attachments[*]"]
```

**Rule: no tool executes without a registered schema and reversibility classification.** Undeclared tools are how governance quietly dies; the platform refuses them.

### 1.1 Reversibility taxonomy — why it drives everything
| Class | Definition | Default treatment |
|---|---|---|
| `reversible` | No external effect, or trivially undone (read, draft, search) | Async action record; low friction; Tier-B runtime eligible |
| `compensable` | External effect with a defined inverse (create JE → void JE; send Slack → delete message) | Sync record; compensation handler registered; saga-managed |
| `irreversible` | Cannot be undone (send external email, wire transfer, delete prod data, publish) | Sync record **before** execution; approval gate below high trust; never auto at Level ≤2 |

This single field connects the runtime tier (doc 03), the approval policy (doc 09), and the rollback machinery (doc 06). It is the highest-leverage piece of metadata in the system.

---

## 2. Connector Architecture

```
Agent → host.tool.call → [Tool Gateway]
                            ├── Schema validation (input)
                            ├── PEP: policy decision (doc 04/09)
                            ├── Approval gate (if required) → suspend session
                            ├── Credential Broker: mint JIT credential
                            ├── Connector Adapter (per system)
                            ├── Egress Proxy: allowlist, TLS pin, DLP scan, size cap
                            ├── ← Response filter: schema validate, PII redact,
                            │     data-class tag, injection scan, truncate/reference
                            └── Action Record write
```

### 2.1 Connector types
1. **First-party connectors** — we build and maintain (Salesforce, NetSuite, Workday, ServiceNow, Jira, GitHub, Snowflake, M365, Slack). Deepest integration: OBO tokens, row-level filters, native idempotency, compensation handlers.
2. **MCP servers** — mount any MCP-compatible server as a tool namespace. This is now the de facto interop standard and we must be excellent at it, but MCP servers are **untrusted by default**: they run in their own sandbox, their tool descriptions are treated as untrusted content (a tool description is an injection vector), and their schemas are pinned by digest so a server cannot silently change a tool's meaning after approval.
3. **OpenAPI import** — generate a connector from a spec; auto-derive reversibility guesses (`GET`=reversible, `DELETE`=irreversible) but require human confirmation before production.
4. **Custom code tools** — tenant-authored, run in Tier-A sandboxes with their own egress allowlist.

### 2.2 MCP-specific hardening
- Tool descriptions and results are wrapped in untrusted-content delimiters before reaching the model, never concatenated as instructions.
- Digest pinning + change detection: "tool `search` changed its description since approval" → tool disabled pending re-approval (defends against rug-pull attacks).
- Per-server egress allowlist and resource caps; a compromised MCP server cannot reach anything but its declared destinations.
- Cross-server confusion prevention: tool names are namespaced (`server_id.tool`), and the model is never shown two tools whose names collide.

---

## 3. Egress Control

Default-deny. The sandbox has no route to the internet; the gateway does, and only to declared destinations.

| Control | Implementation |
|---|---|
| Destination allowlist | Per-tool FQDN allowlist, resolved through our DNS with pinning; no raw IPs |
| TLS | Certificate pinning for first-party connectors; MITM inspection where the customer's policy requires it |
| Payload inspection | DLP scan outbound: PII/secrets/data-class violation → block or redact per policy |
| Size & rate | Per-tool caps; global per-session egress byte budget (an exfiltration circuit breaker) |
| Protocol | HTTPS only; no arbitrary sockets; websockets only for explicitly approved tools |
| Blocked always | Cloud metadata endpoints, internal admin planes, the AgentOS control plane itself |

**Egress byte budget** deserves emphasis: even a perfectly authorized agent should not be able to stream a database out through a legitimate tool. A per-session cumulative outbound budget, with anomaly detection against the agent's historical baseline, catches the "authorized but abusive" case that policy alone misses.

---

## 4. Response Handling — the injection frontier

Every tool response is untrusted input. Pipeline:

1. **Schema validation** — reject/repair non-conforming responses before they reach the model.
2. **Injection scan** — classifier + heuristics for instruction-like content in data fields; on hit: mark the session `tainted_by_untrusted_content`, which activates restrictive policy branches (doc 04 §4.2) and requires approval for subsequent writes.
3. **Data-class tagging** — propagate the class of returned data into session context so downstream policy can reason about it (e.g. reading restricted data disables broad egress tools).
4. **PII handling** — redact or tokenize per policy before the content enters the model context; tokens are re-hydrated only inside the gateway on the way back out if the tool needs the real value.
5. **Size management** — responses over threshold are stored by reference with a summary inlined; the agent can request specific slices. Controls token cost and limits injection surface.

---

## 5. Performance

The gateway sits on every action, so its overhead is the platform's tax.

| Stage | Budget (p99) |
|---|---|
| Schema validation | 1ms |
| Policy decision (cached PDP) | 10ms |
| Credential mint (cached/warm) | 5ms (cold OBO exchange: 80ms, cached per session) |
| Response filtering (scan + tag) | 15ms |
| **Gateway overhead total** | **< 35ms** on top of the upstream call |

Techniques: sidecar PDP with local bundle; per-session credential caching (mint once per connector per session, not per call); async Action Record write for `reversible` ops; streaming pass-through for large reads; connection pooling per connector.

---

## 6. Reliability

- **Idempotency:** key = `hash(session_id, step_id, tool, args)`. Outcome cache (24h) consulted before re-issuing after ambiguous failure. For tools with `idempotency: none`, an ambiguous failure escalates to human confirmation rather than blind retry — the correct behavior for "did the wire transfer go through?"
- **Circuit breakers** per connector; open circuit surfaces to agents as a structured observation so they can adapt rather than crash.
- **Bulkheads:** per-connector concurrency pools so a slow ERP cannot starve the whole gateway.
- **Retry policy** is per-tool metadata, never a global default: retrying a `create_payment` is a bug.

---

## 7. Tool Lifecycle & Governance

```
propose → schema + risk review (security + data owner) → staging with synthetic data
        → canary (limited agents, mandatory approval) → GA → deprecation (versioned, N-1 supported)
```

- Tools are versioned; agents pin a major version. Breaking changes require a new major and an explicit agent-side migration.
- Every tool has an owning group and a review date; unused tools are auto-flagged for retirement (reducing standing attack surface).
- A **tool risk score** feeds the trust engine: an agent's autonomy ceiling can never exceed what its riskiest bound tool allows.

---

## 8. Open Questions

1. **Long-tail connectors.** Do we build a low-code connector builder for the customer's internal APIs (big investment, big moat) or rely on OpenAPI/MCP import? **Leaning:** OpenAPI/MCP import for v1, builder in v2 driven by demand data.
2. **Browser-use tools.** UC-3-style agents want a browser. A headless browser is the largest egress and injection surface in the product. **Leaning:** ship it only in Tier-A with its own network namespace, per-domain allowlist, and mandatory taint marking — or not at all in v1.
3. **Write-side simulation.** For Trust Level 0 ("observe"), we need every write tool to have a dry-run mode. Do we require connector authors to implement `simulate()`? That doubles connector work but makes progressive trust real. **Leaning:** required for `irreversible`, optional otherwise.
