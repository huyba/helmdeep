// Copyright (c) 2026 Overarching AI LLC
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"testing"

	"github.com/huyba/helmdeep/pkg/modelcatalog"
	"github.com/huyba/helmdeep/pkg/modelgw"
	"github.com/huyba/helmdeep/pkg/policy"
	"github.com/huyba/helmdeep/pkg/types"
)

type stubModelProvider struct {
	name string
	resp modelgw.Response
}

func (p *stubModelProvider) Name() string { return p.name }
func (p *stubModelProvider) Complete(context.Context, string, modelgw.Request) (modelgw.Response, error) {
	resp := p.resp
	resp.Provider = p.name
	return resp, nil
}

// TestToolCallsAndModelCallsShareOneVerifiableLedger is Milestone M5's
// central claim, proven directly rather than assumed: a Tool Gateway call
// and a Model Gateway call, using two entirely separate Gateway types with
// no shared code path between them beyond the interfaces both were handed,
// chain into the exact same hash-chained file and `Store.Verify` accepts
// the result end to end. If the two gateways' record-writing ever
// diverged in a way that broke the shared chain (a field order change, a
// different Append discipline), this test — not a review of either
// package in isolation — is what would catch it.
func TestToolCallsAndModelCallsShareOneVerifiableLedger(t *testing.T) {
	endpoint, auditStore, _ := newTestGateway(t)

	// One real Tool Gateway call, over real HTTP, through examples/policies/.
	toolResp := resultOf(t, callTool(t, endpoint, supportToken, "kb.search", map[string]any{"query": "x"}))
	if isErr, _ := toolResp["isError"].(bool); isErr {
		t.Fatalf("tool call unexpectedly denied: %v", toolResp)
	}

	// One Model Gateway call, writing to the SAME *audit.FileStore instance
	// newTestGateway already built for the Tool Gateway above — a stub
	// Provider stands in for a real model API so this test needs no
	// credentials and no network access; pkg/modelgw's own tests already
	// prove AzureOpenAIProvider/AnthropicProvider work against the real
	// APIs (see docs/adr/0011-model-gateway.md).
	// examples/policies/ only knows about Tool Gateway tool names, not the
	// model_call Action this test also needs to exercise — a minimal
	// allow-everything bundle keeps this test about the shared ledger, not
	// about scope policy (that's examples/policies' own, already-tested job).
	policyDir := t.TempDir()
	writeFile(t, policyDir+"/policy.rego", `package helmdeep.authz

decision := {"outcome": "allow", "policy_id": "test.allow"}
`)
	pdp := policy.NewOPADecider()
	if err := pdp.Load(policyDir); err != nil {
		t.Fatalf("Load policy: %v", err)
	}
	provider := &stubModelProvider{name: "stub", resp: modelgw.Response{Model: "stub-v1", Content: "ok"}}
	catalog, err := modelcatalog.New([]modelcatalog.Entry{{Provider: "stub", Model: "stub-v1", Approved: true}})
	if err != nil {
		t.Fatalf("modelcatalog.New: %v", err)
	}
	gw, err := modelgw.New(
		map[string]modelgw.Provider{"stub": provider},
		map[string]modelgw.Route{"reasoning": {Provider: "stub", Model: "stub-v1"}},
		pdp, auditStore, catalog,
	)
	if err != nil {
		t.Fatalf("modelgw.New: %v", err)
	}
	subject := types.Subject{ID: "agent:support-bot", Kind: types.SubjectKindAgent}
	if _, err := gw.Complete(context.Background(), subject, modelgw.Request{
		Purpose:  "reasoning",
		Messages: []modelgw.Message{{Role: "user", Content: "hi"}},
	}); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	// The whole point: one chain, containing both kinds of records,
	// verifies as a single, unbroken ledger.
	if err := auditStore.Verify(context.Background()); err != nil {
		t.Fatalf("Verify: chain mixing tool_call and model_call records did not verify: %v", err)
	}
}
