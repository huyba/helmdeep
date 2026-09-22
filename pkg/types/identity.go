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
// this one). As of Milestone M1, pkg/identity.JWTResolver populates this
// from a verified Session Identity Token's `delegation_chain` claim — see
// docs/adr/0007-credential-broker-scope.md for what's verified (the agent
// hop, cryptographically) versus asserted (the human hop, not checked
// against a real IdP). It's still empty when the pre-M1 static-token
// bootstrap resolver is active (gated behind -dev-insecure).
//
// Scopes records what the caller's own credential grants it — as of
// Milestone M2, from the same SIT's `scope` claim. This is deliberately
// separate from a tool's *required* scopes (pkg/toolregistry.Entry.Scopes):
// a policy compares the two, this type only carries what the caller has.
//
// AgentVersion is what the calling instance's own credential asserts it is
// running — as of the Agent Registry milestone, from the same SIT's
// `agent_version` claim (doc 04 §1.1). It is deliberately separate from
// the Agent Registry's own declared version
// (pkg/agentregistry.Entry.Version): internal/gateway compares the two
// (an instance claiming a version the registry doesn't recognize for this
// agent is denied), this type only carries what the caller asserted.
type Subject struct {
	ID              string      `json:"id"`
	Kind            SubjectKind `json:"kind"`
	AgentVersion    string      `json:"agent_version,omitempty"`
	DelegationChain []string    `json:"delegation_chain,omitempty"`
	Scopes          []string    `json:"scopes,omitempty"`
	TrustLevel      int         `json:"trust_level"`
}
