// Copyright (c) 2026 Overarching AI LLC
// SPDX-License-Identifier: Apache-2.0

package policy

import (
	"context"
	"testing"

	"github.com/huyba/helmdeep/pkg/types"
)

// BenchmarkDecide measures exactly the number ROADMAP.md sets a target for:
// added latency of one policy decision against a loaded, prepared policy —
// not the surrounding HTTP/JSON overhead, which is a separate cost. Run
// with: go test ./pkg/policy/... -bench BenchmarkDecide -benchtime=2s
//
// It uses the real examples/policies/ bundle, not a toy one-rule policy —
// the combined scope+taint+limits evaluation is the realistic cost, not
// the cheapest possible one.
func BenchmarkDecide(b *testing.B) {
	d := NewOPADecider()
	if err := d.Load("../../examples/policies"); err != nil {
		b.Fatalf("Load: %v", err)
	}

	req := types.DecisionRequest{
		Subject: types.Subject{ID: "agent:finance-bot", Kind: types.SubjectKindAgent},
		Action: types.Action{
			Type: "tool_call",
			Tool: "payments.wire_transfer",
			Arguments: map[string]types.Value{
				"account_number": {Data: "ACC-TRUSTED-0001", Provenance: types.Provenance{Source: "suppliermaster", Trusted: true}},
				"amount_usd":     {Data: 100.0, Provenance: types.Provenance{Source: "unspecified", Trusted: false}},
			},
		},
		Context: types.DecisionContext{
			RequestID: "bench",
			Usage:     types.Usage{CallsInWindow: 1, WindowSeconds: 60},
		},
	}

	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := d.Decide(ctx, req); err != nil {
			b.Fatalf("Decide: %v", err)
		}
	}
}
