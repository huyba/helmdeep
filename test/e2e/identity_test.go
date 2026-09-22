// Copyright (c) 2026 Overarching AI LLC
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/lestrrat-go/jwx/v3/jwt"

	"github.com/huyba/helmdeep/internal/devissuer"
	"github.com/huyba/helmdeep/internal/gateway"
	"github.com/huyba/helmdeep/internal/mockupstream"
	"github.com/huyba/helmdeep/pkg/agentregistry"
	"github.com/huyba/helmdeep/pkg/audit"
	"github.com/huyba/helmdeep/pkg/identity"
	"github.com/huyba/helmdeep/pkg/mcp"
	"github.com/huyba/helmdeep/pkg/policy"
	"github.com/huyba/helmdeep/pkg/types"
)

const identityTestAudience = "helmdeep-gateways"

// identityGatewaySetup wires a Gateway with real JWT identity + credential
// broker instead of the static-token bootstrap the other e2e tests use —
// Milestone M1's actual subject under test.
type identityGatewaySetup struct {
	endpoint  string
	issuer    *devissuer.Issuer
	mock      *mockupstream.Server
	auditPath string
}

func newIdentityGateway(t *testing.T) identityGatewaySetup {
	t.Helper()

	issuer, err := devissuer.New(identityTestAudience)
	if err != nil {
		t.Fatalf("devissuer.New: %v", err)
	}
	issuerHTTP := httptest.NewServer(devissuer.NewServer(issuer, 15*time.Minute, time.Minute))
	t.Cleanup(issuerHTTP.Close)

	supplierTS, supplierMock := startMockUpstream(t, "suppliermaster")

	upstreams := []mcp.Upstream{mcp.NewHTTPUpstream("suppliermaster", supplierTS.URL, "")}
	registry, err := mcp.NewStaticRegistry(context.Background(), upstreams)
	if err != nil {
		t.Fatalf("NewStaticRegistry: %v", err)
	}

	pdp := policy.NewOPADecider()
	if err := pdp.Load(mustAbs(t, "../../examples/policies")); err != nil {
		t.Fatalf("Load policy: %v", err)
	}

	auditPath := filepath.Join(t.TempDir(), "audit.log")
	auditStore, err := audit.NewFileStore(auditPath)
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}

	resolver := identity.NewJWTResolver(issuer.PublicKeySet(), identityTestAudience)
	broker := identity.NewHTTPCredentialBroker(issuerHTTP.URL + "/token-exchange")
	agentReg, err := agentregistry.New([]agentregistry.Entry{{AgentID: "agent:finance-bot"}})
	if err != nil {
		t.Fatalf("agentregistry.New: %v", err)
	}
	gw := gateway.New(resolver, pdp, auditStore, registry,
		map[string]types.Provenance{"suppliermaster": {Source: "suppliermaster", Trusted: true}},
		0, broker, registerAllTools(t, registry), agentReg)
	gwServer := mcp.NewServer(":0", "/mcp", gw, "identity-test")

	ts := httptest.NewServer(gwServer.Handler())
	t.Cleanup(ts.Close)

	return identityGatewaySetup{endpoint: ts.URL + "/mcp", issuer: issuer, mock: supplierMock, auditPath: auditPath}
}

