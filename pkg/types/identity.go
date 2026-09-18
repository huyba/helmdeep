// Copyright (c) 2026 Overarching AI LLC
// SPDX-License-Identifier: Apache-2.0

// Package types holds the contracts shared by every HelmDeep component: what
// a caller is, what it's asking to do, what the policy engine decided, and
// what got recorded. Every other package in this repo depends on types;
// types depends on nothing in this repo. Changing a struct here is expensive
// because it's expensive everywhere at once — so change it deliberately.
package types

// SubjectKind identifies what kind of principal a Subject represents.
type SubjectKind string

const (
	SubjectKindUser    SubjectKind = "user"
	SubjectKindAgent   SubjectKind = "agent"
	SubjectKindService SubjectKind = "service"
)

// Subject is the caller a policy decision is evaluated for — concretely,
// today, the agent identity the gateway resolved for an incoming MCP
// request.
//
// DelegationChain records the principals this call was delegated through,
// outermost first (e.g. a human who invoked a workflow agent that spawned
// this one). It is always empty in this repo for now: real delegation
// requires the identity & credential exchange component, which is
// interface-only (see pkg/identity and docs/04-identity-authz.md). The field
// exists now so DecisionRequest and ActionRecord don't need a breaking
// change when delegation lands.
type Subject struct {
	ID              string      `json:"id"`
	Kind            SubjectKind `json:"kind"`
	AgentVersion    string      `json:"agent_version,omitempty"`
	DelegationChain []string    `json:"delegation_chain,omitempty"`
	TrustLevel      int         `json:"trust_level"`
}
