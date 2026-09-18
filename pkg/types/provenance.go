// Copyright (c) 2026 Overarching AI LLC
// SPDX-License-Identifier: Apache-2.0

package types

// Provenance labels where a value came from, so a policy can require that a
// field derive from a trusted source rather than from untrusted model or
// tool output.
//
// Motivating example: a bank account number used in a payment tool call must
// carry Source "supplier_master_record" and Trusted true. If the same field
// instead traces back to text extracted from an inbound email, the gateway
// tags it Source "inbound_email", Trusted false, and a taint policy denies
// the call — regardless of what the model claims the value is.
type Provenance struct {
	// Source names where the value originated: a specific system of record
	// ("supplier_master_record"), a class of untrusted input
	// ("inbound_email", "web_content", "tool_output"), or "user_input" for
	// something the invoking human typed directly. The vocabulary is
	// defined by policy, not by this type.
	Source string `json:"source"`
	// Trusted is the gateway's own classification of Source, decided by
	// configuration (which sources are trusted) rather than by anything the
	// agent or model asserts about itself.
	Trusted bool `json:"trusted"`
}

// Value is a tool-call argument together with the provenance of where it
// came from. The gateway is responsible for building these — tracking which
// field came from which prior tool result, retrieved document, or direct
// user input — so the PDP can evaluate taint policy by reading a label
// rather than re-deriving lineage itself.
type Value struct {
	Data       any        `json:"data"`
	Provenance Provenance `json:"provenance"`
}
