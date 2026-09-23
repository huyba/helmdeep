// Copyright (c) 2026 Overarching AI LLC
// SPDX-License-Identifier: Apache-2.0

package policyservice

import (
	"context"

	"github.com/huyba/helmdeep/pkg/audit"
	"github.com/huyba/helmdeep/pkg/types"
)

// BundleRef answers "which policy bundle is loaded right now". A
// VerifyingLoader implements it; so can any future bundle source.
type BundleRef interface {
	Active() types.PolicyBundleRef
}

// StampingStore decorates a pkg/audit.Store so every Action Record names
// the policy bundle that produced its decision, answering a question the
// ledger could not answer before: `policy_id` says which *rule* fired, but
// not which *version of the bundle* that rule came from — and a rule id is
// not stable across edits. docs/09-governance-trust.md §2.2 calls for a
// bundle "versioned... pinned per session"; this is the read side of that,
// recorded per decision.
//
// It is a decorator rather than a parameter on each gateway for two
// reasons: the stamp is the same fact for every record regardless of which
// component wrote it (so both internal/gateway and pkg/modelgw get it from
// one wiring change, with no signature change to either), and a record's
// bundle reference is something the platform asserts, never something a
// caller can influence — which is the same reason
// docs/adr/0004-action-record-format.md gives for the gateway owning
// record construction in the first place.
//
// The stamp is authenticated: types.ActionRecord.PolicyBundle is part of
// what pkg/audit hashes, so altering a record's bundle reference after the
// fact breaks the chain exactly like altering its decision does.
type StampingStore struct {
	inner audit.Store
	ref   BundleRef
}

// NewStampingStore wraps inner so that each appended record carries ref's
// currently active bundle version and digest.
func NewStampingStore(inner audit.Store, ref BundleRef) *StampingStore {
	return &StampingStore{inner: inner, ref: ref}
}

// Append stamps rec with the active bundle reference and appends it.
//
// If no bundle is loaded yet (the zero reference), the record is appended
// unstamped rather than with an empty stamp: a record carrying an empty
// version and digest claims to know something it doesn't, and an absent
// field is how every other unknown in this ledger is represented. In practice this
// only covers records written before the first successful load — the
// gateway refuses to start without one.
// Whatever rec arrives carrying is discarded either way: the reference is
// the platform's assertion about which bundle was in force, so this
// decorator is its only source, and a caller cannot contribute one.
func (s *StampingStore) Append(ctx context.Context, rec types.ActionRecord) error {
	rec.PolicyBundle = nil
	if ref := s.ref.Active(); ref != (types.PolicyBundleRef{}) {
		rec.PolicyBundle = &ref
	}
	return s.inner.Append(ctx, rec)
}

// Verify delegates: chain integrity is the inner store's business.
func (s *StampingStore) Verify(ctx context.Context) error {
	return s.inner.Verify(ctx)
}

var _ audit.Store = (*StampingStore)(nil)
