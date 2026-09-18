// Copyright (c) 2026 Overarching AI LLC
// SPDX-License-Identifier: Apache-2.0

package types

// Tool describes a callable tool as exposed by an upstream MCP server.
type Tool struct {
	Name        string
	Description string
	InputSchema map[string]any
	Upstream    string // name of the upstream MCP server that owns this tool
}

// ToolCall is a resolved, about-to-be-decided invocation of a tool: the
// gateway has already parsed the incoming tools/call request and attached
// provenance to each argument by the time this exists.
type ToolCall struct {
	Tool      string
	Arguments map[string]Value
}

// ToolResult is what the upstream MCP server returned for a ToolCall,
// before any response-side obligations (e.g. redaction) are applied.
type ToolResult struct {
	Content []byte // opaque MCP result payload (content blocks / structuredContent)
	IsError bool
}
