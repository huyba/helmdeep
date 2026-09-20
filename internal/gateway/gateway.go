// Copyright (c) 2026 Overarching AI LLC
// SPDX-License-Identifier: Apache-2.0

// Package gateway implements the Tool Gateway's core request handling:
// resolving caller identity, building a decision request, consulting the
// PDP, enforcing the verdict (allow / deny / allow-with-obligations),
// writing an action record, and forwarding allowed calls upstream with the
// gateway's own credentials.
//
// It is internal because nothing outside cmd/helmdeep-gateway should import
// gateway internals directly — the stable, importable contracts are the
// interfaces in pkg/.
package gateway

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/huyba/helmdeep/pkg/audit"
	"github.com/huyba/helmdeep/pkg/identity"
	"github.com/huyba/helmdeep/pkg/mcp"
	"github.com/huyba/helmdeep/pkg/policy"
	"github.com/huyba/helmdeep/pkg/types"
)

// Gateway implements mcp.Handler. It depends only on the interfaces in
// pkg/identity, pkg/policy, pkg/audit, and pkg/mcp — never on any concrete
// implementation of them — so that swapping, say, StaticTokenResolver for a
// real identity provider is a wiring change in cmd/helmdeep-gateway's main,
// not a change here. See ARCHITECTURE.md.
type Gateway struct {
	identity identity.Resolver
	pdp      policy.Decider
	auditLog audit.Store
	registry mcp.Registry

	// upstreamProvenance is the trust label attached to values returned by
	// each upstream, keyed by mcp.Upstream.Name(). This is what makes the
	// taint example real rather than assumed: an upstream backing an email
	// inbox and an upstream backing a supplier master record are not
	// equally trusted just because the gateway mediates both. An upstream
	// with no entry here defaults to untrustedProvenance — configuration
	// must opt an upstream into being a trusted source, never the reverse.
	upstreamProvenance map[string]types.Provenance

	// decisionTimeout bounds how long a single PDP.Decide call is allowed
	// to run before the gateway gives up and denies. Zero means no bound —
	// the request's own context is the only limit. A Decider that hangs
	// (a future non-embedded implementation over a network, for instance)
	// must not be able to hang tools/call indefinitely; see
	// docs/adr/0003-fail-closed-behavior.md — a timeout is treated exactly
	// like any other Decide error: deny.
	decisionTimeout time.Duration

	// broker mints a per-call, scoped, expiring downstream credential from
	// the caller's own verified credential (docs/04-identity-authz.md §3)
	// — see docs/adr/0007-credential-broker-scope.md. Nil is a valid,
	// supported configuration: upstreams then use whatever static
	// credential they were configured with (pkg/mcp.HTTPUpstream's
	// pre-Milestone-M1 behavior), which remains correct for an upstream
	// that genuinely has no finer-grained credential to hand out.
	broker identity.CredentialBroker

	usage *usageTracker
	prov  *provenanceCache
	now   func() time.Time
}

// New builds a Gateway from its dependencies. identity, pdp, auditLog, and
// registry are all interfaces; see the type's doc comment for why that
// matters. upstreamProvenance maps upstream name to the provenance label
// applied to values it returns — see the field's doc comment.
// decisionTimeout bounds each policy decision; pass 0 for no bound. broker
// may be nil — see the field's doc comment.
func New(id identity.Resolver, pdp policy.Decider, auditLog audit.Store, registry mcp.Registry, upstreamProvenance map[string]types.Provenance, decisionTimeout time.Duration, broker identity.CredentialBroker) *Gateway {
	return &Gateway{
		identity:           id,
		pdp:                pdp,
		auditLog:           auditLog,
		registry:           registry,
		upstreamProvenance: upstreamProvenance,
		decisionTimeout:    decisionTimeout,
		broker:             broker,
		usage:              newUsageTracker(),
		prov:               newProvenanceCache(),
		now:                time.Now,
	}
}

// decide wraps pdp.Decide with the configured decision timeout, if any.
func (g *Gateway) decide(ctx context.Context, req types.DecisionRequest) (types.DecisionResponse, error) {
	if g.decisionTimeout <= 0 {
		return g.pdp.Decide(ctx, req)
	}
	ctx, cancel := context.WithTimeout(ctx, g.decisionTimeout)
	defer cancel()
	return g.pdp.Decide(ctx, req)
}

