// Copyright (c) 2026 Overarching AI LLC
// SPDX-License-Identifier: Apache-2.0

// Package e2e drives a fully wired Tool Gateway — real HTTP, the actual
// examples/policies/ bundle, a real hash-chained audit log, and mock
// upstream MCP servers — exactly as an unmodified MCP client would, over
// the wire. Unit tests elsewhere cover components in isolation; this test
// exists to catch the thing unit tests can't: components that each work
// individually but don't fit together.
package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/huyba/helmdeep/internal/gateway"
	"github.com/huyba/helmdeep/internal/mockupstream"
	"github.com/huyba/helmdeep/pkg/audit"
	"github.com/huyba/helmdeep/pkg/mcp"
	"github.com/huyba/helmdeep/pkg/policy"
	"github.com/huyba/helmdeep/pkg/types"
)

const (
	supportToken = "support-token"
	financeToken = "finance-token"
)

func startMockUpstream(t *testing.T, profile string) *httptest.Server {
	t.Helper()
	h, err := mockupstream.NewHandler(profile)
	if err != nil {
		t.Fatalf("mockupstream.NewHandler(%q): %v", profile, err)
	}
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)
	return ts
}

// newTestGateway wires the same components a real deployment would — see
// examples/quickstart/config.yaml, which this mirrors — and returns the
// gateway's HTTP endpoint plus its audit store (so the test can call
// Verify directly instead of shelling out to `verify-chain`).
func newTestGateway(t *testing.T) (endpoint string, auditStore *audit.FileStore) {
	t.Helper()

	kb := startMockUpstream(t, "knowledgebase")
	supplier := startMockUpstream(t, "suppliermaster")
	email := startMockUpstream(t, "email")
	payments := startMockUpstream(t, "payments")

	upstreams := []mcp.Upstream{
		mcp.NewHTTPUpstream("knowledgebase", kb.URL, ""),
		mcp.NewHTTPUpstream("suppliermaster", supplier.URL, ""),
		mcp.NewHTTPUpstream("email", email.URL, ""),
		mcp.NewHTTPUpstream("payments", payments.URL, ""),
	}
	registry, err := mcp.NewStaticRegistry(context.Background(), upstreams)
	if err != nil {
		t.Fatalf("NewStaticRegistry: %v", err)
	}

	pdp := policy.NewOPADecider()
	policyPath, err := filepath.Abs("../../examples/policies")
	if err != nil {
		t.Fatalf("resolve policy path: %v", err)
	}
	if err := pdp.Load(policyPath); err != nil {
		t.Fatalf("Load policy: %v", err)
	}

	auditPath := filepath.Join(t.TempDir(), "audit.log")
	auditStore, err = audit.NewFileStore(auditPath)
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}

	resolver := gateway.NewStaticTokenResolver(map[string]gateway.StaticIdentity{
		supportToken: {ID: "agent:support-bot", Kind: types.SubjectKindAgent},
		financeToken: {ID: "agent:finance-bot", Kind: types.SubjectKindAgent},
	})

	provenance := map[string]types.Provenance{
		"suppliermaster": {Source: "suppliermaster", Trusted: true},
		"email":          {Source: "email", Trusted: false},
	}

	gw := gateway.New(resolver, pdp, auditStore, registry, provenance)
	gwServer := mcp.NewServer(":0", "/mcp", gw, "e2e-test")

	ts := httptest.NewServer(gwServer.Handler())
	t.Cleanup(ts.Close)

	return ts.URL + "/mcp", auditStore
}

// call sends one MCP request exactly as a conforming 2026-07-28 client
// would — real headers, real `_meta` — and returns the decoded top-level
// JSON-RPC response.
func call(t *testing.T, endpoint, token, method, name string, params map[string]any) map[string]any {
	t.Helper()
	if params == nil {
		params = map[string]any{}
	}
	params["_meta"] = map[string]any{
		"io.modelcontextprotocol/protocolVersion":    "2026-07-28",
		"io.modelcontextprotocol/clientCapabilities": map[string]any{},
	}
	body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("MCP-Protocol-Version", "2026-07-28")
	req.Header.Set("Mcp-Method", method)
	if name != "" {
		req.Header.Set("Mcp-Name", name)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return out
}

func callTool(t *testing.T, endpoint, token, tool string, arguments map[string]any) map[string]any {
	t.Helper()
	return call(t, endpoint, token, "tools/call", tool, map[string]any{"name": tool, "arguments": arguments})
}

func resultOf(t *testing.T, resp map[string]any) map[string]any {
	t.Helper()
	if e, ok := resp["error"]; ok {
		t.Fatalf("expected a result, got JSON-RPC error: %v", e)
	}
	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("response has no result object: %v", resp)
	}
	return result
}

func TestToolsListIsFilteredByScope(t *testing.T) {
	endpoint, _ := newTestGateway(t)

	resp := call(t, endpoint, supportToken, "tools/list", "", nil)
	result := resultOf(t, resp)
	tools, _ := result["tools"].([]any)

	var names []string
	for _, tl := range tools {
		m, _ := tl.(map[string]any)
		names = append(names, m["name"].(string))
	}
	if len(names) != 1 || names[0] != "kb.search" {
		t.Fatalf("support-bot's tools/list = %v, want exactly [kb.search]", names)
	}
}

