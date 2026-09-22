// Copyright (c) 2026 Overarching AI LLC
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/huyba/helmdeep/pkg/types"
)

// HTTPUpstream is an Upstream that speaks Streamable HTTP (2026-07-28) to a
// real MCP server, using a credential the gateway holds — never the calling
// agent's. See docs/adr/0005-mcp-protocol-compatibility.md: the gateway
// targets the current spec on both its agent-facing and upstream-facing
// sides.
//
// HTTPUpstream expects the upstream to answer with
// Content-Type: application/json (a single JSON object). An upstream that
// responds with an SSE stream instead is not supported in Step 2 — most
// tool calls are quick request/response and don't need one, and adding an
// SSE client for the cases that might is deferred rather than built on
// spec (see ARCHITECTURE.md).
type HTTPUpstream struct {
	name        string
	baseURL     string
	bearerToken string
	httpClient  *http.Client
}

// NewHTTPUpstream builds an Upstream for a single upstream MCP server.
// bearerToken is the gateway's own credential for this upstream (from
// config, e.g. sourced from an env var) and may be empty if the upstream
// requires none.
func NewHTTPUpstream(name, baseURL, bearerToken string) *HTTPUpstream {
	return &HTTPUpstream{
		name:        name,
		baseURL:     baseURL,
		bearerToken: bearerToken,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
			// Doc 05-tool-gateway.md §3's egress default-deny: the transport
			// itself refuses any connection to a link-local address, cloud
			// metadata's reserved space — see egress.go's blockedDial.
			Transport: &http.Transport{
				DialContext: blockedDial((&net.Dialer{Timeout: 10 * time.Second}).DialContext),
			},
		},
	}
}

func (u *HTTPUpstream) Name() string { return u.name }

func (u *HTTPUpstream) ListTools(ctx context.Context) ([]types.Tool, error) {
	var listResult struct {
		Tools []types.Tool `json:"tools"`
	}
	if err := u.call(ctx, "tools/list", "", "", map[string]any{}, &listResult); err != nil {
		return nil, err
	}
	for i := range listResult.Tools {
		listResult.Tools[i].Upstream = u.name
	}
	return listResult.Tools, nil
}

func (u *HTTPUpstream) CallTool(ctx context.Context, tc types.ToolCall) (types.ToolResult, error) {
	args := make(map[string]any, len(tc.Arguments))
	for k, v := range tc.Arguments {
		args[k] = v.Data
	}
	params := map[string]any{
		"name":      tc.Tool,
		"arguments": args,
	}

	var result types.ToolResult
	if err := u.call(ctx, "tools/call", tc.Tool, tc.Credential, params, &result); err != nil {
		return types.ToolResult{}, err
	}
	return result, nil
}

// call sends one JSON-RPC request to the upstream and decodes its `result`
// into out. name is the Mcp-Name header value for tools/call and empty for
// methods that don't carry one (e.g. tools/list). credentialOverride, if
// non-empty, is presented instead of the upstream's own configured static
// bearerToken — see types.ToolCall.Credential's doc comment.
func (u *HTTPUpstream) call(ctx context.Context, method, name, credentialOverride string, params map[string]any, out any) error {
	params["_meta"] = map[string]any{
		"io.modelcontextprotocol/protocolVersion":    string(ProtocolVersionCurrent),
		"io.modelcontextprotocol/clientCapabilities": map[string]any{},
		"io.modelcontextprotocol/clientInfo": Implementation{
			Name:    "helmdeep-gateway",
			Version: "internal",
		},
	}

	body, err := json.Marshal(rpcRequest{
		JSONRPC: "2.0",
		ID:      json.RawMessage(fmt.Sprintf("%q", newRequestID())),
		Method:  method,
		Params:  mustMarshal(params),
	})
	if err != nil {
		return fmt.Errorf("marshal request to upstream %s: %w", u.name, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.baseURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request to upstream %s: %w", u.name, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("MCP-Protocol-Version", string(ProtocolVersionCurrent))
	req.Header.Set("Mcp-Method", method)
	if name != "" {
		req.Header.Set("Mcp-Name", name)
	}
	if token := credentialOverride; token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	} else if u.bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+u.bearerToken)
	}

	resp, err := u.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("call upstream %s: %w", u.name, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if ct := resp.Header.Get("Content-Type"); ct != "" && ct != "application/json" && !hasPrefix(ct, "application/json;") {
		return fmt.Errorf("upstream %s responded with unsupported Content-Type %q (only application/json is supported)", u.name, ct)
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response from upstream %s: %w", u.name, err)
	}

	var envelope struct {
		Result json.RawMessage `json:"result"`
		Error  *rpcError       `json:"error"`
	}
	if err := json.Unmarshal(respBody, &envelope); err != nil {
		return fmt.Errorf("upstream %s returned malformed JSON-RPC (HTTP %d): %w", u.name, resp.StatusCode, err)
	}
	if envelope.Error != nil {
		return fmt.Errorf("upstream %s returned error %d: %s", u.name, envelope.Error.Code, envelope.Error.Message)
	}
	if out != nil {
		if err := json.Unmarshal(envelope.Result, out); err != nil {
			return fmt.Errorf("upstream %s result did not match expected shape: %w", u.name, err)
		}
	}
	return nil
}

func mustMarshal(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		// v is always a map[string]any built by this package from
		// JSON-safe values; a marshal failure here means a caller passed
		// something unsupported (e.g. a channel) as a tool argument, which
		// is a programming error, not a runtime condition to recover from.
		panic("mcp: could not marshal request params: " + err.Error())
	}
	return b
}

func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
