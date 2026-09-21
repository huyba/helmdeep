// Copyright (c) 2026 Overarching AI LLC
// SPDX-License-Identifier: Apache-2.0

package policy

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/huyba/helmdeep/pkg/types"
)

const testPolicyAllowKBSearch = `package helmdeep.authz

default decision := {"outcome": "deny", "policy_id": "default.deny", "reason": "no matching rule"}

decision := {"outcome": "allow", "policy_id": "test.allow", "reason": "tool is kb.search"} if {
	input.action.tool == "kb.search"
}
`

const testPolicyAllowOtherTool = `package helmdeep.authz

default decision := {"outcome": "deny", "policy_id": "default.deny", "reason": "no matching rule"}

decision := {"outcome": "allow", "policy_id": "test.allow.v2", "reason": "tool is other.tool"} if {
	input.action.tool == "other.tool"
}
`

const testPolicyBroken = `package helmdeep.authz

this is not valid rego
`

func writePolicy(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "policy.rego"), []byte(content), 0o600); err != nil {
		t.Fatalf("write policy fixture: %v", err)
	}
}

func decisionRequest(tool string) types.DecisionRequest {
	return types.DecisionRequest{
		Subject: types.Subject{ID: "agent:test", Kind: types.SubjectKindAgent},
		Action:  types.Action{Type: "tool_call", Tool: tool},
	}
}

func TestOPADecider_AllowAndDefaultDeny(t *testing.T) {
	dir := t.TempDir()
	writePolicy(t, dir, testPolicyAllowKBSearch)

	d := NewOPADecider()
	if err := d.Load(dir); err != nil {
		t.Fatalf("Load: %v", err)
	}

	resp, err := d.Decide(context.Background(), decisionRequest("kb.search"))
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if resp.Outcome != types.OutcomeAllow || resp.PolicyID != "test.allow" {
		t.Fatalf("got %+v, want allow/test.allow", resp)
	}

	resp, err = d.Decide(context.Background(), decisionRequest("salesforce.delete_account"))
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if resp.Outcome != types.OutcomeDeny || resp.PolicyID != "default.deny" {
		t.Fatalf("got %+v, want deny/default.deny", resp)
	}
}

func TestOPADecider_DenyBeforeLoad(t *testing.T) {
	d := NewOPADecider()
	resp, err := d.Decide(context.Background(), decisionRequest("kb.search"))
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if resp.Outcome != types.OutcomeDeny || resp.PolicyID != "no_policy_loaded" {
		t.Fatalf("got %+v, want deny/no_policy_loaded", resp)
	}
}

func TestOPADecider_LoadErrorOnBadPath(t *testing.T) {
	d := NewOPADecider()
	if err := d.Load(filepath.Join(t.TempDir(), "does-not-exist")); err == nil {
		t.Fatal("Load: expected an error for a nonexistent policy path, got nil")
	}
}

func TestOPADecider_ReloadBeforeLoadErrors(t *testing.T) {
	d := NewOPADecider()
	if err := d.Reload(); err == nil {
		t.Fatal("Reload: expected an error when called before any Load, got nil")
	}
}

func TestOPADecider_HotReloadPicksUpChange(t *testing.T) {
	dir := t.TempDir()
	writePolicy(t, dir, testPolicyAllowKBSearch)

	d := NewOPADecider()
	if err := d.Load(dir); err != nil {
		t.Fatalf("Load: %v", err)
	}

	resp, _ := d.Decide(context.Background(), decisionRequest("other.tool"))
	if resp.Outcome != types.OutcomeDeny {
		t.Fatalf("before reload: got %+v, want deny", resp)
	}

	writePolicy(t, dir, testPolicyAllowOtherTool)
	if err := d.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}

	resp, _ = d.Decide(context.Background(), decisionRequest("other.tool"))
	if resp.Outcome != types.OutcomeAllow || resp.PolicyID != "test.allow.v2" {
		t.Fatalf("after reload: got %+v, want allow/test.allow.v2", resp)
	}
}

func TestOPADecider_FailedReloadKeepsPreviousPolicyActive(t *testing.T) {
	dir := t.TempDir()
	writePolicy(t, dir, testPolicyAllowKBSearch)

	d := NewOPADecider()
	if err := d.Load(dir); err != nil {
		t.Fatalf("Load: %v", err)
	}

	writePolicy(t, dir, testPolicyBroken)
	if err := d.Reload(); err == nil {
		t.Fatal("Reload: expected an error for a broken policy file, got nil")
	}

	// The gateway must still be enforcing the last good policy, not running
	// with no policy at all.
	resp, err := d.Decide(context.Background(), decisionRequest("kb.search"))
	if err != nil {
		t.Fatalf("Decide after failed reload: %v", err)
	}
	if resp.Outcome != types.OutcomeAllow || resp.PolicyID != "test.allow" {
		t.Fatalf("after failed reload: got %+v, want the pre-reload policy still active", resp)
	}
}

