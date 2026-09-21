// Copyright (c) 2026 Overarching AI LLC
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/huyba/helmdeep/pkg/types"
)

type nopHandler struct{}

func (nopHandler) ListTools(context.Context, CallContext, string) (ListToolsResult, error) {
	return ListToolsResult{}, nil
}
func (nopHandler) CallTool(context.Context, CallContext, string, map[string]any) (types.ToolResult, error) {
	return types.ToolResult{}, nil
}

// A mounted route must be reachable with a plain GET and none of the MCP
// headers, while the MCP endpoint itself keeps enforcing its own rules — a
// health probe is not an MCP client and must not need to pretend to be one.
func TestServer_MountServesNonMCPRouteWithoutMCPHeaders(t *testing.T) {
	s := NewServer(":0", "/mcp", nopHandler{}, "test")
	s.Mount("/probe", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("mounted"))
	}))
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/probe") //nolint:noctx // test against a local httptest server
	if err != nil {
		t.Fatalf("GET /probe: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || string(body) != "mounted" {
		t.Fatalf("GET /probe = %d %q, want 200 \"mounted\"", resp.StatusCode, body)
	}

	mcpResp, err := http.Get(ts.URL + "/mcp") //nolint:noctx // test against a local httptest server
	if err != nil {
		t.Fatalf("GET /mcp: %v", err)
	}
	defer func() { _ = mcpResp.Body.Close() }()
	if mcpResp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("GET /mcp = %d, want 405 (Mount must not disturb the MCP endpoint's rules)", mcpResp.StatusCode)
	}
}
