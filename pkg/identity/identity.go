// Copyright (c) 2026 Overarching AI LLC
// SPDX-License-Identifier: Apache-2.0

// STATUS: interface only. Not implemented. See docs/ROADMAP.md.
//
// Package identity will hold the real identity & credential exchange
// component: workload identity (SPIFFE/SPIRE), OBO token exchange, and the
// credential broker that mints just-in-time, narrowly scoped credentials —
// see docs/04-identity-authz.md.
//
// The Tool Gateway built in Step 2 does not depend on this package. It
// resolves caller identity through a minimal static token→identity map that
// lives in internal/gateway and happens to satisfy the Resolver interface
// below, so the real implementation can later be swapped in here without
// changing the gateway's calling code.
package identity

import (
	"context"

	"github.com/huyba/helmdeep/pkg/types"
)

// Resolver turns a presented credential into a Subject.
type Resolver interface {
	Resolve(ctx context.Context, credential string) (types.Subject, error)
}

// TODO: not implemented.
