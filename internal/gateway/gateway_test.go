// Copyright (c) 2026 Overarching AI LLC
// SPDX-License-Identifier: Apache-2.0

package gateway

import (
	"context"
	"testing"
	"time"

	"github.com/huyba/helmdeep/pkg/audit"
	"github.com/huyba/helmdeep/pkg/mcp"
	"github.com/huyba/helmdeep/pkg/policy"
	"github.com/huyba/helmdeep/pkg/types"
)

// stubResolver always resolves to the same subject, regardless of credential.
type stubResolver struct{ subject types.Subject }

func (r stubResolver) Resolve(context.Context, string) (types.Subject, error) {
	return r.subject, nil
}

// stubDecider returns a fixed DecisionResponse (and error) on every call,
// so tests can force the exact PDP behavior under examination without
// going anywhere near OPA.
type stubDecider struct {
	resp types.DecisionResponse
	err  error
}

func (d stubDecider) Decide(context.Context, types.DecisionRequest) (types.DecisionResponse, error) {
	return d.resp, d.err
}

// slowDecider blocks for delay, or until ctx is cancelled, whichever comes
// first — used to test that the gateway's decision timeout actually cuts a
// hanging Decide short rather than waiting on it forever.
type slowDecider struct {
	resp  types.DecisionResponse
	delay time.Duration
}

func (d slowDecider) Decide(ctx context.Context, _ types.DecisionRequest) (types.DecisionResponse, error) {
	select {
	case <-time.After(d.delay):
		return d.resp, nil
	case <-ctx.Done():
		return types.DecisionResponse{}, ctx.Err()
	}
}

// failingAuditStore always errors on Append, to test that an unrecordable
// decision denies rather than proceeding — see
// docs/adr/0006-operational-failure-classification.md, Decision 2.
type failingAuditStore struct{ err error }

func (s failingAuditStore) Append(context.Context, types.ActionRecord) error { return s.err }
func (s failingAuditStore) Verify(context.Context) error                     { return nil }

// recordingAuditStore wraps a real audit.Store and remembers every record
// it was asked to append, so a test can inspect what actually got written.
type recordingAuditStore struct {
	audit.Store
	records []types.ActionRecord
}

func (s *recordingAuditStore) Append(ctx context.Context, rec types.ActionRecord) error {
	s.records = append(s.records, rec)
	return s.Store.Append(ctx, rec)
}

// spyUpstream records whether it was ever called. Tests use it to assert
// the strongest form of "denied" — not just "the agent got an error back"
// but "the upstream never saw the request at all."
type spyUpstream struct {
	name    string
	tools   []types.Tool
	called  bool
	result  types.ToolResult
	callErr error
}

func (u *spyUpstream) Name() string { return u.name }
func (u *spyUpstream) ListTools(context.Context) ([]types.Tool, error) {
	return u.tools, nil
}
func (u *spyUpstream) CallTool(context.Context, types.ToolCall) (types.ToolResult, error) {
	u.called = true
	return u.result, u.callErr
}

// staticRegistry is a trivial mcp.Registry over one upstream owning one tool.
type staticRegistry struct {
	tool     types.Tool
	upstream *spyUpstream
}

func (r *staticRegistry) Upstreams() []mcp.Upstream { return []mcp.Upstream{r.upstream} }
func (r *staticRegistry) Tools() []types.Tool       { return []types.Tool{r.tool} }
func (r *staticRegistry) Resolve(toolName string) (mcp.Upstream, bool) {
	if toolName == r.tool.Name {
		return r.upstream, true
	}
	return nil, false
}

func newTestGateway(t *testing.T, decider policy.Decider) (*Gateway, *spyUpstream, audit.Store) {
	t.Helper()
	gw, upstream, store, _ := newTestGatewayWithOpts(t, decider, 0, nil)
	return gw, upstream, store
}

