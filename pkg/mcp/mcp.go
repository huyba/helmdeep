// Copyright (c) 2026 Overarching AI LLC
// SPDX-License-Identifier: Apache-2.0

// Package mcp implements the gateway's Model Context Protocol surface: how
// it presents itself to agents as a single MCP server, and how it talks to
// the real upstream MCP servers it forwards allowed calls to.
//
// This package targets the current, stateless MCP spec (2026-07-28)
// exclusively — see docs/adr/0005-mcp-protocol-compatibility.md for why a
// legacy session-based compatibility shim was rejected. It owns
// wire-protocol correctness (JSON-RPC framing, `_meta`, header validation,
// error codes) and nothing about policy or identity; see Handler for the
// boundary with internal/gateway, which owns those.
package mcp

import (
	"context"

	"github.com/huyba/helmdeep/pkg/types"
)

// ProtocolVersion is an MCP spec revision date, as sent in requests/responses.
type ProtocolVersion string

// ProtocolVersionCurrent is the only protocol version this gateway speaks.
// See docs/adr/0005-mcp-protocol-compatibility.md.
const ProtocolVersionCurrent ProtocolVersion = "2026-07-28"

// Listener accepts connections from agents and presents the gateway as a
// single MCP server, regardless of how many upstream servers sit behind it.
// The Step 2 implementation (Server, in server.go) speaks Streamable HTTP
// only; stdio is not implemented (see ARCHITECTURE.md).
type Listener interface {
	Serve(ctx context.Context) error
	Shutdown(ctx context.Context) error
}

// Upstream is a real MCP server the gateway forwards allowed calls to, using
// the gateway's own credentials rather than anything the agent holds.
type Upstream interface {
	Name() string
	ListTools(ctx context.Context) ([]types.Tool, error)
	CallTool(ctx context.Context, call types.ToolCall) (types.ToolResult, error)
}

// Registry resolves which configured Upstream owns a given tool name, so the
// gateway can present many upstream MCP servers behind one endpoint.
type Registry interface {
	Upstreams() []Upstream
	Resolve(toolName string) (Upstream, bool)
	// Tools returns every tool known across all upstreams, unfiltered by
	// policy — the gateway is responsible for filtering this down to what
	// a given caller may see before it reaches tools/list.
	Tools() []types.Tool
}
