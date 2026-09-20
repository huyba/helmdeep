// Copyright (c) 2026 Overarching AI LLC
// SPDX-License-Identifier: Apache-2.0

package policy

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/huyba/helmdeep/pkg/types"
)

// BenchmarkDecideScaling measures how policy-decision latency scales with
// the number of scope rules in the bundle — 10, 100, and 1000 — to show
// whether the gateway's hot path degrades as a deployment's policy grows,
// or whether OPA's rule indexing on the equality check keeps it flat.
// Every case decides against the *last* rule in the bundle, the case a
// naive linear scan would make worst.
func BenchmarkDecideScaling(b *testing.B) {
	for _, n := range []int{10, 100, 1000} {
		b.Run(fmt.Sprintf("%d_rules", n), func(b *testing.B) {
			dir := b.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "policy.rego"), []byte(generateScopeRules(n)), 0o600); err != nil {
				b.Fatalf("write policy: %v", err)
			}

			d := NewOPADecider()
			if err := d.Load(dir); err != nil {
				b.Fatalf("Load: %v", err)
			}

			req := types.DecisionRequest{
				Subject: types.Subject{ID: "agent:test"},
				Action:  types.Action{Tool: fmt.Sprintf("tool.%d", n-1)}, // the last rule generated
			}
			ctx := context.Background()

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				resp, err := d.Decide(ctx, req)
				if err != nil {
					b.Fatalf("Decide: %v", err)
				}
				if resp.Outcome != types.OutcomeAllow {
					b.Fatalf("got %+v, want allow (benchmark input didn't match the generated policy)", resp)
				}
			}
		})
	}
}

// generateScopeRules builds a synthetic bundle of n independent
// equality-matched rules, shaped like examples/policies/scope.rego but
// scaled up — one rule per tool name, all in the same package so a real
// deployment's policy-authoring style (many small, obvious rules) is what
// gets measured, not one artificially complex rule.
func generateScopeRules(n int) string {
	var b strings.Builder
	b.WriteString("package helmdeep.authz\n\n")
	b.WriteString(`default decision := {"outcome": "deny", "policy_id": "default.deny"}` + "\n\n")
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, `decision := {"outcome": "allow", "policy_id": "rule.%d"} if { input.action.tool == "tool.%d" }`+"\n", i, i)
	}
	return b.String()
}
