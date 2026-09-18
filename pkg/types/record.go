// Copyright (c) 2026 Overarching AI LLC
// SPDX-License-Identifier: Apache-2.0

package types

import "time"

// ActionRecord is the append-only, tamper-evident record of one decision the
// gateway made and, if allowed, enforced. Records are hash-chained to their
// predecessor — see docs/adr/0004-action-record-format.md for the format and
// what "tamper-evident" does and doesn't guarantee.
type ActionRecord struct {
	Timestamp time.Time
	Subject   Subject
	Action    Action
	Decision  DecisionResponse
	Outcome   RecordOutcome

	// PrevHash is the Hash of the record immediately before this one in the
	// chain (empty for the first record). Hash is computed over this
	// record's contents plus PrevHash.
	PrevHash string
	Hash     string
}

// RecordOutcome is what actually happened after enforcement, as distinct
// from Decision.Outcome (the PDP can allow a call that then fails upstream).
type RecordOutcome string

const (
	RecordOutcomeAllowed RecordOutcome = "allowed"
	RecordOutcomeDenied  RecordOutcome = "denied"
	// RecordOutcomeFailed means the PDP allowed the call but the upstream
	// invocation itself errored.
	RecordOutcomeFailed RecordOutcome = "failed"
)