func TestOPADecider_UnrecognizedOutcomeIsDenied(t *testing.T) {
	dir := t.TempDir()
	writePolicy(t, dir, `package helmdeep.authz

decision := {"outcome": "maybe", "policy_id": "broken", "reason": "not a real outcome"}
`)
	d := NewOPADecider()
	if err := d.Load(dir); err != nil {
		t.Fatalf("Load: %v", err)
	}

	resp, err := d.Decide(context.Background(), decisionRequest("kb.search"))
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if resp.Outcome != types.OutcomeDeny || resp.PolicyID != "unrecognized_outcome" {
		t.Fatalf("got %+v, want deny/unrecognized_outcome", resp)
	}
}

// TestOPADecider_EmptyPolicyDirectoryDenies is the "empty or missing policy
// must deny, not allow" requirement, made concrete: a directory with zero
// .rego files compiles successfully (there's nothing invalid about an
// empty bundle) but defines no `decision` rule at all, so the query is
// undefined for every input — which Decide treats identically to any other
// failure to get a real answer.
func TestOPADecider_EmptyPolicyDirectoryDenies(t *testing.T) {
	d := NewOPADecider()
	if err := d.Load(t.TempDir()); err != nil {
		t.Fatalf("Load an empty policy directory: %v", err)
	}

	resp, err := d.Decide(context.Background(), decisionRequest("kb.search"))
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if resp.Outcome != types.OutcomeDeny || resp.PolicyID != "policy_undefined" {
		t.Fatalf("got %+v, want deny/policy_undefined", resp)
	}
}

// TestOPADecider_NonObjectDecisionIsDenied covers policy output that isn't
// even the right shape — a string instead of an object — as distinct from
// TestOPADecider_UnrecognizedOutcomeIsDenied's "right shape, wrong value."
// Both are "malformed PDP output," but they fail at different points in
// Decide (JSON-unmarshal-into-struct vs. the outcome switch), so both are
// worth locking in separately.
func TestOPADecider_NonObjectDecisionIsDenied(t *testing.T) {
	dir := t.TempDir()
	writePolicy(t, dir, `package helmdeep.authz

decision := "just a string, not an object"
`)
	d := NewOPADecider()
	if err := d.Load(dir); err != nil {
		t.Fatalf("Load: %v", err)
	}

	resp, err := d.Decide(context.Background(), decisionRequest("kb.search"))
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if resp.Outcome != types.OutcomeDeny || resp.PolicyID != "result_parse_error" {
		t.Fatalf("got %+v, want deny/result_parse_error", resp)
	}
}

// TestOPADecider_LoadsFromKubernetesConfigMapMount reproduces the exact
// directory layout kubelet creates for a mounted ConfigMap volume: the
// real file lives in a hidden, timestamped directory
// (e.g. "..2026_01_01_00_00_00.000000000"), a hidden "..data" symlink
// points at that directory, and the visible, user-facing filename is
// itself a symlink through "..data" — kubelet's mechanism for swapping in
// updated ConfigMap content atomically. A naive recursive directory walk
// (rego.Load's default with a nil filter) finds the same *.rego file three
// times, once per path, and OPA's compiler then reports "multiple default
// rules" for a bundle that has exactly one from any real user's point of
// view — this is exactly what broke Load on a real AKS deployment. See
// hiddenFileFilter's doc comment.
func TestOPADecider_LoadsFromKubernetesConfigMapMount(t *testing.T) {
	dir := t.TempDir()

	realDir := filepath.Join(dir, "..2026_01_01_00_00_00.000000000")
	if err := os.Mkdir(realDir, 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", realDir, err)
	}
	writePolicy(t, realDir, testPolicyAllowKBSearch)

	dataLink := filepath.Join(dir, "..data")
	if err := os.Symlink(realDir, dataLink); err != nil {
		t.Fatalf("symlink ..data: %v", err)
	}
	visibleFile := filepath.Join(dir, "policy.rego")
	if err := os.Symlink(filepath.Join(dataLink, "policy.rego"), visibleFile); err != nil {
		t.Fatalf("symlink policy.rego: %v", err)
	}

	d := NewOPADecider()
	if err := d.Load(dir); err != nil {
		t.Fatalf("Load a ConfigMap-shaped directory: %v", err)
	}

	resp, err := d.Decide(context.Background(), decisionRequest("kb.search"))
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if resp.Outcome != types.OutcomeAllow {
		t.Fatalf("got %+v, want allow (loaded exactly once)", resp)
	}
}

func TestOPADecider_ReadyReflectsLoadState(t *testing.T) {
	d := NewOPADecider()
	if err := d.Ready(); err == nil {
		t.Fatal("Ready() passed before any policy was loaded")
	}

	dir := t.TempDir()
	writePolicy(t, dir, testPolicyAllowKBSearch)
	if err := d.Load(dir); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := d.Ready(); err != nil {
		t.Fatalf("Ready() after a successful Load: %v", err)
	}

	// A failed Reload keeps the previous policy active, so it must not flip
	// readiness — taking the pod out of rotation for a typo in a policy edit
	// would turn a safe failure into an outage.
	writePolicy(t, dir, testPolicyBroken)
	if err := d.Reload(); err == nil {
		t.Fatal("expected Reload of a broken policy to error")
	}
	if err := d.Ready(); err != nil {
		t.Fatalf("Ready() after a failed Reload: %v", err)
	}
}
