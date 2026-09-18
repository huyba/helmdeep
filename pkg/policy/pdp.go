// Copyright (c) 2026 Overarching AI LLC
// SPDX-License-Identifier: Apache-2.0

// Package policy defines the Policy Decision Point (PDP) the Tool Gateway
// consults before every tool call.
//
// STATUS: interface only for Step 1. An embedded OPA/Rego implementation
// lands in Step 2 — see docs/adr/0002-policy-engine-choice.md and
// ROADMAP.md.
package policy

import (
	"context"

	"github.com/huyba/helmdeep/pkg/types"
)

// Decider evaluates a single DecisionRequest and returns a verdict. It must
// be safe for concurrent use: the gateway calls it on every tool call, from
// many sessions at once.
type Decider interface {
	Decide(ctx context.Context, req types.DecisionRequest) (types.DecisionResponse, error)
}

// Loader loads a policy bundle from disk and supports replacing it at
// runtime (hot reload) without restarting the gateway or interrupting
// in-flight decisions.
type Loader interface {
	Load(path string) error
	Reload() error
}
