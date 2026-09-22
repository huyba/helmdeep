// Copyright (c) 2026 Overarching AI LLC
// SPDX-License-Identifier: Apache-2.0

package modelgw

import (
	"context"
	"os"
	"testing"
)

// TestAzureOpenAIProvider_RealAPI and TestAnthropicProvider_RealAPI call
// the actual providers — skipped unless the relevant environment
// variables are set, so `go test ./...` never needs, and CI never spends,
// real API credentials. Run locally with real credentials to verify this
// package still matches each provider's real API, independent of the
// fake-server unit tests above (see docs/adr/0011-model-gateway.md for
// why both exist).
func TestAzureOpenAIProvider_RealAPI(t *testing.T) {
	endpoint := os.Getenv("AZURE_OPENAI_ENDPOINT")
	key := os.Getenv("AZURE_OPENAI_KEY")
	deployment := os.Getenv("AZURE_OPENAI_DEPLOYMENT")
	if endpoint == "" || key == "" || deployment == "" {
		t.Skip("set AZURE_OPENAI_ENDPOINT, AZURE_OPENAI_KEY, and AZURE_OPENAI_DEPLOYMENT to run this against the real API")
	}

	p := &AzureOpenAIProvider{Endpoint: endpoint, APIKey: key, APIVersion: "2024-10-21"}
	resp, err := p.Complete(context.Background(), deployment, Request{
		Messages:  []Message{{Role: "user", Content: "Reply with exactly the word: OK"}},
		MaxTokens: 10,
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Model == "" {
		t.Fatal("real API response did not report a resolved model version")
	}
	if resp.Content == "" {
		t.Fatal("real API response had no content")
	}
	t.Logf("azure-openai real call: model=%s content=%q usage=%+v", resp.Model, resp.Content, resp.Usage)
}

func TestAnthropicProvider_RealAPI(t *testing.T) {
	key := os.Getenv("ANTHROPIC_API_KEY")
	model := os.Getenv("ANTHROPIC_MODEL")
	if key == "" || model == "" {
		t.Skip("set ANTHROPIC_API_KEY and ANTHROPIC_MODEL to run this against the real API")
	}

	p := &AnthropicProvider{APIKey: key}
	resp, err := p.Complete(context.Background(), model, Request{
		Messages:  []Message{{Role: "user", Content: "Reply with exactly the word: OK"}},
		MaxTokens: 10,
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Model == "" {
		t.Fatal("real API response did not report a resolved model version")
	}
	if resp.Content == "" {
		t.Fatal("real API response had no content")
	}
	t.Logf("anthropic real call: model=%s content=%q usage=%+v", resp.Model, resp.Content, resp.Usage)
}
