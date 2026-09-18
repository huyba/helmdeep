// Copyright (c) 2026 Overarching AI LLC
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// Server is the agent-facing MCP listener: a Streamable HTTP transport
// (2026-07-28 spec) that validates every request's headers and `_meta`
// before handing it to a Handler. It implements Listener.
//
// Server owns wire-protocol correctness only. It has no notion of policy,
// identity, or upstream servers — see Handler for where that lives.
type Server struct {
	// AllowedOrigins, if non-empty, is the allowlist Origin headers are
	// checked against (DNS-rebinding protection, required by spec). Empty
	// means Origin is not checked — appropriate when the gateway sits
	// behind an internal ingress with no direct browser exposure, but
	// that's a deployment assumption worth restating: see SECURITY.md.
	AllowedOrigins []string

	handler Handler
	version string
	http    *http.Server
}

// NewServer builds a Server that listens on addr and serves the single MCP
// endpoint at path (e.g. "/mcp"). version is reported in serverInfo.
func NewServer(addr, path string, h Handler, version string) *Server {
	s := &Server{handler: h, version: version}
	mux := http.NewServeMux()
	mux.HandleFunc(path, s.serveHTTP)
	s.http = &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second, // mitigate Slowloris-style slow-header attacks
	}
	return s
}

// Handler returns the underlying http.Handler, so tests can drive it with
// httptest.NewServer instead of binding a real socket via Serve.
func (s *Server) Handler() http.Handler { return s.http.Handler }

// Serve blocks until ctx is cancelled or the HTTP server fails to start.
func (s *Server) Serve(ctx context.Context) error {
	errCh := make(chan error, 1)
	go func() { errCh <- s.http.ListenAndServe() }()
	select {
	case <-ctx.Done():
		return s.Shutdown(context.Background())
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

// Shutdown gracefully stops the HTTP server.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.http.Shutdown(ctx)
}

func (s *Server) serveHTTP(w http.ResponseWriter, r *http.Request) {
	// Security & Endpoint: validate Origin before anything else, per spec.
	if origin := r.Header.Get("Origin"); origin != "" && len(s.AllowedOrigins) > 0 && !contains(s.AllowedOrigins, origin) {
		http.Error(w, "origin not allowed", http.StatusForbidden)
		return
	}

	// This revision of Streamable HTTP has no GET stream and no session to
	// DELETE — see docs/adr/0005-mcp-protocol-compatibility.md's "minimal
	// legacy tolerance."
	switch r.Method {
	case http.MethodGet, http.MethodDelete:
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	case http.MethodPost:
		// continue below
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req rpcRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, nil, rpcError{Code: codeParseError, Message: "malformed JSON-RPC message: " + err.Error()})
		return
	}

	if req.isNotification() {
		// No client-to-server notification is defined on this transport in
		// the core protocol — see the spec's Streamable HTTP page.
		writeError(w, http.StatusBadRequest, nil, rpcError{Code: codeInvalidRequest, Message: "notifications are not accepted on this endpoint"})
		return
	}

	// Standard request headers (Mcp-Method, and Mcp-Name for tools/call):
	// required, and MUST match the body. Checked before semantic
	// processing so a gateway/intermediary could route on headers alone —
	// see ADR 0005.
	if mismatch := s.validateStandardHeaders(r, req); mismatch != nil {
		writeError(w, http.StatusBadRequest, req.ID, *mismatch)
		return
	}

	meta, mErr := parseMeta(req.Params)
	if mErr != nil {
		writeError(w, http.StatusBadRequest, req.ID, *mErr)
		return
	}

	// MCP-Protocol-Version header MUST match _meta's protocolVersion.
	headerVersion := r.Header.Get("MCP-Protocol-Version")
	if headerVersion == "" {
		writeError(w, http.StatusBadRequest, req.ID, rpcError{Code: codeHeaderMismatch, Message: "missing required header: MCP-Protocol-Version"})
		return
	}
	if headerVersion != meta.ProtocolVersion {
		writeError(w, http.StatusBadRequest, req.ID, rpcError{Code: codeHeaderMismatch, Message: "MCP-Protocol-Version header does not match _meta[\"io.modelcontextprotocol/protocolVersion\"]"})
		return
	}

	// Only now do we ask whether the version itself is one we support —
	// header/body agreement is checked first, and independently, of
	// whether we like the value both sides agree on.
	if ProtocolVersion(meta.ProtocolVersion) != ProtocolVersionCurrent {
		writeError(w, http.StatusBadRequest, req.ID, rpcError{
			Code:    codeUnsupportedProtocolVersion,
			Message: "unsupported protocol version",
			Data: unsupportedProtocolVersionData{
				Supported: []string{string(ProtocolVersionCurrent)},
				Requested: meta.ProtocolVersion,
			},
		})
		return
	}

	cc := CallContext{
		Credential:      bearerToken(r),
		ClientInfo:      meta.ClientInfo,
		ProtocolVersion: meta.ProtocolVersion,
		RequestID:       newRequestID(),
	}

	s.dispatch(w, r.Context(), req, cc)
}