// newTestGatewayWithOpts is the full-control constructor the rest of
// newTestGateway's callers don't need. auditOverride, if non-nil, replaces
// the real audit.Store entirely (used to simulate an unwritable audit
// sink); otherwise a real temp-file FileStore is used so tests can also
// exercise the real hash chain if they want to.
func newTestGatewayWithOpts(t *testing.T, decider policy.Decider, decisionTimeout time.Duration, auditOverride audit.Store) (*Gateway, *spyUpstream, audit.Store, *staticRegistry) {
	t.Helper()
	upstream := &spyUpstream{name: "test-upstream"}
	registry := &staticRegistry{tool: types.Tool{Name: "some.tool"}, upstream: upstream}

	var store audit.Store
	if auditOverride != nil {
		store = auditOverride
	} else {
		fileStore, err := audit.NewFileStore(t.TempDir() + "/audit.log")
		if err != nil {
			t.Fatalf("NewFileStore: %v", err)
		}
		store = fileStore
	}

	subject := types.Subject{ID: "agent:test", Kind: types.SubjectKindAgent}
	gw := New(stubResolver{subject: subject}, decider, store, registry, nil, decisionTimeout)
	return gw, upstream, store, registry
}

// TestZeroValueDecisionResponseIsDenied is the single highest-value test in
// this package: it verifies that a Decider returning a bare, unpopulated
// types.DecisionResponse{} — Outcome's zero value, "" — results in a deny,
// not an allow. types.Outcome has no Go-enforced exhaustiveness (it's a
// string type), so this invariant lives entirely in gateway.go's own logic
// reading it. Before the fix this test caught, that logic was
// `decision.Outcome != types.OutcomeDeny` — a deny-list that treated any
// unrecognized outcome, including the zero value, as an allow. It is now
// `decision.Outcome == types.OutcomeAllow || decision.Outcome ==
// types.OutcomeAllowWithObligations` — an allow-list, so a Decider
// implementation that forgets to set Outcome on some branch fails closed by
// construction rather than by convention.
func TestZeroValueDecisionResponseIsDenied(t *testing.T) {
	gw, upstream, _ := newTestGateway(t, stubDecider{resp: types.DecisionResponse{}})

	result, err := gw.CallTool(context.Background(), mcp.CallContext{Credential: "irrelevant"}, "some.tool", nil)
	if err != nil {
		t.Fatalf("CallTool returned a protocol error, want a denied tool result: %v", err)
	}
	if !result.IsError {
		t.Fatal("zero-value DecisionResponse was treated as an allow — this is the exact silent-vulnerability shape docs/adr/0003-fail-closed-behavior.md exists to rule out")
	}
	if upstream.called {
		t.Fatal("upstream was called despite a zero-value (denied) decision")
	}
}

func TestDecideErrorIsDenied(t *testing.T) {
	gw, upstream, _ := newTestGateway(t, stubDecider{
		resp: types.DecisionResponse{Outcome: types.OutcomeAllow}, // deliberately wrong, to prove err wins
		err:  errPDPUnreachable,
	})

	result, err := gw.CallTool(context.Background(), mcp.CallContext{Credential: "irrelevant"}, "some.tool", nil)
	if err != nil {
		t.Fatalf("CallTool returned a protocol error, want a denied tool result: %v", err)
	}
	if !result.IsError {
		t.Fatal("Decide returning an error was not treated as a deny")
	}
	if upstream.called {
		t.Fatal("upstream was called despite Decide returning an error")
	}
}

func TestExplicitAllowReachesUpstream(t *testing.T) {
	gw, upstream, _ := newTestGateway(t, stubDecider{resp: types.DecisionResponse{Outcome: types.OutcomeAllow, PolicyID: "test"}})

	_, err := gw.CallTool(context.Background(), mcp.CallContext{Credential: "irrelevant"}, "some.tool", nil)
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !upstream.called {
		t.Fatal("an explicit allow did not reach upstream")
	}
}

var errPDPUnreachable = testErr("pdp unreachable")

type testErr string

func (e testErr) Error() string { return string(e) }

// TestDecisionTimeoutDenies proves the configured decision timeout is real,
// not just plumbed through and ignored: a Decider that hangs longer than
// the timeout must still produce a denial, promptly, rather than the
// request hanging until the Decider eventually (or never) returns.
func TestDecisionTimeoutDenies(t *testing.T) {
	const timeout = 30 * time.Millisecond
	decider := slowDecider{resp: types.DecisionResponse{Outcome: types.OutcomeAllow}, delay: 2 * time.Second}
	gw, upstream, _, _ := newTestGatewayWithOpts(t, decider, timeout, nil)

	start := time.Now()
	result, err := gw.CallTool(context.Background(), mcp.CallContext{Credential: "irrelevant"}, "some.tool", nil)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("CallTool returned a protocol error, want a denied tool result: %v", err)
	}
	if !result.IsError {
		t.Fatal("a decision that timed out was treated as an allow")
	}
	if upstream.called {
		t.Fatal("upstream was called despite the decision timing out")
	}
	// Generous upper bound: this only needs to prove the gateway didn't
	// wait anywhere near the decider's 2s delay, not pin down an exact
	// scheduling latency.
	if elapsed > 500*time.Millisecond {
		t.Fatalf("CallTool took %s, want well under the decider's 2s delay (timeout was %s)", elapsed, timeout)
	}
}

