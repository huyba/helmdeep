// Copyright (c) 2026 Overarching AI LLC
// SPDX-License-Identifier: Apache-2.0

package types

import "time"

// Action is the operation a Subject is asking to perform. For the Tool
// Gateway this is always a tool call; the field names mirror the platform's
// broader Action Record model (docs/02-architecture.md) so this type can
// later cover model calls and memory writes without a breaking rename.
//
// JSON tags on this and the other types in this file define the `input`
// contract policy is written against — see docs/policy-guide.md. Treat
// renaming a tag as a breaking change to every policy bundle in the wild.
type Action struct {
	Type      string           `json:"type"` // "tool_call" for everything the Tool Gateway handles today
	Tool      string           `json:"tool"`
	Resource  string           `json:"resource,omitempty"` // the object/record the tool acts on, if the caller identified one
	Arguments map[string]Value `json:"arguments"`
}

// Usage carries the running counters a policy needs to enforce aggregate
// limits: call rate, cumulative cost, per-session budget. The gateway owns
// maintaining these counters and populates them on every DecisionRequest;
// the PDP only compares them against policy-defined thresholds and never
// tracks state itself. Keeping counters out of the policy engine is what
// lets policy stay a pure function of its input (see
// docs/adr/0002-policy-engine-choice.md).
type Usage struct {
	CallsInWindow     int     `json:"calls_in_window"`
	WindowSeconds     int     `json:"window_seconds"`
	CumulativeCostUSD float64 `json:"cumulative_cost_usd"`
	SessionBudgetUSD  float64 `json:"session_budget_usd"`
}

// DecisionContext is everything about the moment of the call that isn't the
// subject or the action itself.
type DecisionContext struct {
	SessionID string    `json:"session_id"`
	RequestID string    `json:"request_id"`
	Time      time.Time `json:"time"`
	Usage     Usage     `json:"usage"`
}

// DecisionRequest is what the Tool Gateway asks the PDP to evaluate before a
// tool call is allowed to proceed. This is exactly what's marshaled as
// Rego's `input` — see docs/policy-guide.md.
type DecisionRequest struct {
	Subject Subject         `json:"subject"`
	Action  Action          `json:"action"`
	Context DecisionContext `json:"context"`
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
	Type   ObligationType `json:"type"`
	Target string         `json:"target"` // path into the tool result the obligation applies to
}

// DecisionResponse is the PDP's answer to a DecisionRequest. Field names and
// tags match what example policies in examples/policies/ produce — see
// docs/policy-guide.md.
type DecisionResponse struct {
	Outcome     Outcome      `json:"outcome"`
	Obligations []Obligation `json:"obligations,omitempty"`
	PolicyID    string       `json:"policy_id"` // identifier of the policy/rule that produced this decision
	Reason      string       `json:"reason,omitempty"`
}
