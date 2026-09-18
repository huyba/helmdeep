// Copyright (c) 2026 Overarching AI LLC
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"

	"github.com/huyba/helmdeep/pkg/types"
)

// CallContext is everything the transport (Server) knows about a request
// that Handler needs but that isn't part of the MCP method's own
// parameters: who's asking (as a raw, unresolved credential — Handler is
// responsible for resolving it to a types.Subject) and which protocol
// request this is.
//
// There is deliberately no session ID or connection identifier here. Per
// docs/adr/0005-mcp-protocol-compatibility.md, caller identity is resolved
// fresh from CallContext.Credential on every single call; nothing in this
// struct is retained across requests by the transport.
type CallContext struct {
	// Credential is the raw bearer token from the request's Authorization
	// header, or "" if none was presented. Handler resolves this to a
	// types.Subject; Server never interprets it.
	Credential string
	// ClientInfo is self-reported by the client (io.modelcontextprotocol/clientInfo)
	// and, per spec, MUST NOT be used for security decisions.
	ClientInfo *Implementation
	// ProtocolVersion is always ProtocolVersionCurrent by the time Handler
	// sees a request — Server rejects anything else before calling Handler.
	ProtocolVersion string
	// RequestID is generated fresh by Server for every HTTP request (it is
	// not the JSON-RPC request id, which is caller-supplied and not
	// trustworthy as a uniqueness guarantee). Handler uses it to populate
	// types.DecisionContext.RequestID for policy evaluation and the audit
	// trail.
	RequestID string
}

// ListToolsResult is Handler's answer to a tools/list call: the tools this
// specific caller is allowed to see, already filtered — an agent should not
// learn about a tool its policy forbids it from calling, not just be
// blocked from calling it.
type ListToolsResult struct {
	Tools      []types.Tool
	NextCursor string
}

// ProtocolError lets Handler signal that a tools/call request is invalid at
// the protocol level (the spec's example: an unknown tool name) rather than
// one that reached a tool and failed there. Server maps this to a JSON-RPC
// error response with Code. It must never be used for a policy denial or an
// upstream failure — those are tool execution errors, reported through
// types.ToolResult.IsError, because a denial is something the calling model
// can potentially react to and a JSON-RPC protocol error is not meant to
// be.
type ProtocolError struct {
	Code    int
	Message string
}

func (e *ProtocolError) Error() string { return e.Message }

// Handler answers MCP requests once Server has already validated headers,
// `_meta`, and the protocol version — the business logic side of the
// gateway, implemented by internal/gateway. Handler knows nothing about
// JSON-RPC framing or HTTP; Server knows nothing about policy or identity.
type Handler interface {
	ListTools(ctx context.Context, cc CallContext, cursor string) (ListToolsResult, error)
	CallTool(ctx context.Context, cc CallContext, name string, arguments map[string]any) (types.ToolResult, error)
}
