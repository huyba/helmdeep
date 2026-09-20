// Copyright (c) 2026 Overarching AI LLC
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/huyba/helmdeep/internal/gateway"
	"github.com/huyba/helmdeep/internal/mockupstream"
	"github.com/huyba/helmdeep/pkg/audit"
	"github.com/huyba/helmdeep/pkg/mcp"
	"github.com/huyba/helmdeep/pkg/policy"
	"github.com/huyba/helmdeep/pkg/toolregistry"
	"github.com/huyba/helmdeep/pkg/types"
)

// newToolRegistryTestGateway wires a Gateway against
// examples/policies-tool-registry/ instead of the default examples/policies/
// bundle, with one mock upstream exposing a single tool declared PII and
// scope-gated in the Tool Registry — Milestone M2's own worked example
// (see that bundle's README), run for real over HTTP rather than just
// evaluated in isolation.
func newToolRegistryTestGateway(t *testing.T, callerScopes []string) (endpoint string, mock *mockupstream.Server) {
	t.Helper()

	mock = mockupstream.NewCustomHandler([]mockupstream.ToolDef{
		{
			Name: "crm.export_contacts",
			Handler: func(map[string]any) (map[string]any, bool, string) {
				return map[string]any{"contacts": []any{"a@example.com"}, "notes": "internal reviewer notes"}, false, ""
			},
		},
	})
	ts := httptest.NewServer(mock)
	t.Cleanup(ts.Close)

	upstreams := []mcp.Upstream{mcp.NewHTTPUpstream("crm", ts.URL, "")}
	registry, err := mcp.NewStaticRegistry(context.Background(), upstreams)
	if err != nil {
		t.Fatalf("NewStaticRegistry: %v", err)
	}

	pdp := policy.NewOPADecider()
	if err := pdp.Load(mustAbs(t, "../../examples/policies-tool-registry")); err != nil {
		t.Fatalf("Load policy: %v", err)
	}

	auditStore, err := audit.NewFileStore(filepath.Join(t.TempDir(), "audit.log"))
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}

	const token = "test-token"
	resolver := gateway.NewStaticTokenResolver(map[string]gateway.StaticIdentity{
		token: {ID: "agent:test", Kind: types.SubjectKindAgent, Scopes: callerScopes},
	})

	toolReg, err := toolregistry.New([]toolregistry.Entry{
		{
			ToolID:      "crm.export_contacts",
			Upstream:    "crm",
			Risk:        toolregistry.RiskHigh,
			DataClasses: []string{"pii"},
			Scopes:      []string{"crm.pii_read"},
		},
	})
	if err != nil {
		t.Fatalf("toolregistry.New: %v", err)
	}

	gw := gateway.New(resolver, pdp, auditStore, registry, nil, 0, nil, toolReg)
	gwServer := mcp.NewServer(":0", "/mcp", gw, "e2e-test")
	gwTS := httptest.NewServer(gwServer.Handler())
	t.Cleanup(gwTS.Close)

	return gwTS.URL + "/mcp", mock
}

// TestPIIToolDeniedWithoutRequiredScope exercises
// examples/policies-tool-registry/decision.rego's central rule: a tool
// whose Tool Registry entry declares data class "pii" is denied to a
// caller that lacks the tool's required scope, regardless of anything
// else about the call — the "data class PII denied for agents without
// scope Y" example from the Tool Gateway design doc, run for real.
func TestPIIToolDeniedWithoutRequiredScope(t *testing.T) {
	endpoint, mock := newToolRegistryTestGateway(t, nil) // no scopes granted

	resp := resultOf(t, callTool(t, endpoint, "test-token", "crm.export_contacts", nil))
	if isErr, _ := resp["isError"].(bool); !isErr {
		t.Fatalf("expected a PII tool to be denied without the required scope: %v", resp)
	}
	if calls := toolCalls(mock.Calls()); len(calls) != 0 {
		t.Fatal("upstream was called despite the missing-scope denial")
	}
}

// TestHighRiskToolAllowedWithScopeButRedacted is the same tool, called by a
// caller that does hold crm.pii_read: the call is allowed (the scope check
// passes), but because the tool is also risk tier "high", the result comes
// back with its "notes" field redacted — the obligation the policy attaches
// pending doc 05's not-yet-built approval workflow.
func TestHighRiskToolAllowedWithScopeButRedacted(t *testing.T) {
	endpoint, mock := newToolRegistryTestGateway(t, []string{"crm.pii_read"})

	resp := resultOf(t, callTool(t, endpoint, "test-token", "crm.export_contacts", nil))
	if isErr, _ := resp["isError"].(bool); isErr {
		t.Fatalf("expected the call to be allowed with the required scope granted: %v", resp)
	}
	structured, _ := resp["structuredContent"].(map[string]any)
	if structured["notes"] != "[REDACTED]" {
		t.Fatalf("expected the high-risk tool's 'notes' field to be redacted, got: %v", structured)
	}
	if structured["contacts"] == nil {
		t.Fatalf("expected the rest of the result to pass through unredacted: %v", structured)
	}
	if calls := toolCalls(mock.Calls()); len(calls) != 1 {
		t.Fatalf("got %d upstream calls, want 1", len(calls))
	}
}
