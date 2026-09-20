// Copyright (c) 2026 Overarching AI LLC
// SPDX-License-Identifier: Apache-2.0

// Package identity holds the identity & credential exchange component:
// verifying agent identity from a Session Identity Token (SIT, see
// docs/04-identity-authz.md §1.1) and the client side of the Credential
// Broker's on-behalf-of token exchange (§3). Workload identity via
// SPIFFE/SPIRE is not implemented — JWTResolver's claim shape is a subset
// chosen to stay SPIFFE-compatible so a SPIRE-backed implementation can
// replace it later without changing callers, but SPIRE itself is not
// integrated. See docs/adr/0007-credential-broker-scope.md for what's
// deliberately out of scope in this milestone and why.
package identity

import (
	"context"

	"github.com/huyba/helmdeep/pkg/types"
)

// Resolver turns a presented credential into a Subject.
type Resolver interface {
	Resolve(ctx context.Context, credential string) (types.Subject, error)
}

// CredentialBroker mints a short-lived, narrowly-scoped downstream
// credential for one upstream tool call, given the caller's own verified
// credential (its SIT) — doc 04 §3's on-behalf-of exchange. The agent
// itself never sees the result; only the gateway does, and only for the
// duration of one call.
type CredentialBroker interface {
	Exchange(ctx context.Context, subjectCredential, upstream, tool string) (token string, err error)
}
