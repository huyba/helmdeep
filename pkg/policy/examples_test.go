// Copyright (c) 2026 Overarching AI LLC
// SPDX-License-Identifier: Apache-2.0

package policy

import (
	"context"
	"testing"

	"github.com/huyba/helmdeep/pkg/types"
)

// TestExamplePolicies exercises the actual bundle in examples/policies/ —
// the one the quickstart and README point at — so an edit that breaks the
// worked example is caught here, not discovered by a user following the
// README.
func TestExamplePolicies(t *testing.T) {
	d := NewOPADecider()
	if err := d.Load("../../examples/policies"); err != nil {
		t.Fatalf("Load examples/policies: %v", err)
	}

	trusted := types.Value{Data: "ACC-TRUSTED-0001", Provenance: types.Provenance{Source: "suppliermaster", Trusted: true}}
	untrusted := types.Value{Data: "ACC-PHISHING-6669", Provenance: types.Provenance{Source: "email", Trusted: false}}

	tests := []struct {
		name         string
		req          types.DecisionRequest
		wantOutcome  types.Outcome
		wantPolicyID string
	}{
		{
			name: "support-bot may call kb.search",
			req: types.DecisionRequest{
				Subject: types.Subject{ID: "agent:support-bot"},
				Action:  types.Action{Tool: "kb.search"},
			},
			wantOutcome:  types.OutcomeAllow,
			wantPolicyID: "combined.allow",
		},
		{
			name: "support-bot may not call payments.wire_transfer (scope)",
			req: types.DecisionRequest{
				Subject: types.Subject{ID: "agent:support-bot"},
				Action:  types.Action{Tool: "payments.wire_transfer"},
			},
			wantOutcome:  types.OutcomeDeny,
			wantPolicyID: "scope.deny",
		},
		{
			name: "finance-bot may not call kb.search (scope)",
			req: types.DecisionRequest{
				Subject: types.Subject{ID: "agent:finance-bot"},
				Action:  types.Action{Tool: "kb.search"},
			},
			wantOutcome:  types.OutcomeDeny,
			wantPolicyID: "scope.deny",
		},
		{
			name: "finance-bot wire transfer from a trusted source is allowed",
			req: types.DecisionRequest{
				Subject: types.Subject{ID: "agent:finance-bot"},
				Action: types.Action{
					Tool:      "payments.wire_transfer",
					Arguments: map[string]types.Value{"account_number": trusted},
				},
			},
			wantOutcome:  types.OutcomeAllow,
			wantPolicyID: "combined.allow",
		},
		{
			name: "finance-bot wire transfer from an untrusted source is denied (taint)",
			req: types.DecisionRequest{
				Subject: types.Subject{ID: "agent:finance-bot"},
				Action: types.Action{
					Tool:      "payments.wire_transfer",
					Arguments: map[string]types.Value{"account_number": untrusted},
				},
			},
			wantOutcome:  types.OutcomeDeny,
			wantPolicyID: "taint.deny",
		},
		{
			name: "kb.search over the rate limit is denied (aggregate limits)",
			req: types.DecisionRequest{
				Subject: types.Subject{ID: "agent:support-bot"},
				Action:  types.Action{Tool: "kb.search"},
				Context: types.DecisionContext{Usage: types.Usage{CallsInWindow: 4, WindowSeconds: 60}},
			},
			wantOutcome:  types.OutcomeDeny,
			wantPolicyID: "limits.deny",
		},
		{
			name: "kb.search at the rate limit boundary is still allowed",
			req: types.DecisionRequest{
				Subject: types.Subject{ID: "agent:support-bot"},
				Action:  types.Action{Tool: "kb.search"},
				Context: types.DecisionContext{Usage: types.Usage{CallsInWindow: 3, WindowSeconds: 60}},
			},
			wantOutcome:  types.OutcomeAllow,
			wantPolicyID: "combined.allow",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := d.Decide(context.Background(), tt.req)
			if err != nil {
				t.Fatalf("Decide: %v", err)
			}
			if resp.Outcome != tt.wantOutcome || resp.PolicyID != tt.wantPolicyID {
				t.Fatalf("got outcome=%s policy_id=%s, want outcome=%s policy_id=%s (reason: %s)",
					resp.Outcome, resp.PolicyID, tt.wantOutcome, tt.wantPolicyID, resp.Reason)
			}
		})
	}
}