// TestAuditAppendFailureDeniesAndDoesNotCallUpstream is Decision 2 of
// docs/adr/0006-operational-failure-classification.md, made concrete: if
// the gateway can't durably record a decision, it must not act on it, even
// though the PDP said allow.
func TestAuditAppendFailureDeniesAndDoesNotCallUpstream(t *testing.T) {
	decider := stubDecider{resp: types.DecisionResponse{Outcome: types.OutcomeAllow, PolicyID: "test"}}
	gw, upstream, _, _ := newTestGatewayWithOpts(t, decider, 0, failingAuditStore{err: errAuditUnavailable})

	result, err := gw.CallTool(context.Background(), mcp.CallContext{Credential: "irrelevant"}, "some.tool", nil)
	if err != nil {
		t.Fatalf("CallTool returned a protocol error, want a denied tool result: %v", err)
	}
	if !result.IsError {
		t.Fatal("an allow whose decision record couldn't be written was not treated as a deny")
	}
	if upstream.called {
		t.Fatal("upstream was called despite the audit write failing")
	}
}

var errAuditUnavailable = testErr("audit sink unavailable")

// TestDenialsProduceAuditRecords asserts, directly against the audit
// store, that a denied call is recorded — not just that the agent got a
// denial back. Denied actions are often the most valuable signal in the
// system (see the threat-model post this project's README links to); a
// gateway that only logs allows would be useless for exactly the
// investigation it exists to support.
func TestDenialsProduceAuditRecords(t *testing.T) {
	fileStore, err := audit.NewFileStore(t.TempDir() + "/audit.log")
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	recorder := &recordingAuditStore{Store: fileStore}
	decider := stubDecider{resp: types.DecisionResponse{Outcome: types.OutcomeDeny, PolicyID: "test.deny", Reason: "no"}}
	gw, _, _, _ := newTestGatewayWithOpts(t, decider, 0, recorder)

	if _, err := gw.CallTool(context.Background(), mcp.CallContext{Credential: "irrelevant"}, "some.tool", nil); err != nil {
		t.Fatalf("CallTool: %v", err)
	}

	if len(recorder.records) != 1 {
		t.Fatalf("got %d audit records, want 1", len(recorder.records))
	}
	rec := recorder.records[0]
	if rec.Outcome != types.RecordOutcomeDenied {
		t.Fatalf("record.Outcome = %q, want %q", rec.Outcome, types.RecordOutcomeDenied)
	}
	if rec.Decision.PolicyID != "test.deny" {
		t.Fatalf("record.Decision.PolicyID = %q, want %q", rec.Decision.PolicyID, "test.deny")
	}
}

// TestUsageWindowResets exercises the aggregate-limit counter directly: a
// call count that's high within one window must not still count against a
// caller once the window has genuinely rolled over. This is only
// deterministic because Gateway.now is an injectable clock — no real
// sleeping involved.
func TestUsageWindowResets(t *testing.T) {
	gw := New(stubResolver{subject: types.Subject{ID: "agent:test"}}, stubDecider{}, mustNewAuditStore(t), &staticRegistry{tool: types.Tool{Name: "t"}, upstream: &spyUpstream{}}, nil, 0)

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	gw.now = func() time.Time { return base }

	for i := 0; i < 3; i++ {
		if got := gw.usage.recordAndCount("agent:test", gw.now()); got != i+1 {
			t.Fatalf("call %d: calls_in_window = %d, want %d", i+1, got, i+1)
		}
	}

	// Advance well past the window (defaultUsageWindow is one minute).
	gw.now = func() time.Time { return base.Add(2 * time.Minute) }
	if got := gw.usage.recordAndCount("agent:test", gw.now()); got != 1 {
		t.Fatalf("after the window rolled over: calls_in_window = %d, want 1 (a fresh window)", got)
	}
}

func mustNewAuditStore(t *testing.T) audit.Store {
	t.Helper()
	s, err := audit.NewFileStore(t.TempDir() + "/audit.log")
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	return s
}
