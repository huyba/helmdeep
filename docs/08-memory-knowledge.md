# Doc 08 — Memory & Knowledge

**Owns:** what an agent knows — within a session, across sessions, and across the organization — with permissions enforced at retrieval time.

The core risk here is simple and severe: **a flattened index destroys enterprise permissions.** If an agent can retrieve a document the requesting user cannot read, the platform has manufactured a data breach out of a search feature.

---

## 1. Memory Tiers

| Tier | Scope | Lifetime | Store | Example |
|---|---|---|---|---|
| **Working** | One session | Session | In-sandbox + journal | Current conversation, recent tool results |
| **Episodic** | Agent × subject (user, ticket, account) | Configurable (30–365d) | Postgres + vector | "Last time we handled this customer, they wanted X" |
| **Semantic** | Organization | Long-lived | Vector + graph + object store | Policy docs, runbooks, product knowledge |
| **Procedural** | Agent definition | Version-bound | Registry artifacts | Learned exemplars, tuned prompts, tool-use patterns |

### 1.1 Working memory
Managed as a budget (doc 03 §3.3): eviction by recency+relevance, compaction by summarization, large artifacts stored by reference. Everything evicted remains in the journal — compaction never destroys audit evidence, only model context.

### 1.2 Episodic memory
Written explicitly via `host.memory.write(scope, item)`, never implicitly. Reasons: implicit memory writes create unbounded, ungoverned data collection; explicit writes are policy-checkable, attributable, and deletable.

Each item carries: `subject` (what it's about), `data_classes`, `source_action_id` (provenance), `visibility` (who may retrieve it), `confidence`, `ttl`. Contradiction handling: newer items supersede older with the same `(subject, key)`, but the superseded item is retained with an end-timestamp — agents should be able to answer "what did we believe last month?"

**Deletion is first-class.** GDPR/CCPA erasure must propagate: subject-keyed indexes make "delete everything about person X" a bounded operation, including derived summaries (which store their source item IDs).

### 1.3 Semantic memory (organizational knowledge)
Ingestion pipeline: connector → chunking (structure-aware, not naive fixed-size) → enrichment (title/section path, entities, data class, freshness) → embedding → index. **ACL capture happens at ingestion and is refreshed continuously** — see §2.

### 1.4 Procedural memory
Approved/edited human decisions become candidate exemplars. They do **not** silently modify agent behavior: candidates enter a review queue, are validated against the eval suite, and are promoted as a new agent version. Learning is versioned and reversible — the alternative (agents that quietly drift) is unacceptable in a governed platform.

---

## 2. Permission-Aware Retrieval

Three strategies, used in combination:

| Strategy | How | Pros/Cons |
|---|---|---|
| **Pre-filter (primary)** | Resolve the requester's accessible-document set (ReBAC/Zanzibar) → filter the vector search by that set | Correct; requires efficient set representation for users with millions of docs |
| **Post-filter (safety net)** | Retrieve top-K, then check each document's ACL before returning | Simple, but leaks via ranking side-channels and wastes recall |
| **Index partitioning** | Separate indexes per major security boundary (data class, region, sensitive project) | Strong isolation; index sprawl |

**Design:** partition by data class + residency, pre-filter within a partition using a cached ACL bitmap/roaring set, and post-filter as a final assertion (defense in depth — a mismatch between pre- and post-filter results raises a security alarm, because it means the ACL cache is stale).

### 2.1 Whose permissions?
**The end user's effective permissions**, not the agent's — resolved from the delegation chain root (doc 04). For unattended agents with no human in the chain, retrieval uses the Delegated Authority Grant's explicit document scope, which must be *enumerable and reviewable*, not "everything the service account can see."

### 2.2 ACL freshness
Permission changes must take effect fast. We consume change events from source systems where available (Drive/SharePoint/Confluence webhooks), and re-verify on retrieval with a consistency token. Where a source system offers no change feed, we set a shorter cache TTL and mark the corpus `acl_staleness: high` — visible in the tool registry, because a data owner deserves to know.

### 2.3 Inference leakage
Even with correct ACLs, aggregated answers can leak (e.g. summarizing 200 permitted documents reveals a pattern the user shouldn't know). Mitigations: cap the breadth of a single retrieval for sensitive classes, tag outputs derived from restricted sources so egress policy applies, and require approval for cross-class synthesis. This is an acknowledged partial mitigation, not a solved problem.

---

## 3. Retrieval Quality

Because a governed platform's answers are only as good as its retrieval:
- **Hybrid search** (BM25 + dense) with reciprocal-rank fusion; keyword recall matters enormously for enterprise jargon and IDs.
- **Rerank** with a cross-encoder on the top ~50.
- **Structure-aware chunking** with parent-document expansion (retrieve the chunk, return the section).
- **Freshness weighting** and explicit staleness signals — an outdated policy document confidently retrieved is worse than no answer.
- **Query understanding**: decomposition for multi-hop questions, entity resolution against the org graph.
- **Grounding contract**: retrieved spans are returned with citations; agents are required (by output schema) to cite what they used, enabling automated groundedness checks (doc 10).

---

## 4. Knowledge Graph Layer (light)

A pragmatic entity graph — people, teams, systems, customers, tickets, documents — with relationships. Used for: entity resolution, ACL inference, multi-hop retrieval, and blast-radius analysis during incidents ("what did this agent touch, and what else references it?"). Deliberately *light*: we do not attempt a full enterprise ontology, which is where knowledge-graph projects go to die.

---

## 5. Storage & Scale

- Vector index: pluggable (pgvector for small tenants; a dedicated ANN store beyond ~50M vectors). Sharded by tenant, then by partition.
- Multi-tenancy: hard tenant isolation at the index level — no shared index with a tenant filter, because a filter bug is a cross-tenant breach.
- Embedding version migrations are a real operational burden: dual-write + shadow-read + background backfill, with the model version recorded per vector.
- Cost: embedding storage and re-embedding on model upgrades dominate. Content-addressed caching plus tiering (hot/warm/cold corpora) keeps it bounded.

---

## 6. Open Questions

1. **Do we own the vector store or integrate the customer's?** Owning gives us permission enforcement and quality control; integrating reduces sales friction and data duplication. **Leaning:** integrate as primary (customers already have one), own an optional managed tier — but we always enforce permission filtering ourselves rather than trusting theirs.
2. **Cross-agent memory sharing.** Institutional knowledge is valuable precisely because it compounds across agents — but it is also how one agent's compromised context poisons another. **Leaning:** sharing allowed only through *reviewed, promoted* semantic memory; never raw episodic sharing.
3. **Memory poisoning.** An attacker who plants content that gets written to episodic memory has a persistent foothold. Defenses: provenance on every memory item, taint propagation (memory written during a tainted session is quarantined), and periodic review of high-influence memory items. This deserves its own hardening pass in doc 11.
4. **Right to explanation.** Under EU AI Act-style requirements, can we always reconstruct which knowledge influenced a decision? Citations + journal say mostly yes; model-internal influence says not fully. We should claim only what we can prove.
