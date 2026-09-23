// Copyright (c) 2026 Overarching AI LLC
// SPDX-License-Identifier: Apache-2.0

package policyservice

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/huyba/helmdeep/pkg/policy"
	"github.com/huyba/helmdeep/pkg/types"
)

// spyLoader records what the inner policy.Loader was actually asked to do,
// which is the property most of these tests are about: a bundle that fails
// verification must never reach the compiler.
type spyLoader struct {
	loads   []string
	reloads int
	err     error
}

func (l *spyLoader) Load(path string) error {
	l.loads = append(l.loads, path)
	return l.err
}

func (l *spyLoader) Reload() error {
	l.reloads++
	return l.err
}

var _ policy.Loader = (*spyLoader)(nil)

func TestVerifyingLoader_LoadsAVerifiedBundle(t *testing.T) {
	dir, m, pub, _ := signedBundle(t)
	manifest := filepath.Join(t.TempDir(), "bundle.json")
	if err := WriteManifest(manifest, m); err != nil {
		t.Fatalf("WriteManifest: %v", err)
	}
	inner := &spyLoader{}
	l := NewVerifyingLoader(inner, manifest, pub)

	if err := l.Load(dir); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(inner.loads) != 1 || inner.loads[0] != dir {
		t.Fatalf("inner loads = %v, want one load of %q", inner.loads, dir)
	}
	digest, err := m.Digest()
	if err != nil {
		t.Fatalf("Digest: %v", err)
	}
	if got := l.Active(); got.Version != "v1" || got.Digest != digest {
		t.Fatalf("Active() = %+v, want version v1 and digest %s", got, digest)
	}
}

func TestVerifyingLoader_TamperedBundleNeverReachesTheCompiler(t *testing.T) {
	dir, m, pub, _ := signedBundle(t)
	manifest := filepath.Join(t.TempDir(), "bundle.json")
	if err := WriteManifest(manifest, m); err != nil {
		t.Fatalf("WriteManifest: %v", err)
	}
	write(t, filepath.Join(dir, "scope.rego"), "package helmdeep.authz\n\nscope_allowed if true\n")

	inner := &spyLoader{}
	l := NewVerifyingLoader(inner, manifest, pub)
	err := l.Load(dir)
	if err == nil {
		t.Fatal("Load: expected an error for a bundle that no longer matches its manifest")
	}
	if !strings.Contains(err.Error(), "failed verification") {
		t.Fatalf("Load error should say verification failed, got: %v", err)
	}
	if len(inner.loads) != 0 {
		t.Fatalf("inner loader was asked to compile an unverified bundle: %v", inner.loads)
	}
	if got := l.Active(); got != (types.PolicyBundleRef{}) {
		t.Fatalf("Active() = %+v after a failed load, want the zero reference", got)
	}
}

func TestVerifyingLoader_MissingManifestIsRefused(t *testing.T) {
	dir, _, pub, _ := signedBundle(t)
	inner := &spyLoader{}
	l := NewVerifyingLoader(inner, filepath.Join(t.TempDir(), "absent.json"), pub)
	if err := l.Load(dir); err == nil {
		t.Fatal("Load: expected an error when the manifest file does not exist")
	}
	if len(inner.loads) != 0 {
		t.Fatalf("inner loader ran without a manifest: %v", inner.loads)
	}
}

func TestVerifyingLoader_FailedReloadKeepsThePreviousBundleActive(t *testing.T) {
	// The property that matters operationally: a tampered — or merely
	// half-written — bundle arriving at reload time must not disturb what
	// is already loaded. pkg/policy.OPADecider.Reload already guarantees
	// this for a compile error; verification must not be the weaker link.
	dir, m, pub, _ := signedBundle(t)
	manifest := filepath.Join(t.TempDir(), "bundle.json")
	if err := WriteManifest(manifest, m); err != nil {
		t.Fatalf("WriteManifest: %v", err)
	}
	inner := &spyLoader{}
	l := NewVerifyingLoader(inner, manifest, pub)
	if err := l.Load(dir); err != nil {
		t.Fatalf("Load: %v", err)
	}
	before := l.Active()

	write(t, filepath.Join(dir, "decision.rego"), "package helmdeep.authz\n\ndecision := {\"outcome\": \"allow\"}\n")
	if err := l.Reload(); err == nil {
		t.Fatal("Reload: expected an error for a bundle that no longer matches its manifest")
	}
	if inner.reloads != 0 || len(inner.loads) != 1 {
		t.Fatalf("inner loader was touched by a failed reload: loads=%v reloads=%d", inner.loads, inner.reloads)
	}
	if got := l.Active(); got != before {
		t.Fatalf("Active() = %+v after a failed reload, want the previously loaded %+v", got, before)
	}
}

