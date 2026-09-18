// Copyright (c) 2026 Overarching AI LLC
// SPDX-License-Identifier: Apache-2.0

package gateway

import (
	"context"
	"testing"

	"github.com/huyba/helmdeep/pkg/audit"
	"github.com/huyba/helmdeep/pkg/mcp"
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

func newTestGateway(t *testing.T, decider stubDecider) (*Gateway, *spyUpstream, audit.Store) {
	t.Helper()
	upstream := &spyUpstream{name: "test-upstream"}
	registry := &staticRegistry{tool: types.Tool{Name: "some.tool"}, upstream: upstream}
	auditStore, err := audit.NewFileStore(t.TempDir() + "/audit.log")
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	subject := types.Subject{ID: "agent:test", Kind: types.SubjectKindAgent}
	gw := New(stubResolver{subject: subject}, decider, auditStore, registry, nil)
	return gw, upstream, auditStore
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
