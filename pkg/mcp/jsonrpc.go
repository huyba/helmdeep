// Copyright (c) 2026 Overarching AI LLC
// SPDX-License-Identifier: Apache-2.0

package mcp

import "encoding/json"

// rpcRequest is the wire shape of a client-sent JSON-RPC request or
// notification (a notification simply omits ID). Params is left raw because
// its shape depends on Method.
type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

func (r rpcRequest) isNotification() bool { return len(r.ID) == 0 }

// rpcResultResponse is the wire shape of a successful JSON-RPC response.
// Result is a concrete result type (e.g. discoverResult, listToolsResult)
// marshaled by the caller; ResultType always accompanies it per spec.
type rpcResultResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result"`
}

// rpcErrorResponse is the wire shape of a failed JSON-RPC response.
type rpcErrorResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Error   rpcError        `json:"error"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// JSON-RPC 2.0 base error codes, used for transport/framing failures that
// predate any MCP-specific handling.
const (
	codeParseError     = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
	codeInternalError  = -32603
)

// MCP-specific error codes reserved by the 2026-07-28 spec's error code
// allocation policy (-32020 to -32099). See
// https://modelcontextprotocol.io/specification/2026-07-28/basic#error-codes.
const (
	codeHeaderMismatch                  = -32020
	codeMissingRequiredClientCapability = -32021
	codeUnsupportedProtocolVersion      = -32022
)

type unsupportedProtocolVersionData struct {
	Supported []string `json:"supported"`
	Requested string   `json:"requested"`
}
