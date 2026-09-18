// Copyright (c) 2026 Overarching AI LLC
// SPDX-License-Identifier: Apache-2.0

// STATUS: interface only. Not implemented. See docs/ROADMAP.md.
//
// Package scheduler will place agent sessions onto runtime capacity per
// quota and cost budget, and own durable session lifecycle (suspend/resume,
// survive process/node failure) — see docs/06-orchestration.md. Nothing in
// this repo depends on this package yet.
package scheduler

import "context"

// Scheduler is the shape a future scheduler implementation would expose.
// The signature is illustrative, not a commitment — not implemented.
type Scheduler interface {
	Schedule(ctx context.Context, sessionID string) error
}

// TODO: not implemented.
