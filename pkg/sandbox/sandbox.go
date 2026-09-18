// Copyright (c) 2026 Overarching AI LLC
// SPDX-License-Identifier: Apache-2.0

// STATUS: interface only. Not implemented. See docs/ROADMAP.md.
//
// Package sandbox will hold the agent sandbox runtime: isolated execution
// (microVM per session for write-capable agents, pooled hardened containers
// for read-only reasoning sessions) — see docs/03-agent-runtime.md. The Tool
// Gateway runs outside any sandbox, on the network path the sandbox cannot
// route around; it does not depend on this package.
package sandbox

import "context"

// Runtime is the shape a future sandbox implementation would expose. The
// signature is illustrative, not a commitment — not implemented.
type Runtime interface {
	StartSession(ctx context.Context, sessionID string) error
	StopSession(ctx context.Context, sessionID string) error
}

// TODO: not implemented.
