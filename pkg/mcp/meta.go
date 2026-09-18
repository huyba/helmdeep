// Copyright (c) 2026 Overarching AI LLC
// SPDX-License-Identifier: Apache-2.0

package mcp

import "encoding/json"

// Implementation identifies a client or server by name and version. Per
// spec it is self-reported and MUST NOT be used for security decisions —
// this gateway only ever uses it for logging/display.
type Implementation struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// requestMeta is the subset of a request's `_meta` object this gateway
// reads, using the reserved `io.modelcontextprotocol/*` keys defined by the
// 2026-07-28 spec. ClientCapabilities is kept raw: the gateway doesn't
// currently require any client capability, so it only needs to confirm the
// field is present (required per spec), not parse its contents.
type requestMeta struct {
	ProtocolVersion    string          `json:"io.modelcontextprotocol/protocolVersion"`
	ClientInfo         *Implementation `json:"io.modelcontextprotocol/clientInfo,omitempty"`
	ClientCapabilities json.RawMessage `json:"io.modelcontextprotocol/clientCapabilities"`
}

// paramsMeta unwraps just the `_meta` field from an otherwise
// method-specific params object.
type paramsMeta struct {
	Meta *requestMeta `json:"_meta"`
}

// parseMeta extracts and validates the required `_meta` fields from a raw
// params object. A missing or malformed required field is reported the way
// the spec requires it to be: JSON-RPC code -32602 (Invalid params).
func parseMeta(params json.RawMessage) (requestMeta, *rpcError) {
	var pm paramsMeta
	if len(params) > 0 {
		if err := json.Unmarshal(params, &pm); err != nil {
			return requestMeta{}, &rpcError{Code: codeInvalidParams, Message: "params is not a JSON object: " + err.Error()}
		}
	}
	if pm.Meta == nil {
		return requestMeta{}, &rpcError{Code: codeInvalidParams, Message: "params._meta is required"}
	}
	if pm.Meta.ProtocolVersion == "" {
		return requestMeta{}, &rpcError{Code: codeInvalidParams, Message: "params._meta[\"io.modelcontextprotocol/protocolVersion\"] is required"}
	}
	if len(pm.Meta.ClientCapabilities) == 0 {
		return requestMeta{}, &rpcError{Code: codeInvalidParams, Message: "params._meta[\"io.modelcontextprotocol/clientCapabilities\"] is required"}
	}
	return *pm.Meta, nil
}

// serverInfoMeta builds the `_meta` object this gateway attaches to every
// result, identifying itself per spec recommendation.
func serverInfoMeta(version string) map[string]any {
	return map[string]any{
		"io.modelcontextprotocol/serverInfo": Implementation{
			Name:    "helmdeep-gateway",
			Version: version,
		},
	}
}