// TestValidSITAllowsCallAndRecordsVerifiedPrincipal covers three of M1's
// "Done when" bullets at once: a call with a valid SIT is allowed, the
// resulting Action Record's principal and delegation chain are exactly
// what the (verified) token asserted, and the upstream credential
// presented is provably not the agent's own SIT.
func TestValidSITAllowsCallAndRecordsVerifiedPrincipal(t *testing.T) {
	setup := newIdentityGateway(t)

	sit, err := setup.issuer.IssueSIT("agent:finance-bot", []string{"user:marcus@corp.com", "agent:finance-bot"}, nil, "", 15*time.Minute)
	if err != nil {
		t.Fatalf("IssueSIT: %v", err)
	}

	// examples/policies/scope.rego scopes agent:finance-bot to
	// supplier.get_account among others.
	resp := resultOf(t, callTool(t, setup.endpoint, sit, "supplier.get_account", map[string]any{"supplier_id": "S1"}))
	if isErr, _ := resp["isError"].(bool); isErr {
		t.Fatalf("expected the call to be allowed: %v", resp)
	}

	// The Action Record has a verified principal (the JWT signature
	// verification that already happened is what makes this "verified,"
	// not just "present") and the delegation chain the token asserted.
	records := readAuditRecords(t, setup.auditPath)
	if len(records) == 0 {
		t.Fatal("no audit record written for an allowed call")
	}
	rec := records[len(records)-1]
	if rec.Subject.ID != "agent:finance-bot" {
		t.Fatalf("record.Subject.ID = %q, want agent:finance-bot", rec.Subject.ID)
	}
	wantChain := []string{"user:marcus@corp.com", "agent:finance-bot"}
	if len(rec.Subject.DelegationChain) != 2 || rec.Subject.DelegationChain[0] != wantChain[0] || rec.Subject.DelegationChain[1] != wantChain[1] {
		t.Fatalf("record.Subject.DelegationChain = %v, want %v", rec.Subject.DelegationChain, wantChain)
	}

	// The credential the upstream actually received must be a per-call
	// token the broker minted — never the agent's own SIT — and that
	// token must itself be valid, scoped to this exact upstream and tool,
	// and short-lived (per the broker's exchangeTTL, not the SIT's much
	// longer TTL).
	calls := toolCalls(setup.mock.Calls())
	if len(calls) != 1 {
		t.Fatalf("upstream received %d tools/call(s), want 1", len(calls))
	}
	presented := calls[0].AuthHeader
	if presented == "" || presented == "Bearer "+sit {
		t.Fatalf("upstream received the agent's own SIT instead of a minted credential: %q", presented)
	}
	downstreamToken := presented[len("Bearer "):]
	tok, err := jwt.Parse([]byte(downstreamToken), jwt.WithKeySet(setup.issuer.PublicKeySet()), jwt.WithValidate(true), jwt.WithAudience("suppliermaster"))
	if err != nil {
		t.Fatalf("the credential presented upstream does not verify as a token scoped to this upstream: %v", err)
	}
	var tool string
	if err := tok.Get("tool", &tool); err != nil || tool != "supplier.get_account" {
		t.Fatalf("downstream token's tool claim = %q, err=%v, want supplier.get_account", tool, err)
	}
	if exp, ok := tok.Expiration(); !ok || time.Until(exp) > 2*time.Minute {
		t.Fatalf("downstream token expiration = %v, want short-lived (~1 minute), not the SIT's 15-minute lifetime", exp)
	}
}

// TestForgedSITIsDeniedAndLedgered is M1's "a call with a forged or
// expired token is denied and ledgered" — the forged half.
func TestForgedSITIsDeniedAndLedgered(t *testing.T) {
	setup := newIdentityGateway(t)

	attacker, err := devissuer.New(identityTestAudience) // a different issuer, different key
	if err != nil {
		t.Fatalf("devissuer.New: %v", err)
	}
	forged, err := attacker.IssueSIT("agent:finance-bot", nil, nil, "", 15*time.Minute)
	if err != nil {
		t.Fatalf("IssueSIT: %v", err)
	}

	resp := resultOf(t, callTool(t, setup.endpoint, forged, "supplier.get_account", map[string]any{"supplier_id": "S1"}))
	if isErr, _ := resp["isError"].(bool); !isErr {
		t.Fatal("a forged token was accepted")
	}
	if calls := toolCalls(setup.mock.Calls()); len(calls) != 0 {
		t.Fatalf("upstream was called despite a forged token, want 0 calls, got %d", len(calls))
	}

	records := readAuditRecords(t, setup.auditPath)
	if len(records) == 0 || records[len(records)-1].Outcome != types.RecordOutcomeDenied {
		t.Fatalf("forged-token attempt was not ledgered as a denial: %+v", records)
	}
}

// TestExpiredSITIsDeniedAndLedgered is the expired half of the same
// requirement.
func TestExpiredSITIsDeniedAndLedgered(t *testing.T) {
	setup := newIdentityGateway(t)

	expired, err := setup.issuer.IssueSIT("agent:finance-bot", nil, nil, "", -time.Minute) // already expired
	if err != nil {
		t.Fatalf("IssueSIT: %v", err)
	}

	resp := resultOf(t, callTool(t, setup.endpoint, expired, "supplier.get_account", map[string]any{"supplier_id": "S1"}))
	if isErr, _ := resp["isError"].(bool); !isErr {
		t.Fatal("an expired token was accepted")
	}
	if calls := toolCalls(setup.mock.Calls()); len(calls) != 0 {
		t.Fatalf("upstream was called despite an expired token, want 0 calls, got %d", len(calls))
	}

	records := readAuditRecords(t, setup.auditPath)
	if len(records) == 0 || records[len(records)-1].Outcome != types.RecordOutcomeDenied {
		t.Fatalf("expired-token attempt was not ledgered as a denial: %+v", records)
	}
}
