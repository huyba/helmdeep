// Copyright (c) 2026 Overarching AI LLC
// SPDX-License-Identifier: Apache-2.0

// Package audit defines the append-only action record store: every decision
// the gateway makes, allow or deny, is written here before the caller sees
// the result of an irreversible action.
//
// STATUS: interface only for Step 1. A file-backed, hash-chained
// implementation lands in Step 2 — see
// docs/adr/0004-action-record-format.md and ROADMAP.md.
package audit

import (
	"context"

	"github.com/huyba/helmdeep/pkg/types"
)

// Recorder appends a new action record, chaining it to the previous
// record's hash. Append must be durable before the gateway proceeds with an
// irreversible action — see docs/adr/0003-fail-closed-behavior.md.
type Recorder interface {
	Append(ctx context.Context, rec types.ActionRecord) error
}

// Store is a Recorder that can also verify its own integrity.
type Store interface {
	Recorder
	// Verify walks the full chain and returns an error describing the
	// first break it finds, or nil if the chain is intact end to end.
	Verify(ctx context.Context) error
}
