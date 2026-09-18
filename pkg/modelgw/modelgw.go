// Copyright (c) 2026 Overarching AI LLC
// SPDX-License-Identifier: Apache-2.0

// STATUS: interface only. Not implemented. See docs/ROADMAP.md.
//
// Package modelgw will hold the Model Gateway: routing, version pinning,
// caching, quotas, redaction, and provider fallback for model calls — see
// docs/07-model-gateway.md. The Tool Gateway does not call this package;
// it exists so a future agent runtime built in this monorepo has an
// interface to implement against.
package modelgw

import "context"

// Client is the shape a future model-gateway implementation would expose.
// The signature is illustrative, not a commitment — not implemented.
type Client interface {
	Complete(ctx context.Context, provider, model string, request []byte) (response []byte, err error)
}

// TODO: not implemented.
