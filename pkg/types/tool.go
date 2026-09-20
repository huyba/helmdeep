// Copyright (c) 2026 Overarching AI LLC
// SPDX-License-Identifier: Apache-2.0

package types

import "encoding/json"

// Tool describes a callable tool as exposed by an upstream MCP server.
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"inputSchema"`
	Upstream    string          `json:"-"` // which upstream MCP server owns this tool; gateway-internal, never serialized to the agent
}

// ToolCall is a resolved, about-to-be-decided invocation of a tool: the
// gateway has already parsed the incoming tools/call request and attached
// provenance to each argument by the time this exists.
type ToolCall struct {
	Tool      string
	Arguments map[string]Value
	// Credential, if non-empty, is a per-call token the Credential Broker
	// minted for this specific upstream+tool (docs/04-identity-authz.md
	// §3) — an mcp.Upstream implementation should present this instead of
	// whatever static credential it might otherwise be configured with.
	// Empty means no broker is configured; the upstream falls back to its
	// own static configuration, which is the pre-Milestone-M1 behavior and
	// remains valid for upstreams that don't need per-call credentials.
	Credential string
}

// ToolResult is what the upstream MCP server returned for a ToolCall,
// before any response-side obligations (e.g. redaction) are applied. The
// gateway treats Content and StructuredContent as opaque JSON and forwards
// them mostly unmodified — it does not need to understand every MCP content
// block type to proxy one.
type ToolResult struct {
	Content           json.RawMessage `json:"content"`
	StructuredContent json.RawMessage `json:"structuredContent,omitempty"`
	IsError           bool            `json:"isError"`
}