func TestVerifyingLoader_ReloadPicksUpAReSignedBundle(t *testing.T) {
	dir, m, pub, priv := signedBundle(t)
	manifest := filepath.Join(t.TempDir(), "bundle.json")
	if err := WriteManifest(manifest, m); err != nil {
		t.Fatalf("WriteManifest: %v", err)
	}
	inner := &spyLoader{}
	l := NewVerifyingLoader(inner, manifest, pub)
	if err := l.Load(dir); err != nil {
		t.Fatalf("Load: %v", err)
	}

	write(t, filepath.Join(dir, "decision.rego"), "package helmdeep.authz\n\ndefault decision := {\"outcome\": \"deny\", \"policy_id\": \"v2\"}\n")
	next, err := Sign(dir, "v2", priv)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if err := WriteManifest(manifest, next); err != nil {
		t.Fatalf("WriteManifest: %v", err)
	}
	if err := l.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if got := l.Active(); got.Version != "v2" {
		t.Fatalf("Active() = %+v after reloading a re-signed bundle, want version v2", got)
	}
}

func TestVerifyingLoader_ReloadBeforeLoadFails(t *testing.T) {
	_, m, pub, _ := signedBundle(t)
	manifest := filepath.Join(t.TempDir(), "bundle.json")
	if err := WriteManifest(manifest, m); err != nil {
		t.Fatalf("WriteManifest: %v", err)
	}
	l := NewVerifyingLoader(&spyLoader{}, manifest, pub)
	if err := l.Reload(); err == nil {
		t.Fatal("Reload: expected an error before any successful Load")
	}
}

func TestVerifyingLoader_AVerifiedButUncompilableBundleDoesNotBecomeActive(t *testing.T) {
	// Authentic and broken are different failures: the manifest verifies,
	// the Rego doesn't compile. Active must keep describing what the PDP
	// really holds, not what we tried to give it.
	dir, m, pub, _ := signedBundle(t)
	manifest := filepath.Join(t.TempDir(), "bundle.json")
	if err := WriteManifest(manifest, m); err != nil {
		t.Fatalf("WriteManifest: %v", err)
	}
	compileErr := errors.New("compile policy bundle: unexpected token")
	inner := &spyLoader{err: compileErr}
	l := NewVerifyingLoader(inner, manifest, pub)

	if err := l.Load(dir); !errors.Is(err, compileErr) {
		t.Fatalf("Load error = %v, want the inner loader's compile error", err)
	}
	if got := l.Active(); got != (types.PolicyBundleRef{}) {
		t.Fatalf("Active() = %+v after a bundle failed to compile, want the zero reference", got)
	}
}

// TestVerifyingLoader_WithARealOPADecider exercises the whole path with the
// actual PDP rather than a spy — verify, compile, decide — and then the
// failure that matters most: after a tampered reload is refused, the PDP is
// still deciding on the bundle it already had, not failing closed on every
// request because policy went missing.
func TestVerifyingLoader_WithARealOPADecider(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "decision.rego"), `package helmdeep.authz

default decision := {"outcome": "deny", "policy_id": "signed.v1", "reason": "fail closed"}
`)
	pub, priv, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	m, err := Sign(dir, "v1", priv)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	manifest := DefaultManifestPath(dir)
	if err := WriteManifest(manifest, m); err != nil {
		t.Fatalf("WriteManifest: %v", err)
	}

	pdp := policy.NewOPADecider()
	l := NewVerifyingLoader(pdp, manifest, pub)
	if err := l.Load(dir); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := pdp.Ready(); err != nil {
		t.Fatalf("Ready after a verified load: %v", err)
	}

	decide := func() types.DecisionResponse {
		t.Helper()
		resp, err := pdp.Decide(context.Background(), types.DecisionRequest{
			Subject: types.Subject{ID: "agent:test", Kind: types.SubjectKindAgent},
			Action:  types.Action{Type: "tool_call", Tool: "kb.search"},
			Context: types.DecisionContext{Time: time.Now()},
		})
		if err != nil {
			t.Fatalf("Decide: %v", err)
		}
		return resp
	}
	if got := decide(); got.PolicyID != "signed.v1" {
		t.Fatalf("decision came from %q, want the signed bundle's rule", got.PolicyID)
	}

	// An attacker (or a careless operator) swaps in an allow-everything
	// rule without re-signing.
	write(t, filepath.Join(dir, "decision.rego"), `package helmdeep.authz

default decision := {"outcome": "allow", "policy_id": "unsigned.allow_all"}
`)
	if err := l.Reload(); err == nil {
		t.Fatal("Reload: expected an error for an unsigned edit to the bundle")
	}
	if got := decide(); got.PolicyID != "signed.v1" || got.Outcome != types.OutcomeDeny {
		t.Fatalf("after a refused reload the PDP decided %+v, want the signed bundle's deny", got)
	}
	if err := pdp.Ready(); err != nil {
		t.Fatalf("Ready after a refused reload: %v", err)
	}
}
