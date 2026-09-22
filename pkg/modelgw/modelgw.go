// Copyright (c) 2026 Overarching AI LLC
// SPDX-License-Identifier: Apache-2.0

// Package modelgw is the Model Gateway (docs/07-model-gateway.md):
// policy-gated, audited invocation of a model provider, on the same
// hash-chained ledger the Tool Gateway writes to (pkg/audit). Milestone M5
// implements a deliberately narrow slice of doc 07 — explicit version
// pinning and recorded (never silent) fallback between exactly two real
// providers — not the full normalized request schema, Model Catalog,
// safety pipeline, or caching layers doc 07 also describes. See
// docs/adr/0011-model-gateway.md for what's simplified and why.
//
// Gateway is a Go library, not an HTTP or MCP-facing server: nothing in
// this repo yet is an agent runtime that would call it over the wire (see
// pkg/sandbox, an interface-only stub), so there is no wire protocol to
// design against a real caller. It is meant to be called in-process,
// exactly the shape a future agent runtime's model-calling code would
// use.
package modelgw

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/huyba/helmdeep/pkg/audit"
	"github.com/huyba/helmdeep/pkg/policy"
	"github.com/huyba/helmdeep/pkg/types"
)

// Message is one turn in a chat-style request — the minimal shape every
// provider in this package can express. Doc 07 §2's full ModelRequest
// (typed content parts, tool schemas, structured output contracts) is not
// implemented; see docs/adr/0011-model-gateway.md.
type Message struct {
	Role    string // "system" | "user" | "assistant"
	Content string
}

// Request is what a caller asks the Model Gateway to do. Purpose selects
// a configured Route (doc 07 §2: "Agents declare purpose and constraints,
// not a model name, by default") — it is a routing key into Gateway's own
// config, not a free-form label a provider ever sees.
type Request struct {
	Purpose     string
	Messages    []Message
	MaxTokens   int
	Temperature float64
}

// Usage is token accounting for one call — doc 07 §4's outbound step 9
// ("Cost/token accounting and Action Record write").
type Usage struct {
	PromptTokens     int
	CompletionTokens int
}

// Response is a successful completion. Model is the *actual* resolved
// model version the provider reports back, not merely the config value
// that was requested — see Provider's doc comment on why that distinction
// matters for version pinning. FallbackFrom is non-empty if the primary
// provider failed and this response came from the route's configured
// fallback instead (doc 07 §3.2: "Fallback is recorded, never silent").
type Response struct {
	Provider     string
	Model        string
	Content      string
	Usage        Usage
	FallbackFrom string
}

// Provider is one model backend. Complete's model parameter is always an
// explicit version from a Route — never "latest" or any other alias (doc
// 07 §3.1's version-pinning rule; Route validates this at construction,
// not per-call, so a bad config fails at startup). A Provider
// implementation must return the *actual* model version the backend
// resolved the request to (most APIs echo this in their response) so a
// provider silently serving a different version than requested is
// detectable in the Action Record, not hidden by trusting the request
// echoed back.
type Provider interface {
	Name() string
	Complete(ctx context.Context, model string, req Request) (Response, error)
}

// Route pins exactly one provider+model pair for a Purpose, with an
// optional fallback pair doc 07 §3.2 calls for. Model and FallbackModel
// must be explicit versions; New rejects "latest" outright — see
// docs/adr/0011-model-gateway.md for why that's checked here and not left
// to a provider to enforce.
type Route struct {
	Provider         string
	Model            string
	FallbackProvider string // empty means no fallback configured for this route
	FallbackModel    string
}

// Gateway routes a Request to a configured Provider, gated by pdp (the
// same policy.Decider the Tool Gateway uses — a "model_call" Action is
// just another kind of DecisionRequest, per pkg/types.Action's own doc
// comment) and recorded to auditLog (the same audit.Store — "on the same
// ledger" is literal: a tool call and a model call chain into the exact
// same file, verified by the exact same `verify-chain` command).
type Gateway struct {
	providers map[string]Provider
	routes    map[string]Route
	pdp       policy.Decider
	auditLog  audit.Store
	now       func() time.Time
}

