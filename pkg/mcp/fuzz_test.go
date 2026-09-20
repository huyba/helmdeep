// Copyright (c) 2026 Overarching AI LLC
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/huyba/helmdeep/pkg/types"
)

// noopHandler answers every request trivially — this fuzz target is about
// the transport (server.go's parsing and header/`_meta` validation), not
// about anything Handler implementations do.
type noopHandler struct{}

func (noopHandler) ListTools(context.Context, CallContext, string) (ListToolsResult, error) {
	return ListToolsResult{}, nil
}

func (noopHandler) CallTool(context.Context, CallContext, string, map[string]any) (types.ToolResult, error) {
	return types.ToolResult{}, nil
}

// FuzzServeHTTP throws arbitrary bytes at the gateway's MCP endpoint as
// the POST body, with a fixed set of otherwise-valid headers. The only
// property under test is that malformed, truncated, or adversarially
// shaped JSON-RPC input — this is the one surface any unauthenticated
// network attacker can reach before identity or policy ever get involved
// — cannot panic the process. A panic here would be a denial-of-service
// path that bypasses every other control in this repo, since it happens
// before enforcement.
func FuzzServeHTTP(f *testing.F) {
	f.Add([]byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}`))
	f.Add([]byte(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"x","arguments":{"a":1},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}`))
	f.Add([]byte(`{"jsonrpc":"2.0","id":3,"method":"server/discover","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}`))
	f.Add([]byte(`{}`))
	f.Add([]byte(`not json at all`))
	f.Add([]byte(`{"jsonrpc":"2.0","method":"tools/list"}`)) // notification-shaped (no id)
	f.Add([]byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"_meta":{}}}`))
	f.Add([]byte(``))
	f.Add([]byte(`null`))
	f.Add([]byte(`{"params": {"_meta": {"io.modelcontextprotocol/protocolVersion": 12345}}}`))

	srv := NewServer(":0", "/mcp", noopHandler{}, "fuzz")

	f.Fuzz(func(t *testing.T, body []byte) {
		req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		req.Header.Set("MCP-Protocol-Version", "2026-07-28")
		req.Header.Set("Mcp-Method", "tools/list")
		req.Header.Set("Mcp-Name", "x")

		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req) // must not panic
	})
}
