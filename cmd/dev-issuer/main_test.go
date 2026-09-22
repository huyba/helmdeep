// Copyright (c) 2026 Overarching AI LLC
// SPDX-License-Identifier: Apache-2.0

package main

import "testing"

// TestHostForLog is the one piece of this binary with any logic worth
// unit-testing directly; everything else (flag wiring, signal handling) is
// exercised by actually running the binary — see
// examples/quickstart/README.md and pkg/identity/jwt_test.go, which both
// depend on internal/devissuer's real behavior.
func TestHostForLog(t *testing.T) {
	tests := []struct{ addr, want string }{
		{":8444", "localhost:8444"},
		{"0.0.0.0:8444", "0.0.0.0:8444"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := hostForLog(tt.addr); got != tt.want {
			t.Errorf("hostForLog(%q) = %q, want %q", tt.addr, got, tt.want)
		}
	}
}