// New validates routes (every Model/FallbackModel is a real, non-"latest"
// string; every referenced provider name exists in providers) and returns
// a Gateway, or an error describing the first problem — a misconfigured
// route should fail loudly at startup, matching
// pkg/toolregistry.New's same convention.
func New(providers map[string]Provider, routes map[string]Route, pdp policy.Decider, auditLog audit.Store) (*Gateway, error) {
	for purpose, r := range routes {
		if r.Provider == "" {
			return nil, fmt.Errorf("route %q: provider is required", purpose)
		}
		if _, ok := providers[r.Provider]; !ok {
			return nil, fmt.Errorf("route %q: provider %q is not configured", purpose, r.Provider)
		}
		if err := validateModelVersion(purpose, "model", r.Model); err != nil {
			return nil, err
		}
		if r.FallbackProvider != "" {
			if _, ok := providers[r.FallbackProvider]; !ok {
				return nil, fmt.Errorf("route %q: fallback provider %q is not configured", purpose, r.FallbackProvider)
			}
			if err := validateModelVersion(purpose, "fallback_model", r.FallbackModel); err != nil {
				return nil, err
			}
		}
	}
	return &Gateway{providers: providers, routes: routes, pdp: pdp, auditLog: auditLog, now: time.Now}, nil
}

func validateModelVersion(purpose, field, model string) error {
	if model == "" {
		return fmt.Errorf("route %q: %s is required", purpose, field)
	}
	if model == "latest" {
		return fmt.Errorf("route %q: %s is %q — doc 07 §3.1 bans provider aliases; pin an explicit version", purpose, field, model)
	}
	return nil
}

// ErrDenied is returned when the policy decision for a call is anything
// other than allow. Distinct from a provider error: the call never
// reached a provider at all.
type ErrDenied struct {
	PolicyID string
	Reason   string
}

func (e *ErrDenied) Error() string {
	return fmt.Sprintf("model call denied (%s): %s", e.PolicyID, e.Reason)
}

// Complete evaluates subject's request against policy, and — only if
// allowed — calls the configured provider, falling back to the route's
// configured alternate on a provider error. Two audit records are
// written for an allowed call, mirroring internal/gateway.Gateway.CallTool's
// same split: the decision, before any provider is called (a denial stops
// here, with exactly one record); the outcome, after the call completes —
// success or failure — since token usage and the actually-resolved model
// version are only known afterward. See docs/adr/0003-fail-closed-behavior.md:
// an audit-write failure denies the call, exactly like the Tool Gateway.
func (g *Gateway) Complete(ctx context.Context, subject types.Subject, req Request) (Response, error) {
	now := g.now()
	route, ok := g.routes[req.Purpose]
	if !ok {
		return Response{}, g.denyf(ctx, subject, req, now, "modelgw.no_route", "no route configured for purpose %q", req.Purpose)
	}

	action := types.Action{Type: "model_call", Tool: req.Purpose}
	decisionReq := types.DecisionRequest{
		Subject: subject,
		Action:  action,
		Context: types.DecisionContext{Time: now},
	}
	decision, err := g.pdp.Decide(ctx, decisionReq)
	if err != nil {
		return Response{}, g.denyf(ctx, subject, req, now, "modelgw.pdp_error", "%v", err)
	}
	allowed := decision.Outcome == types.OutcomeAllow || decision.Outcome == types.OutcomeAllowWithObligations
	if !allowed {
		reason := decision.Reason
		if reason == "" {
			reason = "denied by policy"
		}
		if err := g.auditLog.Append(ctx, types.ActionRecord{
			Timestamp: now, Subject: subject, Action: action, Decision: decision, Outcome: types.RecordOutcomeDenied,
		}); err != nil {
			slog.Error("failed to record model_call denial", "purpose", req.Purpose, "error", err)
		}
		return Response{}, &ErrDenied{PolicyID: decision.PolicyID, Reason: reason}
	}

	// The decision itself is committed before any provider is called —
	// same reasoning as the Tool Gateway: if we can't durably record that
	// this call was authorized, we don't make it.
	if err := g.auditLog.Append(ctx, types.ActionRecord{
		Timestamp: now, Subject: subject, Action: action, Decision: decision, Outcome: types.RecordOutcomeAllowed,
	}); err != nil {
		return Response{}, &ErrDenied{PolicyID: "modelgw.audit_unavailable", Reason: "could not durably record this decision, denying: " + err.Error()}
	}

	resp, callErr := g.call(ctx, route, req)
	outcome := types.RecordOutcomeAllowed
	if callErr != nil {
		outcome = types.RecordOutcomeFailed
	}
	if err := g.auditLog.Append(ctx, types.ActionRecord{
		Timestamp: g.now(), Subject: subject, Action: g.outcomeAction(action, resp), Decision: decision, Outcome: outcome,
	}); err != nil {
		slog.Error("failed to record model_call outcome", "purpose", req.Purpose, "error", err)
	}
	if callErr != nil {
		return Response{}, callErr
	}
	return resp, nil
}