// ListTools returns the tools the caller identified by cc.Credential is
// permitted to call — an agent should not learn about a tool it can't use,
// not just be blocked from calling it. An unresolvable credential sees an
// empty tool list, not an error: "we don't know who you are" and "you may
// call nothing" are the same fact from the caller's side.
//
// v1 does not paginate: cursor is accepted (per the wire contract) and
// ignored, and every result is a single page. This is a real simplification
// worth revisiting if a deployment's tool catalog grows large enough for it
// to matter — nothing here would need to change shape to add it later.
func (g *Gateway) ListTools(ctx context.Context, cc mcp.CallContext, cursor string) (mcp.ListToolsResult, error) {
	subject, err := g.identity.Resolve(ctx, cc.Credential)
	if err != nil {
		return mcp.ListToolsResult{}, nil
	}

	now := g.now()
	var visible []types.Tool
	for _, tool := range g.registry.Tools() {
		req := types.DecisionRequest{
			Subject: subject,
			Action:  types.Action{Type: "tool_call", Tool: tool.Name},
			Context: types.DecisionContext{
				RequestID: cc.RequestID,
				Time:      now,
				Usage:     g.usage.usagePeek(subject.ID, now),
			},
		}
		decision, err := g.decide(ctx, req)
		if err != nil {
			continue // fail closed per tool: an error hides exactly that one tool, not the whole list
		}
		if decision.Outcome == types.OutcomeAllow || decision.Outcome == types.OutcomeAllowWithObligations {
			visible = append(visible, tool)
		}
	}
	return mcp.ListToolsResult{Tools: visible}, nil
}

// CallTool is the gateway's hot path: resolve identity, decide, enforce,
// record, forward. See docs/adr/0003-fail-closed-behavior.md — every branch
// that isn't an explicit, well-formed allow ends in a denial, and denials
// are returned as tool execution errors (isError: true) so the calling
// model gets an actionable reason, per MCP's own guidance that execution
// errors (unlike protocol errors) are things a model can react to.
func (g *Gateway) CallTool(ctx context.Context, cc mcp.CallContext, name string, arguments map[string]any) (types.ToolResult, error) {
	now := g.now()

	subject, err := g.identity.Resolve(ctx, cc.Credential)
	if err != nil {
		return g.denyWithoutSubject(ctx, name, arguments, "identity.unresolved", err.Error(), now)
	}

	upstream, ok := g.registry.Resolve(name)
	if !ok {
		// Matches the MCP spec's own example for this exact case: a
		// protocol error, not a policy denial — the request refers to
		// something that doesn't exist, independent of who's asking.
		return types.ToolResult{}, &mcp.ProtocolError{Code: -32602, Message: "unknown tool: " + name}
	}

	action := types.Action{
		Type:      "tool_call",
		Tool:      name,
		Arguments: g.taintArguments(subject.ID, arguments, now),
	}
	req := types.DecisionRequest{
		Subject: subject,
		Action:  action,
		Context: types.DecisionContext{
			RequestID: cc.RequestID,
			Time:      now,
			Usage:     g.usage.usage(subject.ID, now),
		},
	}

	decision, err := g.decide(ctx, req)
	if err != nil {
		// Belt and suspenders on top of OPADecider's own fail-closed
		// behavior: whatever Decider this gateway is wired to, an error
		// here is a deny — see docs/adr/0003-fail-closed-behavior.md.
		decision = types.DecisionResponse{Outcome: types.OutcomeDeny, PolicyID: "pdp.error", Reason: err.Error()}
	}

	// allowed is deliberately an allow-list, not `!= OutcomeDeny`: the zero
	// value of types.Outcome is "", and an allow-list makes that — and any
	// other value a Decider might return that isn't one of the two allow
	// outcomes — deny by construction. A deny-list here would mean a
	// Decider that returns a zero-value DecisionResponse on some
	// unhandled branch gets treated as an allow, which is exactly the
	// silent-vulnerability shape docs/adr/0003-fail-closed-behavior.md
	// exists to rule out. See internal/gateway/gateway_test.go's
	// TestZeroValueDecisionResponseIsDenied.
	allowed := decision.Outcome == types.OutcomeAllow || decision.Outcome == types.OutcomeAllowWithObligations

	recordOutcome := types.RecordOutcomeDenied
	if allowed {
		recordOutcome = types.RecordOutcomeAllowed
	}

	// The decision record is committed before any upstream side effect —
	// see docs/adr/0003 and docs/adr/0004. If we can't durably record the
	// decision, we don't act on it, regardless of what the PDP said.
	if err := g.auditLog.Append(ctx, types.ActionRecord{
		Timestamp: now,
		Subject:   subject,
		Action:    action,
		Decision:  decision,
		Outcome:   recordOutcome,
	}); err != nil {
		return deniedResult("could not durably record this decision, denying: " + err.Error()), nil
	}

	if !allowed {
		reason := decision.Reason
		if reason == "" {
			reason = "denied by policy"
		}
		return deniedResult(reason), nil
	}

	credential := ""
	if g.broker != nil {
		minted, err := g.broker.Exchange(ctx, cc.Credential, upstream.Name(), name)
		if err != nil {
			// The decision was Allowed, but we could not obtain a scoped
			// credential to act on it with — proceeding on some other,
			// less-scoped credential would defeat the entire point of
			// having a broker configured. Fail closed here exactly like
			// every other operational failure — see
			// docs/adr/0006-operational-failure-classification.md.
			exchangeErr := err
			if err := g.auditLog.Append(ctx, types.ActionRecord{
				Timestamp: g.now(),
				Subject:   subject,
				Action:    action,
				Decision:  decision,
				Outcome:   types.RecordOutcomeFailed,
			}); err != nil {
				slog.Error("failed to record credential exchange failure", "tool", name, "error", err)
			}
			return failedResult("could not obtain a scoped upstream credential: " + exchangeErr.Error()), nil
		}
		credential = minted
	}

	result, callErr := upstream.CallTool(ctx, types.ToolCall{Tool: name, Arguments: action.Arguments, Credential: credential})
	if callErr != nil {
		// The decision was Allowed and already recorded as such; this is a
		// second, separate record for the distinct fact that the upstream
		// call itself then failed — see docs/adr/0004-action-record-format.md.
		if err := g.auditLog.Append(ctx, types.ActionRecord{
			Timestamp: g.now(),
			Subject:   subject,
			Action:    action,
			Decision:  decision,
			Outcome:   types.RecordOutcomeFailed,
		}); err != nil {
			slog.Error("failed to record upstream failure", "tool", name, "error", err)
		}
		// A tool execution error (isError: true), not a protocol error —
		// see docs/adr/0006-operational-failure-classification.md. The
		// call was authorized; it's the environment that failed, and the
		// calling model can potentially react to that the same way it
		// reacts to a denial.
		return failedResult("upstream call failed: " + callErr.Error()), nil
	}

	if decision.Outcome == types.OutcomeAllowWithObligations {
		result = applyObligations(result, decision.Obligations)
	}

	g.recordResultProvenance(subject.ID, upstream.Name(), result, now)
	return result, nil
}

