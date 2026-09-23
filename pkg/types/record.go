// Copyright (c) 2026 Overarching AI LLC
// SPDX-License-Identifier: Apache-2.0

package types

import "time"

// ActionRecord is the append-only, tamper-evident record of one decision the
// gateway made and, if allowed, enforced. Records are hash-chained to their
// predecessor — see docs/adr/0004-action-record-format.md for the format and
// what "tamper-evident" does and doesn't guarantee.
type ActionRecord struct {
	Timestamp time.Time        `json:"timestamp"`
	Subject   Subject          `json:"subject"`
	Action    Action           `json:"action"`
	Decision  DecisionResponse `json:"decision"`
	Outcome   RecordOutcome    `json:"outcome"`

	// PolicyBundle identifies the policy bundle whose rules produced
	// Decision — Decision.PolicyID names the rule, which is not enough on
	// its own to reproduce a decision, because a rule id is not stable
	// across edits to the bundle that defines it. Populated by
	// pkg/policyservice.StampingStore when the deployment verifies signed
	// bundles; nil means this record was written by a deployment that
	// doesn't (or before its first bundle loaded), which is why it's a
	// pointer with omitempty: records written before this field existed
	// still hash to the same value they did then, so existing chains stay
	// verifiable.
	PolicyBundle *PolicyBundleRef `json:"policy_bundle,omitempty"`

	// PrevHash is the Hash of the record immediately before this one in the
	// chain (empty for the first record). Hash is computed over this
	// record's contents plus PrevHash. Hash itself is deliberately excluded
	// from what gets hashed (see pkg/audit) — a field can't authenticate
	// itself.
	PrevHash string `json:"prev_hash"`
	Hash     string `json:"hash"`
}

// PolicyBundleRef identifies one signed, versioned policy bundle: the
// operator's own version string and the content digest of its signed
// manifest (pkg/policyservice.Manifest.Digest). Two records carrying the
// same Digest were decided by byte-identical policy, whatever their
// Version strings say.
type PolicyBundleRef struct {
	Version string `json:"version"`
	Digest  string `json:"digest"`
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
