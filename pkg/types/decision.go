// Copyright (c) 2026 Overarching AI LLC
// SPDX-License-Identifier: Apache-2.0

package types

import "time"

// Action is the operation a Subject is asking to perform. For the Tool
// Gateway this is always a tool call; the field names mirror the platform's
// broader Action Record model (docs/02-architecture.md) so this type can
// later cover model calls and memory writes without a breaking rename.
type Action struct {
	Type      string // "tool_call" for everything the Tool Gateway handles today
	Tool      string
	Resource  string // the object/record the tool acts on, if the caller identified one
	Arguments map[string]Value
}

// Usage carries the running counters a policy needs to enforce aggregate
// limits: call rate, cumulative cost, per-session budget. The gateway owns
// maintaining these counters and populates them on every DecisionRequest;
// the PDP only compares them against policy-defined thresholds and never
// tracks state itself. Keeping counters out of the policy engine is what
// lets policy stay a pure function of its input (see
// docs/adr/0002-policy-engine-choice.md).
type Usage struct {
	CallsInWindow     int
	WindowSeconds     int
	CumulativeCostUSD float64
	SessionBudgetUSD  float64
}

// DecisionContext is everything about the moment of the call that isn't the
// subject or the action itself.
type DecisionContext struct {
	SessionID string
	RequestID string
	Time      time.Time
	Usage     Usage
}

// DecisionRequest is what the Tool Gateway asks the PDP to evaluate before a
// tool call is allowed to proceed.
type DecisionRequest struct {
	Subject Subject
	Action  Action
	Context DecisionContext
}

// Outcome is the PDP's verdict on a DecisionRequest.
type Outcome string

const (
	OutcomeAllow                Outcome = "allow"
	OutcomeDeny                 Outcome = "deny"
	OutcomeAllowWithObligations Outcome = "allow_with_obligations"
)

// ObligationType is a response-side transformation the gateway must apply to
// an allowed call's result before returning it to the agent.
type ObligationType string

const (
	ObligationRedact ObligationType = "redact"
)

// Obligation is one such transformation, e.g. redact a field from the tool
// result before it reaches the agent.
type Obligation struct {
	Type   ObligationType
	Target string // path into the tool result the obligation applies to
}

// DecisionResponse is the PDP's answer to a DecisionRequest.
type DecisionResponse struct {
	Outcome     Outcome
	Obligations []Obligation
	PolicyID    string // identifier of the policy/rule that produced this decision
	Reason      string
}
