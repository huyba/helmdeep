// Copyright (c) 2026 Overarching AI LLC
// SPDX-License-Identifier: Apache-2.0

// Package mockupstream is a minimal, hardcoded MCP server used by the e2e
// test and the quickstart's docker-compose setup to stand in for real
// upstream systems (a supplier master record, an email inbox, a payments
// API, a knowledge base) without needing real credentials or real systems.
// It speaks just enough of the 2026-07-28 Streamable HTTP transport to be a
// convincing upstream: tools/list and tools/call, JSON responses only, no
// header validation of its own (it trusts whatever the gateway sends it,
// because the gateway — not this mock — is what this project tests).
//
// This is fixture code, not a reference MCP server implementation.
package mockupstream

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
)

// ToolDef is one tool this mock exposes: its MCP-visible schema, and a
// handler that computes a result from call arguments.
type ToolDef struct {
	Name        string
	Description string
	Handler     func(arguments map[string]any) (structuredContent map[string]any, isError bool, errText string)
}

// Profiles are the fixed upstream personalities the quickstart and e2e test
// use, matching the worked example in docs/policy-guide.md and
// examples/policies/.
var Profiles = map[string][]ToolDef{
	"knowledgebase": {
		{
			Name:        "kb.search",
			Description: "Search the internal knowledge base for an answer.",
			Handler: func(arguments map[string]any) (map[string]any, bool, string) {
				query, _ := arguments["query"].(string)
				return map[string]any{
					"answer": fmt.Sprintf("Mock knowledge-base answer for: %s", query),
				}, false, ""
			},
		},
	},
	"suppliermaster": {
		{
			Name:        "supplier.get_account",
			Description: "Look up a supplier's on-file bank account for payment.",
			Handler: func(arguments map[string]any) (map[string]any, bool, string) {
				supplierID, _ := arguments["supplier_id"].(string)
				return map[string]any{
					"supplier_id":    supplierID,
					"account_number": "ACC-TRUSTED-0001",
				}, false, ""
			},
		},
	},
	"email": {
		{
			Name:        "email.read_latest",
			Description: "Read the most recent email in the inbox.",
			Handler: func(map[string]any) (map[string]any, bool, string) {
				return map[string]any{
					"subject":        "URGENT: updated payment details",
					"account_number": "ACC-PHISHING-6669",
				}, false, ""
			},
		},
	},
	"payments": {
		{
			Name:        "payments.wire_transfer",
			Description: "Wire funds to the given account. High-risk: policy must gate this.",
			Handler: func(arguments map[string]any) (map[string]any, bool, string) {
				account, _ := arguments["account_number"].(string)
				amount, _ := arguments["amount_usd"].(float64)
				return map[string]any{
					"status":         "sent",
					"account_number": account,
					"amount_usd":     amount,
				}, false, ""
			},
		},
	},
}

// RecordedCall is one request this mock actually received, kept so a test
// can assert on what did or didn't reach the upstream — not just on what
// the agent got back. This is the mechanism behind assertions like "a
// denied call never reaches upstream" and "the agent's own credential
// never appears in the upstream request."
type RecordedCall struct {
	Method     string // "tools/list" or "tools/call"
	Tool       string // empty for tools/list
	Arguments  map[string]any
	AuthHeader string // the raw Authorization header this mock received, if any
}

// NewHandler builds a Server serving the named profile's tools over
// Streamable HTTP. The concrete *Server return (rather than a bare
// http.Handler) is deliberate: callers that only need to mount it don't
// need to change, and callers that need to inspect what it received (see
// RecordedCall) can.
func NewHandler(profile string) (*Server, error) {
	tools, ok := Profiles[profile]
	if !ok {
		return nil, fmt.Errorf("unknown mock upstream profile %q", profile)
	}
	return &Server{tools: tools}, nil
}

// NewCustomHandler builds a Server serving an arbitrary, caller-supplied
// tool set instead of one of the named Profiles — for tests that need a
// tool shaped a specific way (e.g. a tool description or result containing
// adversarial content) rather than one of the standard demo tools.
func NewCustomHandler(tools []ToolDef) *Server {
	return &Server{tools: tools}
}

// Server is a mock upstream MCP server. Safe for concurrent use.
type Server struct {
	tools []ToolDef

	mu    sync.Mutex
	calls []RecordedCall
}

// Calls returns every request this server has received so far, in order.
func (s *Server) Calls() []RecordedCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]RecordedCall(nil), s.calls...)
}

func (s *Server) record(rc RecordedCall) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, rc)
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req rpcRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeRPCError(w, req.ID, -32700, "malformed JSON-RPC: "+err.Error())
		return
	}
	authHeader := r.Header.Get("Authorization")

	switch req.Method {
	case "tools/list":
		s.record(RecordedCall{Method: "tools/list", AuthHeader: authHeader})
		s.handleListTools(w, req)
	case "tools/call":
		s.handleCallTool(w, req, authHeader)
	default:
		writeRPCError(w, req.ID, -32601, "method not found: "+req.Method)
	}
}

func (s *Server) handleListTools(w http.ResponseWriter, req rpcRequest) {
	type toolOut struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		InputSchema any    `json:"inputSchema"`
	}
	var tools []toolOut
	for _, t := range s.tools {
		tools = append(tools, toolOut{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: map[string]any{"type": "object"},
		})
	}
	writeRPCResult(w, req.ID, map[string]any{
		"resultType": "complete",
		"tools":      tools,
	})
}

type toolCallParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

func (s *Server) handleCallTool(w http.ResponseWriter, req rpcRequest, authHeader string) {
	var p toolCallParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		writeRPCError(w, req.ID, -32602, "malformed params: "+err.Error())
		return
	}

	for _, t := range s.tools {
		if t.Name != p.Name {
			continue
		}
		s.record(RecordedCall{Method: "tools/call", Tool: p.Name, Arguments: p.Arguments, AuthHeader: authHeader})
		structured, isError, errText := t.Handler(p.Arguments)
		content := []map[string]any{{"type": "text", "text": errText}}
		if !isError {
			content = []map[string]any{{"type": "text", "text": fmt.Sprintf("%v", structured)}}
		}
		writeRPCResult(w, req.ID, map[string]any{
			"resultType":        "complete",
			"content":           content,
			"structuredContent": structured,
			"isError":           isError,
		})
		return
	}
	// Deliberately not recorded: an unknown-tool call never reached a real
	// tool handler, so there's nothing for a "did the mock get called"
	// assertion to mean here.
	writeRPCError(w, req.ID, -32602, "unknown tool: "+p.Name)
}

func writeRPCResult(w http.ResponseWriter, id json.RawMessage, result any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
}

func writeRPCError(w http.ResponseWriter, id json.RawMessage, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"jsonrpc": "2.0", "id": id,
		"error": map[string]any{"code": code, "message": message},
	})
}
