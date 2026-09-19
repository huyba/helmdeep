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
		{
			// Empty scope: an agent with no entry in allowed_tools at all,
			// not merely one missing this specific tool.
			name: "an agent with no scope entry at all is denied",
			req: types.DecisionRequest{
				Subject: types.Subject{ID: "agent:nobody-configured-this-one"},
				Action:  types.Action{Tool: "kb.search"},
			},
			wantOutcome:  types.OutcomeDeny,
			wantPolicyID: "scope.deny",
		},
		{
			// Adjacent-but-different: a tool name that's plausible-looking
			// but not the one actually granted — proves scope matches the
			// exact tool name, not a prefix or a fuzzy match.
			name: "a similarly-named but ungranted tool is denied, not fuzzy-matched into scope",
			req: types.DecisionRequest{
				Subject: types.Subject{ID: "agent:support-bot"},
				Action:  types.Action{Tool: "kb.delete"},
			},
			wantOutcome:  types.OutcomeDeny,
			wantPolicyID: "scope.deny",
		},
		{
			// Mixed provenance: account_number is trusted (the only
			// argument taint.rego actually checks for this tool) while
			// amount_usd is untrusted — the call must still be allowed,
			// proving the taint check is scoped to the specific argument
			// the policy names, not "every argument must be trusted."
			name: "mixed provenance: only the checked argument's trust matters",
			req: types.DecisionRequest{
				Subject: types.Subject{ID: "agent:finance-bot"},
				Action: types.Action{
					Tool: "payments.wire_transfer",
					Arguments: map[string]types.Value{
						"account_number": trusted,
						"amount_usd":     {Data: 250.0, Provenance: types.Provenance{Source: "unspecified", Trusted: false}},
					},
				},
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
