// Copyright (c) 2026 Overarching AI LLC
// SPDX-License-Identifier: Apache-2.0

package modelgw

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/huyba/helmdeep/pkg/audit"
	"github.com/huyba/helmdeep/pkg/types"
)

// stubProvider returns a fixed Response/error and records every call it
// received, so tests can assert on both what a Gateway decided to do and
// what it actually sent.
type stubProvider struct {
	name    string
	resp    Response
	err     error
	calls   []Request
	called  bool
	modelIn string
}

func (p *stubProvider) Name() string { return p.name }
func (p *stubProvider) Complete(_ context.Context, model string, req Request) (Response, error) {
	p.called = true
	p.modelIn = model
	p.calls = append(p.calls, req)
	if p.err != nil {
		return Response{}, p.err
	}
	resp := p.resp
	resp.Provider = p.name
	if resp.Model == "" {
		resp.Model = model
	}
	return resp, nil
}

type stubDecider struct {
	resp types.DecisionResponse
	err  error
}

func (d stubDecider) Decide(context.Context, types.DecisionRequest) (types.DecisionResponse, error) {
	return d.resp, d.err
}

type recordingAuditStore struct {
	records []types.ActionRecord
	err     error
}

func (s *recordingAuditStore) Append(_ context.Context, rec types.ActionRecord) error {
	if s.err != nil {
		return s.err
	}
	s.records = append(s.records, rec)
	return nil
}
func (s *recordingAuditStore) Verify(context.Context) error { return nil }

var _ audit.Store = (*recordingAuditStore)(nil)

const testPurpose = "reasoning"

func testSubject() types.Subject {
	return types.Subject{ID: "agent:test", Kind: types.SubjectKindAgent}
}

func TestNew_RejectsLatestAsAModelVersion(t *testing.T) {
	providers := map[string]Provider{"p": &stubProvider{name: "p"}}
	_, err := New(providers, map[string]Route{testPurpose: {Provider: "p", Model: "latest"}}, stubDecider{}, &recordingAuditStore{})
	if err == nil {
		t.Fatal("New: expected an error for a route pinned to \"latest\"")
	}
}

func TestNew_RejectsUnknownProvider(t *testing.T) {
	_, err := New(map[string]Provider{}, map[string]Route{testPurpose: {Provider: "nope", Model: "v1"}}, stubDecider{}, &recordingAuditStore{})
	if err == nil {
		t.Fatal("New: expected an error for a route referencing an unconfigured provider")
	}
}

func TestComplete_AllowedCallReachesProviderAndRecordsTwoRecords(t *testing.T) {
	p := &stubProvider{name: "p", resp: Response{Model: "v1-resolved", Content: "hi"}}
	store := &recordingAuditStore{}
	gw, err := New(map[string]Provider{"p": p}, map[string]Route{testPurpose: {Provider: "p", Model: "v1"}},
		stubDecider{resp: types.DecisionResponse{Outcome: types.OutcomeAllow, PolicyID: "test"}}, store)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	resp, err := gw.Complete(context.Background(), testSubject(), Request{Purpose: testPurpose, Messages: []Message{{Role: "user", Content: "hi"}}})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if !p.called || p.modelIn != "v1" {
		t.Fatalf("provider called=%v with model=%q, want called with the pinned version v1", p.called, p.modelIn)
	}
	if resp.Model != "v1-resolved" {
		t.Fatalf("Response.Model = %q, want the provider's resolved version", resp.Model)
	}
	if len(store.records) != 2 {
		t.Fatalf("got %d audit records, want 2 (decision, then outcome)", len(store.records))
	}
	if store.records[0].Outcome != types.RecordOutcomeAllowed || store.records[1].Outcome != types.RecordOutcomeAllowed {
		t.Fatalf("records = %+v, want both allowed", store.records)
	}
}

func TestComplete_DeniedByPolicyNeverCallsProvider(t *testing.T) {
	p := &stubProvider{name: "p"}
	store := &recordingAuditStore{}
	gw, err := New(map[string]Provider{"p": p}, map[string]Route{testPurpose: {Provider: "p", Model: "v1"}},
		stubDecider{resp: types.DecisionResponse{Outcome: types.OutcomeDeny, PolicyID: "test.deny", Reason: "no"}}, store)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = gw.Complete(context.Background(), testSubject(), Request{Purpose: testPurpose})
	var denied *ErrDenied
	if !errors.As(err, &denied) {
		t.Fatalf("Complete: got %v, want *ErrDenied", err)
	}
	if p.called {
		t.Fatal("provider was called despite a policy denial")
	}
	if len(store.records) != 1 || store.records[0].Outcome != types.RecordOutcomeDenied {
		t.Fatalf("records = %+v, want exactly one denied record", store.records)
	}
}

