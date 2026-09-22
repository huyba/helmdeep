// Copyright (c) 2026 Overarching AI LLC
// SPDX-License-Identifier: Apache-2.0

package modelgw

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAzureOpenAIProvider_SuccessReturnsResolvedModelAndUsage(t *testing.T) {
	var gotPath, gotAPIKey string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path + "?" + r.URL.RawQuery
		gotAPIKey = r.Header.Get("api-key")
		_, _ = w.Write([]byte(`{
			"model": "gpt-4o-2024-11-20",
			"choices": [{"message": {"content": "OK"}}],
			"usage": {"prompt_tokens": 13, "completion_tokens": 1}
		}`))
	}))
	defer ts.Close()

	p := &AzureOpenAIProvider{Endpoint: ts.URL, APIKey: "test-key", APIVersion: "2024-10-21"}
	resp, err := p.Complete(context.Background(), "gpt-4o-chat", Request{
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Provider != "azure-openai" {
		t.Fatalf("Provider = %q, want azure-openai", resp.Provider)
	}
	// The resolved model (from the response body) must be reported even
	// though it differs from the deployment name ("gpt-4o-chat") sent —
	// that's the whole point of not trusting the request echoed back.
	if resp.Model != "gpt-4o-2024-11-20" {
		t.Fatalf("Model = %q, want the API's resolved version, not the deployment name", resp.Model)
	}
	if resp.Content != "OK" {
		t.Fatalf("Content = %q, want OK", resp.Content)
	}
	if resp.Usage.PromptTokens != 13 || resp.Usage.CompletionTokens != 1 {
		t.Fatalf("Usage = %+v, want {13 1}", resp.Usage)
	}
	if !strings.HasPrefix(gotPath, "/openai/deployments/gpt-4o-chat/chat/completions?api-version=2024-10-21") {
		t.Fatalf("request path = %q, want the deployment name in the URL", gotPath)
	}
	if gotAPIKey != "test-key" {
		t.Fatalf("api-key header = %q, want test-key", gotAPIKey)
	}
}

func TestAzureOpenAIProvider_APIErrorIsReturned(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error": {"message": "deployment 'nope' not found"}}`))
	}))
	defer ts.Close()

	p := &AzureOpenAIProvider{Endpoint: ts.URL, APIKey: "k", APIVersion: "2024-10-21"}
	_, err := p.Complete(context.Background(), "nope", Request{Messages: []Message{{Role: "user", Content: "hi"}}})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("Complete: got %v, want an error mentioning the API's message", err)
	}
}