func (s *Server) dispatch(w http.ResponseWriter, ctx context.Context, req rpcRequest, cc CallContext) {
	switch req.Method {
	case "server/discover":
		s.handleDiscover(w, req)
	case "tools/list":
		s.handleListTools(w, ctx, req, cc)
	case "tools/call":
		s.handleCallTool(w, ctx, req, cc)
	default:
		writeError(w, http.StatusNotFound, req.ID, rpcError{Code: codeMethodNotFound, Message: "method not found: " + req.Method})
	}
}

func (s *Server) handleDiscover(w http.ResponseWriter, req rpcRequest) {
	result := map[string]any{
		"resultType":        "complete",
		"supportedVersions": []string{string(ProtocolVersionCurrent)},
		"capabilities": map[string]any{
			"tools": map[string]any{"listChanged": false},
		},
		"instructions": "HelmDeep Tool Gateway: forwards tools/list and tools/call to upstream MCP servers, subject to policy.",
		"_meta":        serverInfoMeta(s.version),
	}
	writeResult(w, req.ID, result)
}

type listToolsParams struct {
	Cursor string `json:"cursor,omitempty"`
}

func (s *Server) handleListTools(w http.ResponseWriter, ctx context.Context, req rpcRequest, cc CallContext) {
	var p listToolsParams
	if len(req.Params) > 0 {
		_ = json.Unmarshal(req.Params, &p) // cursor is optional; malformed extra fields are ignored
	}

	result, err := s.handler.ListTools(ctx, cc, p.Cursor)
	if err != nil {
		s.internalError(w, req.ID, "tools/list", err)
		return
	}

	out := map[string]any{
		"resultType": "complete",
		"tools":      result.Tools,
		"_meta":      serverInfoMeta(s.version),
	}
	if result.NextCursor != "" {
		out["nextCursor"] = result.NextCursor
	}
	writeResult(w, req.ID, out)
}

type toolCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

func (s *Server) handleCallTool(w http.ResponseWriter, ctx context.Context, req rpcRequest, cc CallContext) {
	var p toolCallParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		writeError(w, http.StatusBadRequest, req.ID, rpcError{Code: codeInvalidParams, Message: "malformed tools/call params: " + err.Error()})
		return
	}

	var args map[string]any
	if len(p.Arguments) > 0 {
		if err := json.Unmarshal(p.Arguments, &args); err != nil {
			writeError(w, http.StatusBadRequest, req.ID, rpcError{Code: codeInvalidParams, Message: "malformed arguments: " + err.Error()})
			return
		}
	}

	result, err := s.handler.CallTool(ctx, cc, p.Name, args)
	if err != nil {
		var pe *ProtocolError
		if errors.As(err, &pe) {
			writeError(w, http.StatusBadRequest, req.ID, rpcError{Code: pe.Code, Message: pe.Message})
			return
		}
		s.internalError(w, req.ID, "tools/call", err)
		return
	}

	out := map[string]any{
		"resultType": "complete",
		"content":    rawOrEmptyArray(result.Content),
		"isError":    result.IsError,
		"_meta":      serverInfoMeta(s.version),
	}
	if len(result.StructuredContent) > 0 {
		out["structuredContent"] = result.StructuredContent
	}
	writeResult(w, req.ID, out)
}

