// Copyright (c) 2026 Overarching AI LLC
// SPDX-License-Identifier: Apache-2.0

package policyservice

import (
	"crypto/ed25519"
	"fmt"
	"sync"

	"github.com/huyba/helmdeep/pkg/policy"
	"github.com/huyba/helmdeep/pkg/types"
)

// VerifyingLoader decorates a pkg/policy.Loader so that a bundle is
// verified against its signed manifest before it is ever compiled: an
// unsigned, modified, or wrongly-signed bundle is refused, and the inner
// loader never sees it.
//
// Two properties this shape buys, both of them tested:
//
//   - A failed verification on Reload leaves the previously active policy
//     in place, exactly like a failed compile already did
//     (pkg/policy.OPADecider.Reload) — a tampered or half-written bundle
//     must never leave the gateway with no policy loaded, which combined
//     with fail-closed evaluation would deny every request.
//   - Verification cannot be forgotten by a caller that has one of these:
//     there is no path to the inner Load that skips it. The decision to
//     run unverified is made once, at wiring time, by constructing the
//     plain pkg/policy.OPADecider instead — see cmd/helmdeep-gateway.
//
// Safe for concurrent use; Load and Reload are serialized against each
// other and against Active.
type VerifyingLoader struct {
	inner        policy.Loader
	manifestPath string
	pub          ed25519.PublicKey

	mu     sync.RWMutex
	path   string // bundle path from the last Load, remembered for Reload
	active types.PolicyBundleRef
}

// NewVerifyingLoader wraps inner so every load verifies the bundle against
// the manifest at manifestPath, signed by pub.
func NewVerifyingLoader(inner policy.Loader, manifestPath string, pub ed25519.PublicKey) *VerifyingLoader {
	return &VerifyingLoader{inner: inner, manifestPath: manifestPath, pub: pub}
}

// Load verifies the bundle at path and, only if it verifies, compiles it
// via the inner loader. A verification failure returns an error and changes
// nothing — neither the inner loader's active policy nor Active's answer.
func (l *VerifyingLoader) Load(path string) error {
	ref, err := l.verify(path)
	if err != nil {
		return err
	}
	if err := l.inner.Load(path); err != nil {
		// The bundle is authentic but doesn't compile. Leave Active
		// pointing at whatever is really loaded (the previous bundle, or
		// nothing yet) rather than at a bundle the PDP rejected.
		return err
	}
	l.mu.Lock()
	l.path = path
	l.active = ref
	l.mu.Unlock()
	return nil
}

// Reload re-verifies and recompiles the bundle from the path passed to the
// last successful Load. A bundle that no longer matches its manifest — the
// signature is wrong, a file changed, a file was added or removed — is
// refused here, so the hot-reload path is not a way around the check the
// startup path enforces.
func (l *VerifyingLoader) Reload() error {
	l.mu.RLock()
	path := l.path
	l.mu.RUnlock()
	if path == "" {
		return fmt.Errorf("reload called before a successful Load")
	}
	return l.Load(path)
}

// Active reports the bundle version and digest currently loaded, or the
// zero value if no bundle has been successfully loaded yet. This is what a
// StampingStore writes into every Action Record.
func (l *VerifyingLoader) Active() types.PolicyBundleRef {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.active
}

// verify reads the manifest, checks the bundle against it, and returns the
// reference to stamp records with if it passes.
func (l *VerifyingLoader) verify(path string) (types.PolicyBundleRef, error) {
	m, err := ReadManifest(l.manifestPath)
	if err != nil {
		return types.PolicyBundleRef{}, err
	}
	if err := Verify(path, m, l.pub); err != nil {
		return types.PolicyBundleRef{}, fmt.Errorf("policy bundle at %q failed verification against %q: %w", path, l.manifestPath, err)
	}
	digest, err := m.Digest()
	if err != nil {
		return types.PolicyBundleRef{}, err
	}
	return types.PolicyBundleRef{Version: m.Version, Digest: digest}, nil
}

var _ policy.Loader = (*VerifyingLoader)(nil)
var _ BundleRef = (*VerifyingLoader)(nil)
