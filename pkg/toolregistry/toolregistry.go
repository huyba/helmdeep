// Copyright (c) 2026 Overarching AI LLC
// SPDX-License-Identifier: Apache-2.0

// Package toolregistry is the Tool Registry (docs/05-tool-gateway.md §1):
// the governed catalog of tools a deployment has actually reviewed and
// declared, as distinct from whatever an upstream MCP server happens to
// expose over tools/list. Doc 05's own rule is absolute: "no tool executes
// without a registered schema and reversibility classification.
// Undeclared tools are how governance quietly dies; the platform refuses
// them." Milestone M2 implements a deliberately narrow slice of that: tool
// ID, owning upstream, risk rating, data classes, and required scopes —
// not the full schema/semantics/egress/limits/observability metadata doc
// 05 describes, because nothing in this repo consumes those yet. See
// docs/adr/0008-tool-registry.md for the scoping reasoning, following the
// same principle docs/adr/0004 and docs/adr/0007 already established:
// don't add a field before something reads it.
//
// This is a separate package from pkg/controlplane on purpose — see that
// package's own doc comment. pkg/controlplane remains the interface-only
// stub for the *other* control-plane services (Agent Registry, Model
// Catalog, Trust Engine, Tenant/Org Service); the Tool Registry needed a
// real implementation for this milestone and grew its own home rather
// than becoming the one exception inside a catch-all stub.
package toolregistry

import "fmt"

// RiskRating is a tool's declared risk tier (docs/05-tool-gateway.md §1).
// It drives policy decisions (Milestone M2) and, eventually, default
// approval requirements and runtime tier selection (doc 05 §1.1) — neither
// of the latter two exists yet.
type RiskRating string

const (
	RiskLow      RiskRating = "low"
	RiskMedium   RiskRating = "medium"
	RiskHigh     RiskRating = "high"
	RiskCritical RiskRating = "critical"
)

func (r RiskRating) valid() bool {
	switch r {
	case RiskLow, RiskMedium, RiskHigh, RiskCritical:
		return true
	default:
		return false
	}
}

// Entry is one governed tool's registration metadata — the narrow slice of
// docs/05-tool-gateway.md §1's full schema this milestone implements.
type Entry struct {
	// ToolID matches the name the tool is called by over MCP
	// (types.Action.Tool / types.Tool.Name) — the join key between this
	// registry and pkg/mcp.Registry's routing table.
	ToolID string
	// Upstream is the upstream MCP server this tool is allowed to reach —
	// Milestone M3's per-tool egress destination (doc 05-tool-gateway.md
	// §3). internal/gateway.Gateway.CallTool enforces it: a call is
	// refused if pkg/mcp.Registry would actually route it somewhere else.
	// Left empty, the check is skipped (backward compatible with entries
	// written before M3 — see docs/adr/0008-tool-registry.md).
	Upstream string
	// Risk is the tool's declared risk tier.
	Risk RiskRating
	// DataClasses names the categories of data this tool reads or writes
	// (docs/05-tool-gateway.md §1 splits read/write; this milestone
	// doesn't need that distinction yet, so one list covers both — split
	// it when a policy actually needs to tell them apart).
	DataClasses []string
	// Scopes is what a caller must hold (types.Subject.Scopes) to be
	// eligible to call this tool at all. A policy is what actually
	// compares the two lists; the gateway only carries this value through
	// as governance metadata (types.Action.RequiredScopes).
	Scopes []string
}

// Registry is the config-backed Tool Registry. Safe for concurrent read
// access after construction; there is no runtime mutation (no
// Milestone-M2 requirement needs hot-reloading the tool list the way
// pkg/policy.Loader hot-reloads policy).
type Registry struct {
	byID map[string]Entry
}

// New builds a Registry from entries, rejecting duplicate tool IDs and any
// entry with a missing ID or an invalid risk rating outright — a
// misconfigured registry entry should fail loudly at startup, not silently
// admit a tool with a risk rating no policy will recognize.
func New(entries []Entry) (*Registry, error) {
	byID := make(map[string]Entry, len(entries))
	for _, e := range entries {
		if e.ToolID == "" {
			return nil, fmt.Errorf("tool registry entry has an empty tool id")
		}
		if !e.Risk.valid() {
			return nil, fmt.Errorf("tool %q: invalid risk rating %q (want one of: low, medium, high, critical)", e.ToolID, e.Risk)
		}
		if _, exists := byID[e.ToolID]; exists {
			return nil, fmt.Errorf("tool %q is registered more than once", e.ToolID)
		}
		byID[e.ToolID] = e
	}
	return &Registry{byID: byID}, nil
}

// Lookup returns the registered entry for toolID, or false if the tool has
// never been declared to the registry at all — the case
// docs/05-tool-gateway.md §1's "undeclared tools are refused" rule exists
// for, enforced by internal/gateway, not by this package (Registry only
// answers "is this declared," not "should this be allowed").
func (r *Registry) Lookup(toolID string) (Entry, bool) {
	e, ok := r.byID[toolID]
	return e, ok
}