// call tries the route's primary provider, then its fallback (if
// configured), never silently — a fallback is only ever reachable via
// Response.FallbackFrom, which g.outcomeAction folds into the audit
// record's own Action.Tool so it is visible in the ledger too.
func (g *Gateway) call(ctx context.Context, route Route, req Request) (Response, error) {
	primary := g.providers[route.Provider]
	resp, err := primary.Complete(ctx, route.Model, req)
	if err == nil {
		return resp, nil
	}
	if route.FallbackProvider == "" {
		return Response{}, fmt.Errorf("provider %q failed and no fallback is configured: %w", route.Provider, err)
	}
	slog.Warn("model gateway falling back", "purpose", req.Purpose, "from_provider", route.Provider, "to_provider", route.FallbackProvider, "error", err)
	fallback := g.providers[route.FallbackProvider]
	resp, fbErr := fallback.Complete(ctx, route.FallbackModel, req)
	if fbErr != nil {
		return Response{}, fmt.Errorf("provider %q failed (%v), and fallback provider %q also failed: %w", route.Provider, err, route.FallbackProvider, fbErr)
	}
	resp.FallbackFrom = route.Provider
	return resp, nil
}

// outcomeAction folds which provider/model actually served the call back
// into the Action recorded for the outcome record, so a fallback is
// visible in the ledger itself, not only in the in-process Response.
func (g *Gateway) outcomeAction(action types.Action, resp Response) types.Action {
	if resp.Provider == "" {
		return action // the call failed outright; nothing to add
	}
	detail := resp.Provider + "/" + resp.Model
	if resp.FallbackFrom != "" {
		detail += " (fallback from " + resp.FallbackFrom + ")"
	}
	action.Resource = detail
	return action
}

func (g *Gateway) denyf(ctx context.Context, subject types.Subject, req Request, now time.Time, policyID, format string, args ...any) error {
	reason := fmt.Sprintf(format, args...)
	decision := types.DecisionResponse{Outcome: types.OutcomeDeny, PolicyID: policyID, Reason: reason}
	action := types.Action{Type: "model_call", Tool: req.Purpose}
	if err := g.auditLog.Append(ctx, types.ActionRecord{
		Timestamp: now, Subject: subject, Action: action, Decision: decision, Outcome: types.RecordOutcomeDenied,
	}); err != nil {
		slog.Error("failed to record model_call denial", "purpose", req.Purpose, "error", err)
	}
	return &ErrDenied{PolicyID: policyID, Reason: reason}
}
