// Copyright (c) 2026 Overarching AI LLC
// SPDX-License-Identifier: Apache-2.0

// STATUS: interface only. Not implemented. See docs/ROADMAP.md.
//
// Package controlplane will hold the control plane: agent registry, model
// catalog, and tenant/org service — see docs/02-architecture.md §2
// (Control Plane). This package was not in the originally proposed layout;
// it's added because the component status table lists a Control plane
// stub, and every other stub component got its own pkg/ directory. See
// ARCHITECTURE.md "Repo layout notes" for why.
//
// Tool Registry, also listed under Control Plane in doc 02 §2, is NOT
// here — it got a real implementation in Milestone M2
// (pkg/toolregistry) before this package got any, so it lives in its own
// package rather than becoming the one implemented corner of a stub. See
// pkg/toolregistry's doc comment.
package controlplane

import "context"

// Registry is the shape a future control-plane implementation would expose.
// The signature is illustrative, not a commitment — not implemented.
type Registry interface {
	RegisterAgent(ctx context.Context, agentID, version string) error
}

// TODO: not implemented.
