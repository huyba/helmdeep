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
