// Copyright (c) 2026 Overarching AI LLC
// SPDX-License-Identifier: Apache-2.0

package modelgw

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAnthropicProvider_SuccessReturnsResolvedModelAndUsage(t *testing.T) {
	var gotBody map[string]any
	var gotAPIKey, gotVersion string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		gotAPIKey = r.Header.Get("x-api-key")
		gotVersion = r.Header.Get("anthropic-version")
		_, _ = w.Write([]byte(`{
			"model": "claude-haiku-4-5-20251001",
			"content": [{"type": "text", "text": "OK"}],
			"usage": {"input_tokens": 20, "output_tokens": 3}
		}`))
	}))
	defer ts.Close()

	p := &AnthropicProvider{APIKey: "test-key", BaseURL: ts.URL}
	resp, err := p.Complete(context.Background(), "claude-haiku-4-5-20251001", Request{
		Messages: []Message{
			{Role: "system", Content: "Be terse."},
			{Role: "user", Content: "hi"},
		},
		MaxTokens: 5,
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Provider != "anthropic" || resp.Model != "claude-haiku-4-5-20251001" || resp.Content != "OK" {
		t.Fatalf("got %+v", resp)
	}
	if resp.Usage.PromptTokens != 20 || resp.Usage.CompletionTokens != 3 {
		t.Fatalf("Usage = %+v, want {20 3}", resp.Usage)
	}
	if gotAPIKey != "test-key" {
		t.Fatalf("x-api-key = %q, want test-key", gotAPIKey)
	}
	if gotVersion != AnthropicAPIVersion {
		t.Fatalf("anthropic-version = %q, want %q", gotVersion, AnthropicAPIVersion)
	}
	if gotBody["system"] != "Be terse." {
		t.Fatalf("system field = %v, want the system-role message pulled out of messages[]", gotBody["system"])
	}
	msgs, _ := gotBody["messages"].([]any)
	if len(msgs) != 1 {
		t.Fatalf("messages = %v, want exactly the one non-system message", msgs)
	}
}

func TestAnthropicProvider_DefaultsMaxTokensWhenUnset(t *testing.T) {
	var gotBody map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_, _ = w.Write([]byte(`{"model":"m","content":[{"type":"text","text":"x"}],"usage":{}}`))
	}))
	defer ts.Close()

	p := &AnthropicProvider{APIKey: "k", BaseURL: ts.URL}
	if _, err := p.Complete(context.Background(), "m", Request{Messages: []Message{{Role: "user", Content: "hi"}}}); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if gotBody["max_tokens"].(float64) <= 0 {
		t.Fatalf("max_tokens = %v, want a positive default (Anthropic requires this field)", gotBody["max_tokens"])
	}
}

func TestAnthropicProvider_APIErrorIsReturned(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error": {"message": "invalid x-api-key"}}`))
	}))
	defer ts.Close()

	p := &AnthropicProvider{APIKey: "bad", BaseURL: ts.URL}
	_, err := p.Complete(context.Background(), "m", Request{Messages: []Message{{Role: "user", Content: "hi"}}})
	if err == nil || !strings.Contains(err.Error(), "invalid x-api-key") {
		t.Fatalf("Complete: got %v, want an error mentioning the API's message", err)
	}
}