// internalError logs the real error server-side and returns a generic
// message to the caller — the caller learns that something failed, not
// gateway-internal detail that might help an attacker.
func (s *Server) internalError(w http.ResponseWriter, id json.RawMessage, method string, err error) {
	slog.Error("mcp handler error", "method", method, "error", err)
	writeError(w, http.StatusInternalServerError, id, rpcError{Code: codeInternalError, Message: "internal error"})
}

// validateStandardHeaders checks Mcp-Method (all requests) and Mcp-Name
// (tools/call) against the parsed body, per the spec's Server Validation
// rules. Returns nil if everything matches.
func (s *Server) validateStandardHeaders(r *http.Request, req rpcRequest) *rpcError {
	method := r.Header.Get("Mcp-Method")
	if method == "" {
		return &rpcError{Code: codeHeaderMismatch, Message: "missing required header: Mcp-Method"}
	}
	if method != req.Method {
		return &rpcError{Code: codeHeaderMismatch, Message: "Mcp-Method header does not match body method"}
	}

	if req.Method != "tools/call" {
		return nil
	}

	name := r.Header.Get("Mcp-Name")
	if name == "" {
		return &rpcError{Code: codeHeaderMismatch, Message: "missing required header: Mcp-Name"}
	}
	decoded, err := decodeHeaderValue(name)
	if err != nil {
		return &rpcError{Code: codeHeaderMismatch, Message: "Mcp-Name header is not validly encoded: " + err.Error()}
	}

	var p toolCallParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		// Body isn't parseable as tools/call params at all; the later,
		// method-specific parse will report this properly. Header
		// validation has nothing to compare against.
		return nil
	}
	if decoded != p.Name {
		return &rpcError{Code: codeHeaderMismatch, Message: "Mcp-Name header does not match params.name"}
	}
	return nil
}

const (
	b64SentinelPrefix = "=?base64?"
	b64SentinelSuffix = "?="
)

// decodeHeaderValue reverses the Base64 sentinel encoding the spec defines
// for header values that can't be represented as plain ASCII. A value
// without the sentinel markers is returned unchanged.
func decodeHeaderValue(v string) (string, error) {
	if !strings.HasPrefix(v, b64SentinelPrefix) || !strings.HasSuffix(v, b64SentinelSuffix) {
		return v, nil
	}
	inner := strings.TrimSuffix(strings.TrimPrefix(v, b64SentinelPrefix), b64SentinelSuffix)
	decoded, err := base64.StdEncoding.DecodeString(inner)
	if err != nil {
		return "", err
	}
	return string(decoded), nil
}

func bearerToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if strings.HasPrefix(auth, prefix) {
		return strings.TrimPrefix(auth, prefix)
	}
	return ""
}

func writeResult(w http.ResponseWriter, id json.RawMessage, result any) {
	resultJSON, err := json.Marshal(result)
	if err != nil {
		// A result we built ourselves failing to marshal is a bug in this
		// package, not a caller error; there's no good JSON-RPC code for
		// "the server's own response didn't serialize."
		slog.Error("mcp: failed to marshal result", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(rpcResultResponse{JSONRPC: "2.0", ID: id, Result: resultJSON})
}

func writeError(w http.ResponseWriter, status int, id json.RawMessage, rpcErr rpcError) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(rpcErrorResponse{JSONRPC: "2.0", ID: id, Error: rpcErr})
}

func contains(list []string, v string) bool {
	for _, item := range list {
		if item == v {
			return true
		}
	}
	return false
}

func rawOrEmptyArray(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage("[]")
	}
	return raw
}

// newRequestID generates a gateway-local unique identifier for one HTTP
// request. It is not a session ID — nothing about it is retained or
// accepted back from a client; it exists purely so this request's policy
// decision and audit record can be correlated with each other.
func newRequestID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand failing is effectively unrecoverable on any real
		// system; fall back to a fixed marker rather than panicking mid
		// request so the request can still be denied cleanly upstream.
		return "req-rand-unavailable"
	}
	return "req_" + hex.EncodeToString(b[:])
}
