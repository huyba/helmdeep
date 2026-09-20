// Copyright (c) 2026 Overarching AI LLC
// SPDX-License-Identifier: Apache-2.0

package audit

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/huyba/helmdeep/pkg/types"
)

// BenchmarkAppend isolates the audit write's cost, since
// test/e2e/gateway_bench_test.go's BenchmarkGatewayOverhead showed the
// full gateway round trip costing far more than the mock upstream call
// alone — this pins down how much of that is the durable (fsync'd) audit
// write specifically, as opposed to policy evaluation or anything else.
func BenchmarkAppend(b *testing.B) {
	s, err := NewFileStore(filepath.Join(b.TempDir(), "audit.log"))
	if err != nil {
		b.Fatalf("NewFileStore: %v", err)
	}
	rec := testRecord("kb.search", types.RecordOutcomeAllowed)
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := s.Append(ctx, rec); err != nil {
			b.Fatalf("Append: %v", err)
		}
	}
}