// TestComplete_ZeroValueDecisionResponseIsDenied is modelgw's own version
// of internal/gateway's highest-value test: a Decider returning a bare
// DecisionResponse{} (Outcome's zero value) must not be treated as an
// allow, here either — this logic is a fresh copy of the Tool Gateway's,
// not a shared helper, so it needs its own regression lock.
func TestComplete_ZeroValueDecisionResponseIsDenied(t *testing.T) {
	p := &stubProvider{name: "p"}
	gw, err := New(map[string]Provider{"p": p}, map[string]Route{testPurpose: {Provider: "p", Model: "v1"}}, stubDecider{}, &recordingAuditStore{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := gw.Complete(context.Background(), testSubject(), Request{Purpose: testPurpose}); err == nil {
		t.Fatal("Complete: a zero-value DecisionResponse was treated as an allow")
	}
	if p.called {
		t.Fatal("provider was called despite a zero-value decision")
	}
}

func TestComplete_UnknownPurposeIsDenied(t *testing.T) {
	gw, err := New(map[string]Provider{}, map[string]Route{}, stubDecider{resp: types.DecisionResponse{Outcome: types.OutcomeAllow}}, &recordingAuditStore{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := gw.Complete(context.Background(), testSubject(), Request{Purpose: "nonexistent"}); err == nil {
		t.Fatal("Complete: expected an error for a purpose with no configured route")
	}
}

func TestComplete_AuditWriteFailureDeniesAndDoesNotCallProvider(t *testing.T) {
	p := &stubProvider{name: "p"}
	store := &recordingAuditStore{err: errors.New("disk full")}
	gw, err := New(map[string]Provider{"p": p}, map[string]Route{testPurpose: {Provider: "p", Model: "v1"}},
		stubDecider{resp: types.DecisionResponse{Outcome: types.OutcomeAllow, PolicyID: "test"}}, store)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := gw.Complete(context.Background(), testSubject(), Request{Purpose: testPurpose}); err == nil {
		t.Fatal("Complete: an unrecordable decision was not treated as a deny")
	}
	if p.called {
		t.Fatal("provider was called despite the audit write failing")
	}
}

func TestComplete_FallsBackOnPrimaryFailureAndRecordsIt(t *testing.T) {
	primary := &stubProvider{name: "primary", err: errors.New("rate limited")}
	fallback := &stubProvider{name: "fallback", resp: Response{Model: "fb-v1", Content: "from fallback"}}
	store := &recordingAuditStore{}
	gw, err := New(
		map[string]Provider{"primary": primary, "fallback": fallback},
		map[string]Route{testPurpose: {Provider: "primary", Model: "v1", FallbackProvider: "fallback", FallbackModel: "fb-1"}},
		stubDecider{resp: types.DecisionResponse{Outcome: types.OutcomeAllow, PolicyID: "test"}}, store,
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	resp, err := gw.Complete(context.Background(), testSubject(), Request{Purpose: testPurpose})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if !primary.called || !fallback.called {
		t.Fatalf("primary.called=%v fallback.called=%v, want both true", primary.called, fallback.called)
	}
	if resp.Provider != "fallback" || resp.FallbackFrom != "primary" {
		t.Fatalf("got %+v, want a response from fallback with FallbackFrom=primary", resp)
	}
	// The fallback must be visible in the ledger itself, not only in the
	// in-process Response — doc 07 §3.2: "Fallback is recorded, never
	// silent."
	found := false
	for _, r := range store.records {
		if r.Action.Resource != "" && r.Outcome == types.RecordOutcomeAllowed {
			found = true
			if !strings.Contains(r.Action.Resource, "fallback") {
				t.Fatalf("outcome record's Action.Resource = %q, want it to mention the fallback", r.Action.Resource)
			}
		}
	}
	if !found {
		t.Fatal("no outcome record recorded the fallback")
	}
}

func TestComplete_BothPrimaryAndFallbackFailingReturnsError(t *testing.T) {
	primary := &stubProvider{name: "primary", err: errors.New("down")}
	fallback := &stubProvider{name: "fallback", err: errors.New("also down")}
	store := &recordingAuditStore{}
	gw, err := New(
		map[string]Provider{"primary": primary, "fallback": fallback},
		map[string]Route{testPurpose: {Provider: "primary", Model: "v1", FallbackProvider: "fallback", FallbackModel: "fb-1"}},
		stubDecider{resp: types.DecisionResponse{Outcome: types.OutcomeAllow, PolicyID: "test"}}, store,
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := gw.Complete(context.Background(), testSubject(), Request{Purpose: testPurpose}); err == nil {
		t.Fatal("Complete: expected an error when both primary and fallback fail")
	}
	if len(store.records) != 2 || store.records[1].Outcome != types.RecordOutcomeFailed {
		t.Fatalf("records = %+v, want [allowed, failed]", store.records)
	}
}
