// Copyright (c) 2026 Overarching AI LLC
// SPDX-License-Identifier: Apache-2.0

package policyservice

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/huyba/helmdeep/pkg/audit"
	"github.com/huyba/helmdeep/pkg/types"
)

type recordingStore struct {
	records []types.ActionRecord
	err     error
}

func (s *recordingStore) Append(_ context.Context, rec types.ActionRecord) error {
	if s.err != nil {
		return s.err
	}
	s.records = append(s.records, rec)
	return nil
}

func (s *recordingStore) Verify(context.Context) error { return nil }

var _ audit.Store = (*recordingStore)(nil)

// staticRef is a BundleRef that always reports the same bundle.
type staticRef types.PolicyBundleRef

func (r staticRef) Active() types.PolicyBundleRef { return types.PolicyBundleRef(r) }

func TestStampingStore_StampsEveryRecord(t *testing.T) {
	inner := &recordingStore{}
	ref := staticRef{Version: "v7", Digest: "sha256:abc"}
	store := NewStampingStore(inner, ref)

	for i := 0; i < 2; i++ {
		if err := store.Append(context.Background(), types.ActionRecord{
			Timestamp: time.Now(),
			Subject:   types.Subject{ID: "agent:finance-bot", Kind: types.SubjectKindAgent},
			Action:    types.Action{Type: "tool_call", Tool: "kb.search"},
			Decision:  types.DecisionResponse{Outcome: types.OutcomeAllow, PolicyID: "combined.allow"},
			Outcome:   types.RecordOutcomeAllowed,
		}); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	if len(inner.records) != 2 {
		t.Fatalf("inner store holds %d records, want 2", len(inner.records))
	}
	for i, rec := range inner.records {
		if rec.PolicyBundle == nil {
			t.Fatalf("record %d was appended without a policy bundle reference", i)
		}
		if *rec.PolicyBundle != types.PolicyBundleRef(ref) {
			t.Fatalf("record %d carries %+v, want %+v", i, *rec.PolicyBundle, types.PolicyBundleRef(ref))
		}
	}
}

func TestStampingStore_NoBundleLoadedLeavesTheRecordUnstamped(t *testing.T) {
	inner := &recordingStore{}
	store := NewStampingStore(inner, staticRef{})
	if err := store.Append(context.Background(), types.ActionRecord{Outcome: types.RecordOutcomeDenied}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if inner.records[0].PolicyBundle != nil {
		t.Fatalf("record carries %+v, want no reference at all", *inner.records[0].PolicyBundle)
	}
}

func TestStampingStore_OverwritesACallerSuppliedReference(t *testing.T) {
	// The bundle reference is the platform's assertion, not an input: a
	// caller that sets one (a stale record being replayed, a component that
	// guessed) must not be able to make the ledger say a different bundle
	// decided this action than the one that did.
	inner := &recordingStore{}
	store := NewStampingStore(inner, staticRef{Version: "real", Digest: "sha256:real"})
	spoofed := types.PolicyBundleRef{Version: "spoofed", Digest: "sha256:spoofed"}
	if err := store.Append(context.Background(), types.ActionRecord{PolicyBundle: &spoofed}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if got := *inner.records[0].PolicyBundle; got.Version != "real" {
		t.Fatalf("record carries %+v, want the active bundle", got)
	}
}

func TestStampingStore_PropagatesAppendFailure(t *testing.T) {
	// Fail-closed depends on an Append error reaching the gateway (it
	// denies the call rather than proceeding unrecorded), so the decorator
	// must not swallow one.
	appendErr := errors.New("disk full")
	store := NewStampingStore(&recordingStore{err: appendErr}, staticRef{Version: "v1"})
	if err := store.Append(context.Background(), types.ActionRecord{}); !errors.Is(err, appendErr) {
		t.Fatalf("Append error = %v, want the inner store's error", err)
	}
}

// TestStampedRecordsAreHashedAndChainVerifies pins the property that makes
// the stamp worth anything: it is inside the hash chain, so it can't be
// edited after the fact without breaking verification.
func TestStampedRecordsAreHashedAndChainVerifies(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.log")
	fileStore, err := audit.NewFileStore(path)
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	store := NewStampingStore(fileStore, staticRef{Version: "v1", Digest: "sha256:deadbeef"})
	rec := types.ActionRecord{
		Timestamp: time.Now().UTC(),
		Subject:   types.Subject{ID: "agent:finance-bot", Kind: types.SubjectKindAgent},
		Action:    types.Action{Type: "tool_call", Tool: "payments.wire_transfer"},
		Decision:  types.DecisionResponse{Outcome: types.OutcomeDeny, PolicyID: "taint.deny"},
		Outcome:   types.RecordOutcomeDenied,
	}
	if err := store.Append(context.Background(), rec); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := store.Verify(context.Background()); err != nil {
		t.Fatalf("Verify: %v", err)
	}

	data, err := os.ReadFile(path) // #nosec G304 -- test-controlled temp path
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if !strings.Contains(string(data), `"policy_bundle":{"version":"v1","digest":"sha256:deadbeef"}`) {
		t.Fatalf("log line does not carry the bundle reference: %s", data)
	}

	// Rewrite the bundle version in place, leaving the stored hash alone —
	// the exact move someone covering their tracks would make to claim a
	// different policy was in force.
	tampered := strings.Replace(string(data), `"version":"v1"`, `"version":"v9"`, 1)
	if tampered == string(data) {
		t.Fatal("test bug: nothing was tampered with")
	}
	if err := os.WriteFile(path, []byte(tampered), 0o600); err != nil { // #nosec G703 -- test-controlled temp path
		t.Fatalf("write tampered log: %v", err)
	}
	reopened, err := audit.NewFileStore(path)
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	if err := reopened.Verify(context.Background()); err == nil {
		t.Fatal("Verify: a record whose bundle reference was rewritten must break the chain")
	}
}

// TestUnstampedRecordsHashAsTheyAlwaysDid is the backward-compatibility
// guarantee: a ledger written before this field existed still verifies,
// because an absent policy_bundle is absent from what gets hashed too.
func TestUnstampedRecordsHashAsTheyAlwaysDid(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.log")
	store, err := audit.NewFileStore(path)
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	if err := store.Append(context.Background(), types.ActionRecord{
		Timestamp: time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC),
		Subject:   types.Subject{ID: "agent:support-bot", Kind: types.SubjectKindAgent},
		Action:    types.Action{Type: "tool_call", Tool: "kb.search"},
		Decision:  types.DecisionResponse{Outcome: types.OutcomeAllow, PolicyID: "combined.allow"},
		Outcome:   types.RecordOutcomeAllowed,
	}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	data, err := os.ReadFile(path) // #nosec G304 -- test-controlled temp path
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	var written map[string]any
	if err := json.Unmarshal(data, &written); err != nil {
		t.Fatalf("parse record: %v", err)
	}
	if _, present := written["policy_bundle"]; present {
		t.Fatal("an unstamped record serialized a policy_bundle key, which would change every pre-existing record's hash")
	}
	if err := store.Verify(context.Background()); err != nil {
		t.Fatalf("Verify: %v", err)
	}
}
