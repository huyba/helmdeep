// Copyright (c) 2026 Overarching AI LLC
// SPDX-License-Identifier: Apache-2.0

// Package mcp defines the gateway's Model Context Protocol surface: how it
// presents itself to agents as a single MCP server, and how it talks to the
// real upstream MCP servers it forwards allowed calls to.
//
// STATUS: interface only for Step 1. Wire-level implementation lands in
// Step 2, targeting the current stateless MCP spec (2026-07-28) with a
// compatibility shim for the prior session-based protocol (2025-11-25 and
// earlier) that most agent clients still speak. See
// docs/adr/0005-mcp-protocol-compatibility.md and ROADMAP.md.
package mcp

import (
	"context"

	"github.com/huyba/helmdeep/pkg/types"
)

// ProtocolVersion is an MCP spec revision date, as sent in requests/responses.
type ProtocolVersion string

const (
	// ProtocolVersionCurrent is the stateless spec revision the gateway
	// targets as primary.
	ProtocolVersionCurrent ProtocolVersion = "2026-07-28"
	// ProtocolVersionLegacy is the last session-based revision the gateway's
	// compatibility shim supports.
	ProtocolVersionLegacy ProtocolVersion = "2025-11-25"
)

// Listener accepts connections from agents and presents the gateway as a
// single MCP server, regardless of how many upstream servers sit behind it.
// A concrete implementation will speak Streamable HTTP and stdio, per
// transport requirements in the spec.
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
}
