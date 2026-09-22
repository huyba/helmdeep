// Copyright (c) 2026 Overarching AI LLC
// SPDX-License-Identifier: Apache-2.0

package devissuer

import (
	"encoding/json"
	"net/http"
	"time"
)

// Server exposes an Issuer over HTTP: a dev-only stand-in for the
// combination of an Enterprise IdP and an OAuth token-exchange
// authorization server (doc 04 §3, §6). Three endpoints:
//
//   - POST /issue — dev convenience for minting a SIT directly (a real
//     deployment's runtime would obtain one via sandbox attestation; Phase 0
//     has no sandbox to attest, so this is the substitute).
//   - POST /token-exchange — RFC 8693-shaped on-behalf-of exchange.
//   - GET /.well-known/jwks.json — the public key set, for the Gateway's
//     JWTResolver to verify signatures without ever holding the private key.
type Server struct {
	issuer      *Issuer
	sitTTL      time.Duration
	exchangeTTL time.Duration
}

// NewServer wraps issuer for HTTP use. sitTTL bounds SITs minted via
// /issue; exchangeTTL bounds tokens minted via /token-exchange — per doc
// 04 §1.1 ("<= 15 min") and the credential-ladder's "short-lived" per-call
// tokens, exchangeTTL should be much shorter than sitTTL.
func NewServer(issuer *Issuer, sitTTL, exchangeTTL time.Duration) *Server {
	return &Server{issuer: issuer, sitTTL: sitTTL, exchangeTTL: exchangeTTL}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/issue":
		s.handleIssue(w, r)
	case "/token-exchange":
		s.handleExchange(w, r)
	case "/.well-known/jwks.json":
		s.handleJWKS(w, r)
	default:
		http.NotFound(w, r)
	}
}

type issueRequest struct {
	Agent           string   `json:"agent"`
	DelegationChain []string `json:"delegation_chain"`
	Scope           []string `json:"scope"`
	AgentVersion    string   `json:"agent_version"`
}

type issueResponse struct {
	Token     string `json:"token"`
	ExpiresIn int    `json:"expires_in"`
}

func (s *Server) handleIssue(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req issueRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	token, err := s.issuer.IssueSIT(req.Agent, req.DelegationChain, req.Scope, req.AgentVersion, s.sitTTL)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, issueResponse{Token: token, ExpiresIn: int(s.sitTTL.Seconds())})
}

type exchangeResponse struct {
	AccessToken     string `json:"access_token"`
	IssuedTokenType string `json:"issued_token_type"`
	TokenType       string `json:"token_type"`
	ExpiresIn       int    `json:"expires_in"`
}

// handleExchange implements the subset of RFC 8693 this repo needs: form
// fields grant_type, subject_token, resource (the upstream name), and a
// non-standard `tool` field — RFC 8693 has no notion of per-tool
// granularity, and doc 04's credential ladder needs exactly that, so this
// is a documented, deliberate extension rather than a spec deviation
// nobody chose. See docs/adr/0007-credential-broker-scope.md.
func (s *Server) handleExchange(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	const expectedGrantType = "urn:ietf:params:oauth:grant-type:token-exchange"
	if gt := r.FormValue("grant_type"); gt != expectedGrantType {
		writeJSONError(w, http.StatusBadRequest, "unsupported_grant_type", "grant_type must be "+expectedGrantType)
		return
	}

	subjectToken := r.FormValue("subject_token")
	upstream := r.FormValue("resource")
	tool := r.FormValue("tool")
	if subjectToken == "" || upstream == "" || tool == "" {
		writeJSONError(w, http.StatusBadRequest, "invalid_request", "subject_token, resource, and tool are all required")
		return
	}

	result, err := s.issuer.Exchange(r.Context(), subjectToken, upstream, tool, s.exchangeTTL)
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "invalid_grant", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, exchangeResponse{ //nolint:gosec // G101: "AccessToken" is an RFC 8693 response field name, not a hardcoded credential
		AccessToken:     result.Token,
		IssuedTokenType: "urn:ietf:params:oauth:token-type:access_token",
		TokenType:       "Bearer",
		ExpiresIn:       int(s.exchangeTTL.Seconds()),
	})
}

func (s *Server) handleJWKS(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(s.issuer.PublicKeySet())
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeJSONError(w http.ResponseWriter, status int, code, description string) {
	writeJSON(w, status, map[string]string{"error": code, "error_description": description})
}
