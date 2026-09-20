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
	"os"
	"path/filepath"
	"strings"
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

// startMockUpstream returns both the httptest server (for the gateway to
// call) and the underlying *mockupstream.Server (so a test can inspect
// what it actually received — see mockupstream.RecordedCall).
func startMockUpstream(t testing.TB, profile string) (*httptest.Server, *mockupstream.Server) {
	t.Helper()
	h, err := mockupstream.NewHandler(profile)
	if err != nil {
		t.Fatalf("mockupstream.NewHandler(%q): %v", profile, err)
	}
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)
	return ts, h
}

// mockUpstreams holds a spy on every upstream in the standard test
// topology, so tests can assert on what each one did or didn't receive.
type mockUpstreams struct {
	kb, supplier, email, payments *mockupstream.Server
}

// newTestGateway wires the same components a real deployment would — see
// examples/quickstart/config.yaml, which this mirrors — and returns the
// gateway's HTTP endpoint, its audit store (so a test can call Verify
// directly instead of shelling out to `verify-chain`), and a spy on each
// mock upstream.
func newTestGateway(t testing.TB) (endpoint string, auditStore *audit.FileStore, mocks mockUpstreams) {
	t.Helper()
	return newTestGatewayWithPolicy(t, mustAbs(t, "../../examples/policies"))
}