func TestScopeDenyForOutOfScopeTool(t *testing.T) {
	endpoint, _ := newTestGateway(t)

	resp := callTool(t, endpoint, supportToken, "payments.wire_transfer", map[string]any{
		"account_number": "ACC-TRUSTED-0001", "amount_usd": 100,
	})
	result := resultOf(t, resp)
	if isErr, _ := result["isError"].(bool); !isErr {
		t.Fatalf("expected isError=true for an out-of-scope tool call, got %v", result)
	}
}

func TestUnknownToolIsAProtocolError(t *testing.T) {
	endpoint, _ := newTestGateway(t)

	resp := callTool(t, endpoint, supportToken, "does.not.exist", nil)
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected a JSON-RPC error for an unknown tool, got %v", resp)
	}
	if code, _ := errObj["code"].(float64); code != -32602 {
		t.Fatalf("error code = %v, want -32602 (per the MCP spec's own example)", errObj["code"])
	}
}

func TestTaintAllowsTrustedProvenanceAndDeniesUntrusted(t *testing.T) {
	endpoint, _ := newTestGateway(t)

	// finance-bot fetches the account number from the trusted supplier
	// master record...
	getAccount := callTool(t, endpoint, financeToken, "supplier.get_account", map[string]any{"supplier_id": "S1"})
	getResult := resultOf(t, getAccount)
	structured, _ := getResult["structuredContent"].(map[string]any)
	trustedAccount, _ := structured["account_number"].(string)
	if trustedAccount == "" {
		t.Fatalf("supplier.get_account did not return an account_number: %v", getResult)
	}

	// ...and reusing that exact value in a wire transfer is allowed.
	transfer := callTool(t, endpoint, financeToken, "payments.wire_transfer", map[string]any{
		"account_number": trustedAccount, "amount_usd": 100,
	})
	transferResult := resultOf(t, transfer)
	if isErr, _ := transferResult["isError"].(bool); isErr {
		t.Fatalf("wire transfer with a trusted account number was denied: %v", transferResult)
	}

	// But an account number sourced from the untrusted email upstream is
	// denied, even though finance-bot is in scope for both tools.
	readEmail := callTool(t, endpoint, financeToken, "email.read_latest", nil)
	emailResult := resultOf(t, readEmail)
	emailStructured, _ := emailResult["structuredContent"].(map[string]any)
	phishingAccount, _ := emailStructured["account_number"].(string)
	if phishingAccount == "" {
		t.Fatalf("email.read_latest did not return an account_number: %v", emailResult)
	}

	badTransfer := callTool(t, endpoint, financeToken, "payments.wire_transfer", map[string]any{
		"account_number": phishingAccount, "amount_usd": 100,
	})
	badResult := resultOf(t, badTransfer)
	if isErr, _ := badResult["isError"].(bool); !isErr {
		t.Fatalf("wire transfer with an email-sourced account number was allowed: %v", badResult)
	}
}

func TestAggregateLimitDeniesAfterThreshold(t *testing.T) {
	endpoint, _ := newTestGateway(t)

	// examples/policies/limits.rego caps kb.search at 3 calls per window.
	for i := 0; i < 3; i++ {
		resp := callTool(t, endpoint, supportToken, "kb.search", map[string]any{"query": "test"})
		result := resultOf(t, resp)
		if isErr, _ := result["isError"].(bool); isErr {
			t.Fatalf("call %d: expected allow within the limit, got denied: %v", i+1, result)
		}
	}

	resp := callTool(t, endpoint, supportToken, "kb.search", map[string]any{"query": "test"})
	result := resultOf(t, resp)
	if isErr, _ := result["isError"].(bool); !isErr {
		t.Fatal("4th call within the window: expected the rate limit to deny it, got allow")
	}
}

func TestUnresolvedCredentialIsDenied(t *testing.T) {
	endpoint, _ := newTestGateway(t)

	resp := callTool(t, endpoint, "not-a-real-token", "kb.search", map[string]any{"query": "test"})
	result := resultOf(t, resp)
	if isErr, _ := result["isError"].(bool); !isErr {
		t.Fatalf("expected an unrecognized credential to be denied, got %v", result)
	}
}

func TestAuditChainIsIntactAfterATypicalSession(t *testing.T) {
	endpoint, auditStore := newTestGateway(t)

	callTool(t, endpoint, supportToken, "kb.search", map[string]any{"query": "test"})
	callTool(t, endpoint, supportToken, "payments.wire_transfer", nil) // denied
	callTool(t, endpoint, "bad-token", "kb.search", nil)               // denied, unresolved identity
	callTool(t, endpoint, financeToken, "email.read_latest", nil)

	if err := auditStore.Verify(context.Background()); err != nil {
		t.Fatalf("Verify: %v", err)
	}
}
