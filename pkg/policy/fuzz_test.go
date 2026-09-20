// Copyright (c) 2026 Overarching AI LLC
// SPDX-License-Identifier: Apache-2.0

package policy

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// FuzzLoad throws arbitrary bytes at OPADecider.Load as a policy file's
// content — an operator loading a bundle from disk is trusted input in
// production, but a hot-reloaded bundle (SIGHUP, see cmd/helmdeep-gateway)
// is exactly the kind of "someone edited a file and it might be garbage"
// surface worth fuzzing. This is deliberately not fuzzing Rego's own
// parser (that's OPA's fuzz suite's job, not ours) — it's fuzzing our
// wrapper: Load must never panic, and if it does return a policy that
// compiles, Decide against that policy must never panic and must never
// resolve to a zero-value outcome without an error (the same fail-closed
// invariant internal/gateway/gateway_test.go's
// TestZeroValueDecisionResponseIsDenied locks in at the gateway level —
// here it's tested against whatever Rego a fuzzer can produce, not a
// hand-written zero value).
func FuzzLoad(f *testing.F) {
	f.Add([]byte(testPolicyAllowKBSearch))
	f.Add([]byte(testPolicyBroken))
	f.Add([]byte(""))
	f.Add([]byte("package helmdeep.authz"))
	f.Add([]byte(`package helmdeep.authz

decision := "not an object"
`))
	f.Add([]byte(`package helmdeep.authz

decision := {"outcome": input.subject.id}
`))

	f.Fuzz(func(t *testing.T, content []byte) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "policy.rego"), content, 0o600); err != nil {
			t.Fatalf("write policy fixture: %v", err)
		}

		d := NewOPADecider()
		if err := d.Load(dir); err != nil {
			return // a compile error is an expected outcome, not a bug
		}

		resp, err := d.Decide(context.Background(), decisionRequest("kb.search"))
		if err != nil {
			return // Decide itself never actually errors (see opa.go), but the interface allows it
		}
		if resp.Outcome == "" {
			t.Fatalf("Decide returned an empty Outcome with no error for policy content: %q", content)
		}
	})
}
