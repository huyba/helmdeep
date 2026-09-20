// Copyright (c) 2026 Overarching AI LLC
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"testing"
)

// Both benchmarks below call supplier.get_account, not kb.search:
// examples/policies/limits.rego caps kb.search at 3 calls per window, and
// a benchmark that runs thousands of iterations would spend nearly all of
// them being denied by the rate limiter rather than measuring the allow
// path it's meant to. supplier.get_account has no configured limit.

// BenchmarkDirectUpstreamCall measures the mock upstream's own round trip
// with nothing in front of it — the baseline BenchmarkGatewayOverhead is
// measured against. Both benchmarks send the exact same JSON-RPC request
// shape over real HTTP; the only difference is what's listening on the
// other end.
func BenchmarkDirectUpstreamCall(b *testing.B) {
	supplierTS, _ := startMockUpstream(b, "suppliermaster")
	endpoint := supplierTS.URL

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resultOf(b, callTool(b, endpoint, "", "supplier.get_account", map[string]any{"supplier_id": "S1"}))
	}
}

// BenchmarkGatewayOverhead measures the same call routed through the full
// Tool Gateway: identity resolution, a real (embedded OPA) policy
// decision, a durable hash-chained audit write, and the forwarded upstream
// call. The delta between this and BenchmarkDirectUpstreamCall is the
// gateway's real, end-to-end added latency — the number ROADMAP.md's
// "platform overhead < 15%" target and README's sub-5ms *policy decision*
// target are each one piece of. See the README for both numbers reported
// together with the hardware they were measured on.
func BenchmarkGatewayOverhead(b *testing.B) {
	endpoint, _, _ := newTestGateway(b)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resultOf(b, callTool(b, endpoint, financeToken, "supplier.get_account", map[string]any{"supplier_id": "S1"}))
	}
}