// newTestGatewayWithPolicy is newTestGateway with the policy bundle path as
// a parameter, so a test that needs behavior the public example bundle
// doesn't demonstrate (e.g. obligations/redaction) can point at its own
// fixture under test/e2e/testdata/ instead.
func newTestGatewayWithPolicy(t testing.TB, policyPath string) (endpoint string, auditStore *audit.FileStore, mocks mockUpstreams) {
	t.Helper()

	kbTS, kb := startMockUpstream(t, "knowledgebase")
	supplierTS, supplier := startMockUpstream(t, "suppliermaster")
	emailTS, email := startMockUpstream(t, "email")
	paymentsTS, payments := startMockUpstream(t, "payments")
	mocks = mockUpstreams{kb: kb, supplier: supplier, email: email, payments: payments}

	upstreams := []mcp.Upstream{
		mcp.NewHTTPUpstream("knowledgebase", kbTS.URL, ""),
		mcp.NewHTTPUpstream("suppliermaster", supplierTS.URL, ""),
		mcp.NewHTTPUpstream("email", emailTS.URL, ""),
		mcp.NewHTTPUpstream("payments", paymentsTS.URL, ""),
	}
	registry, err := mcp.NewStaticRegistry(context.Background(), upstreams)
	if err != nil {
		t.Fatalf("NewStaticRegistry: %v", err)
	}

	pdp := policy.NewOPADecider()
	if err := pdp.Load(policyPath); err != nil {
		t.Fatalf("Load policy %s: %v", policyPath, err)
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

	gw := gateway.New(resolver, pdp, auditStore, registry, provenance, 0, nil)
	gwServer := mcp.NewServer(":0", "/mcp", gw, "e2e-test")

	ts := httptest.NewServer(gwServer.Handler())
	t.Cleanup(ts.Close)

	return ts.URL + "/mcp", auditStore, mocks
}

func mustAbs(t testing.TB, path string) string {
	t.Helper()
	abs, err := filepath.Abs(path)
	if err != nil {
		t.Fatalf("resolve path %s: %v", path, err)
	}
	return abs
}

// call sends one MCP request exactly as a conforming 2026-07-28 client
// would — real headers, real `_meta` — and returns the decoded top-level
// JSON-RPC response.
func call(t testing.TB, endpoint, token, method, name string, params map[string]any) map[string]any {
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

func callTool(t testing.TB, endpoint, token, tool string, arguments map[string]any) map[string]any {
	t.Helper()
	return call(t, endpoint, token, "tools/call", tool, map[string]any{"name": tool, "arguments": arguments})
}

func resultOf(t testing.TB, resp map[string]any) map[string]any {
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
	endpoint, _, _ := newTestGateway(t)

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
	endpoint, _, mocks := newTestGateway(t)

	resp := callTool(t, endpoint, supportToken, "payments.wire_transfer", map[string]any{
		"account_number": "ACC-TRUSTED-0001", "amount_usd": 100,
	})
	result := resultOf(t, resp)
	if isErr, _ := result["isError"].(bool); !isErr {
		t.Fatalf("expected isError=true for an out-of-scope tool call, got %v", result)
	}

	// The strong form of "denied": not just that the agent got an error,
	// but that the payments upstream was never asked to execute the tool.
	// (NewStaticRegistry does call tools/list on every upstream once at
	// startup to build its catalog — that's routine and not what this
	// checks; toolCalls filters down to actual tools/call attempts.)
	if calls := toolCalls(mocks.payments.Calls()); len(calls) != 0 {
		t.Fatalf("payments upstream received %d tools/call(s) for a denied request, want 0: %+v", len(calls), calls)
	}
}

// toolCalls filters a mock upstream's recorded calls down to actual
// tools/call attempts, excluding the routine tools/list every upstream
// receives once at registry startup.
func toolCalls(calls []mockupstream.RecordedCall) []mockupstream.RecordedCall {
	var out []mockupstream.RecordedCall
	for _, c := range calls {
		if c.Method == "tools/call" {
			out = append(out, c)
		}
	}
	return out
}

func TestUnknownToolIsAProtocolError(t *testing.T) {
	endpoint, _, _ := newTestGateway(t)

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
	endpoint, _, _ := newTestGateway(t)

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
	endpoint, _, _ := newTestGateway(t)

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
	endpoint, _, _ := newTestGateway(t)

	resp := callTool(t, endpoint, "not-a-real-token", "kb.search", map[string]any{"query": "test"})
	result := resultOf(t, resp)
	if isErr, _ := result["isError"].(bool); !isErr {
		t.Fatalf("expected an unrecognized credential to be denied, got %v", result)
	}
}

func TestAuditChainIsIntactAfterATypicalSession(t *testing.T) {
	endpoint, auditStore, _ := newTestGateway(t)

	callTool(t, endpoint, supportToken, "kb.search", map[string]any{"query": "test"})
	callTool(t, endpoint, supportToken, "payments.wire_transfer", nil) // denied
	callTool(t, endpoint, "bad-token", "kb.search", nil)               // denied, unresolved identity
	callTool(t, endpoint, financeToken, "email.read_latest", nil)

	if err := auditStore.Verify(context.Background()); err != nil {
		t.Fatalf("Verify: %v", err)
	}
}

// TestUpstreamCredentialNeverLeaksAgentToken asserts the gateway's core
// promise directly against what the upstream actually received, not just
// against documented intent: the agent's own bearer token must never
// appear in the request the gateway forwards. Both configured upstreams in
// the standard test topology use an empty gateway-held token (see
// newTestGatewayWithPolicy), so the upstream should see no Authorization
// header at all — and, whatever it saw, it must not be the agent's.
func TestUpstreamCredentialNeverLeaksAgentToken(t *testing.T) {
	endpoint, _, mocks := newTestGateway(t)

	callTool(t, endpoint, supportToken, "kb.search", map[string]any{"query": "test"})

	calls := toolCalls(mocks.kb.Calls())
	if len(calls) != 1 {
		t.Fatalf("kb upstream received %d tools/call(s), want 1", len(calls))
	}
	if got := calls[0].AuthHeader; got == "Bearer "+supportToken {
		t.Fatalf("upstream received the agent's own token: %q", got)
	}
}

// TestMultipleUpstreamsRouteCorrectly asserts routing isolation directly:
// a call to a tool on one upstream must not be visible to any other
// upstream behind the same gateway endpoint.
func TestMultipleUpstreamsRouteCorrectly(t *testing.T) {
	endpoint, _, mocks := newTestGateway(t)

	callTool(t, endpoint, supportToken, "kb.search", map[string]any{"query": "test"})
	callTool(t, endpoint, financeToken, "supplier.get_account", map[string]any{"supplier_id": "S1"})

	if got := len(toolCalls(mocks.kb.Calls())); got != 1 {
		t.Fatalf("kb upstream received %d tools/call(s), want 1", got)
	}
	if got := len(toolCalls(mocks.supplier.Calls())); got != 1 {
		t.Fatalf("supplier upstream received %d tools/call(s), want 1", got)
	}
	if got := len(toolCalls(mocks.email.Calls())); got != 0 {
		t.Fatalf("email upstream received %d call(s), want 0 — it was never called", got)
	}
	if got := len(toolCalls(mocks.payments.Calls())); got != 0 {
		t.Fatalf("payments upstream received %d tools/call(s), want 0 — it was never called", got)
	}
}

// TestObligationsRedactField exercises allow_with_obligations end to end,
// using the test-only fixture at testdata/obligations-policy/ — the public
// example bundle deliberately doesn't demonstrate this (see
// docs/policy-guide.md), so this test brings its own policy.
func TestObligationsRedactField(t *testing.T) {
	endpoint, _, _ := newTestGatewayWithPolicy(t, mustAbs(t, "testdata/obligations-policy"))

	resp := callTool(t, endpoint, financeToken, "supplier.get_account", map[string]any{"supplier_id": "S1"})
	result := resultOf(t, resp)
	if isErr, _ := result["isError"].(bool); isErr {
		t.Fatalf("expected allow_with_obligations to succeed, got isError: %v", result)
	}

	structured, ok := result["structuredContent"].(map[string]any)
	if !ok {
		t.Fatalf("no structuredContent in result: %v", result)
	}
	if got := structured["account_number"]; got != "[REDACTED]" {
		t.Fatalf("structuredContent.account_number = %v, want the field redacted", got)
	}
	// The obligation targets only this one field — everything else in the
	// same object must pass through untouched.
	if got := structured["supplier_id"]; got != "S1" {
		t.Fatalf("structuredContent.supplier_id = %v, want it untouched by the redaction obligation", got)
	}
}

// TestUpstreamUnreachableIsAToolExecutionErrorAndRecordsFailure covers
// docs/adr/0006-operational-failure-classification.md, Decision 1, over
// real HTTP: an upstream that was reachable when the registry built its
// tool index, then goes down before the call, must produce a clean
// isError:true result — not a JSON-RPC protocol error, not a hang — and
// the audit log must still contain a record of the attempt.
func TestUpstreamUnreachableIsAToolExecutionErrorAndRecordsFailure(t *testing.T) {
	paymentsTS, _ := startMockUpstream(t, "payments")

	upstreams := []mcp.Upstream{mcp.NewHTTPUpstream("payments", paymentsTS.URL, "")}
	registry, err := mcp.NewStaticRegistry(context.Background(), upstreams)
	if err != nil {
		t.Fatalf("NewStaticRegistry: %v", err)
	}

	allowEverythingPolicy := t.TempDir()
	writeFile(t, allowEverythingPolicy+"/policy.rego", `package helmdeep.authz

decision := {"outcome": "allow", "policy_id": "test.allow"}
`)
	pdp := policy.NewOPADecider()
	if err := pdp.Load(allowEverythingPolicy); err != nil {
		t.Fatalf("Load: %v", err)
	}

	auditPath := filepath.Join(t.TempDir(), "audit.log")
	auditStore, err := audit.NewFileStore(auditPath)
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}

	const token = "test-token"
	resolver := gateway.NewStaticTokenResolver(map[string]gateway.StaticIdentity{
		token: {ID: "agent:test", Kind: types.SubjectKindAgent},
	})
	gw := gateway.New(resolver, pdp, auditStore, registry, nil, 0, nil)
	gwServer := mcp.NewServer(":0", "/mcp", gw, "e2e-test")
	ts := httptest.NewServer(gwServer.Handler())
	t.Cleanup(ts.Close)

	// The registry already has payments.wire_transfer indexed; now take
	// the upstream down before the actual call.
	paymentsTS.Close()

	resp := callTool(t, ts.URL+"/mcp", token, "payments.wire_transfer", map[string]any{"account_number": "x", "amount_usd": 1})
	result := resultOf(t, resp)
	if isErr, _ := result["isError"].(bool); !isErr {
		t.Fatalf("expected isError:true for an unreachable upstream, got %v", result)
	}

	records := readAuditRecords(t, auditPath)
	found := false
	for _, rec := range records {
		if rec.Outcome == types.RecordOutcomeFailed {
			found = true
		}
	}
	if !found {
		t.Fatalf("no RecordOutcomeFailed record found among %d records", len(records))
	}
}

func writeFile(t testing.TB, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func readAuditRecords(t testing.TB, path string) []types.ActionRecord {
	t.Helper()
	data, err := os.ReadFile(path) // #nosec G304 -- test's own t.TempDir() fixture
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var records []types.ActionRecord
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var rec types.ActionRecord
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("unmarshal audit record: %v", err)
		}
		records = append(records, rec)
	}
	return records
}
