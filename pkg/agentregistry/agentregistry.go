// Copyright (c) 2026 Overarching AI LLC
// SPDX-License-Identifier: Apache-2.0

// Package agentregistry is the Agent Registry (docs/02-architecture.md §2's
// Control Plane component catalog; docs/04-identity-authz.md's "Agent
// Definition... Permanent (versioned)"; docs/09-governance-trust.md §6):
// the governed catalog of agents a deployment has actually declared, as
// distinct from whatever identity a presented credential happens to
// assert. Mirrors pkg/toolregistry's own shape and reasoning almost
// exactly — see that package's doc comment — because the same governance
// question applies to *who* is calling as already applied to *what* is
// being called: an agent identity absent from this registry is refused,
// the same way an undeclared tool already is (Milestone M2).
//
// This is a deliberately narrow slice of doc 09 §6's full lifecycle model
// (owning group, business purpose, review dates, decommission plans,
// onboarding review, periodic recertification, orphan detection — none of
// that exists here) — see docs/adr/0012-agent-registry.md for the scoping
// reasoning, following the same principle every prior ADR in this repo
// has used: don't add a field before something reads it.
//
// This is a separate package from pkg/controlplane on purpose — see that
// package's own doc comment, and pkg/toolregistry's, which established
// the same split first: pkg/controlplane remains the interface-only stub
// for the *other* control-plane services (Model Catalog, Trust Engine,
// Approval Service, Eval Service, Tenant/Org Service, Policy Service);
// the Agent Registry needed a real implementation for this milestone and
// grew its own home rather than becoming the one exception inside a
// catch-all stub.
package agentregistry

import "fmt"

// Entry is one governed agent's registration metadata — the narrow slice
// of doc 02 §2's full Agent Registry model ("definitions, versions,
// owners, dependencies, autonomy ceiling") this milestone implements.
type Entry struct {
	// AgentID matches types.Subject.ID exactly (e.g. "agent:support-bot") —
	// the join key between this registry and a resolved caller identity.
	AgentID string
	// Version, if non-empty, is the only agent_version a calling instance
	// may assert (types.Subject.AgentVersion, from the SIT's own
	// `agent_version` claim) to be accepted. Left empty, any asserted
	// version — including none at all — is accepted; this is the
	// pre-Agent-Registry-milestone behavior, preserved for entries that
	// haven't opted into version pinning, exactly like
	// pkg/toolregistry.Entry.Upstream's own empty-means-skip convention.
	Version string
	// Owner is the owning team or individual (doc 09 §6: "every agent has
	// an owning group") — descriptive metadata for an operator reading the
	// config; internal/gateway does not currently act on it.
	Owner string
	// Risk is the agent's declared risk tier, using the same four-tier
	// scale as pkg/toolregistry.RiskRating (doc 09 §6's "risk
	// classification") — descriptive metadata today; nothing compares it
	// against a policy threshold yet.
	Risk string
	// DataClasses names the categories of data this agent is approved to
	// touch (doc 09 §6's "data-class inventory") — descriptive metadata
	// today, not cross-checked against what any tool call actually reads
	// or writes.
	DataClasses []string
}

// Registry is the config-backed Agent Registry. Safe for concurrent read
// access after construction; there is no runtime mutation, matching
// pkg/toolregistry.Registry.
type Registry struct {
	byID map[string]Entry
}

// New builds a Registry from entries, rejecting an entry with a missing
// AgentID or a duplicate AgentID outright — a misconfigured registry
// should fail loudly at startup, matching pkg/toolregistry.New's own
// convention.
func New(entries []Entry) (*Registry, error) {
	byID := make(map[string]Entry, len(entries))
	for _, e := range entries {
		if e.AgentID == "" {
			return nil, fmt.Errorf("agent registry entry has an empty agent id")
		}
		if _, exists := byID[e.AgentID]; exists {
			return nil, fmt.Errorf("agent %q is registered more than once", e.AgentID)
		}
		byID[e.AgentID] = e
	}
	return &Registry{byID: byID}, nil
}

// Lookup returns the registered entry for agentID, or false if the agent
// has never been declared to the registry at all — the case
// internal/gateway's own "undeclared agents are refused" rule exists for,
// enforced there, not by this package (Registry only answers "is this
// declared," not "should this be allowed" — matching
// pkg/toolregistry.Registry.Lookup's identical division of labor).
func (r *Registry) Lookup(agentID string) (Entry, bool) {
	e, ok := r.byID[agentID]
	return e, ok
}