// denyWithoutSubject handles the case where identity itself couldn't be
// resolved. It still writes an audit record — a denial with no known
// subject is exactly the kind of event SECURITY.md and doc J2 (an
// incident investigation) need to be able to find later, not one that's
// safe to leave unrecorded.
func (g *Gateway) denyWithoutSubject(ctx context.Context, name string, arguments map[string]any, policyID, reason string, now time.Time) (types.ToolResult, error) {
	decision := types.DecisionResponse{Outcome: types.OutcomeDeny, PolicyID: policyID, Reason: reason}
	action := types.Action{Type: "tool_call", Tool: name}
	if err := g.auditLog.Append(ctx, types.ActionRecord{
		Timestamp: now,
		Subject:   types.Subject{ID: "unknown", Kind: types.SubjectKindAgent},
		Action:    action,
		Decision:  decision,
		Outcome:   types.RecordOutcomeDenied,
	}); err != nil {
		slog.Error("failed to record unresolved-identity denial", "tool", name, "error", err)
	}
	return deniedResult("credential not recognized"), nil
}

func deniedResult(reason string) types.ToolResult {
	return textErrorResult("denied: " + reason)
}

func failedResult(reason string) types.ToolResult {
	return textErrorResult(reason)
}

func textErrorResult(text string) types.ToolResult {
	content, _ := json.Marshal([]map[string]any{
		{"type": "text", "text": text},
	})
	return types.ToolResult{Content: content, IsError: true}
}

// taintArguments tags every top-level string argument with the provenance
// the gateway has on file for it, defaulting to untrusted for anything it
// has no record of. See provenance.go for what this does and doesn't cover.
func (g *Gateway) taintArguments(subjectID string, arguments map[string]any, now time.Time) map[string]types.Value {
	tainted := make(map[string]types.Value, len(arguments))
	for k, v := range arguments {
		prov := untrustedProvenance
		if s, ok := v.(string); ok {
			prov = g.prov.resolve(subjectID, s, now)
		}
		tainted[k] = types.Value{Data: v, Provenance: prov}
	}
	return tainted
}

// recordResultProvenance is the other half of taint tracking: after an
// allowed call succeeds, remember the values it returned as having come
// from this upstream, so a later call that reuses one of them can be
// tagged correctly. See provenance.go.
func (g *Gateway) recordResultProvenance(subjectID, upstreamName string, result types.ToolResult, now time.Time) {
	prov, ok := g.upstreamProvenance[upstreamName]
	if !ok {
		prov = types.Provenance{Source: upstreamName, Trusted: false}
	}
	if len(result.StructuredContent) > 0 {
		var decoded any
		if err := json.Unmarshal(result.StructuredContent, &decoded); err == nil {
			g.prov.recordTopLevelStrings(subjectID, decoded, prov, now)
		}
	}
}

var _ mcp.Handler = (*Gateway)(nil)
