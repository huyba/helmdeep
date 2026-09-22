// Copyright (c) 2026 Overarching AI LLC
// SPDX-License-Identifier: Apache-2.0

package modelgw

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// AzureOpenAIProvider calls a real Azure OpenAI resource's Chat Completions
// API. In Azure's API, the URL path segment callers usually think of as
// "the model" is actually a *deployment name* the account owner chose when
// they deployed a model version (see `az cognitiveservices account
// deployment create`) — Complete's model parameter is that deployment
// name, and the resolved model version this provider reports back in
// Response.Model comes from the API's own response body, not from config,
// so a deployment silently repointed at a different underlying model
// version is still visible in the Action Record.
type AzureOpenAIProvider struct {
	// Endpoint is the resource's base URL, e.g.
	// "https://my-resource.openai.azure.com/" (trailing slash optional).
	Endpoint string
	// APIKey is the resource's key (az cognitiveservices account keys list).
	APIKey string
	// APIVersion is the Azure OpenAI REST API version, e.g. "2024-10-21".
	// Azure versions its *API surface* independently of model versions;
	// this is not the version-pinning doc 07 §3.1 means, which is about
	// the model itself (the deployment name / Response.Model).
	APIVersion string
	// HTTPClient is used for the request if non-nil; otherwise a client
	// with a 60s timeout is used. Exposed for tests.
	HTTPClient *http.Client
}

func (p *AzureOpenAIProvider) Name() string { return "azure-openai" }

type azureChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type azureChatRequest struct {
	Messages    []azureChatMessage `json:"messages"`
	MaxTokens   int                `json:"max_tokens,omitempty"`
	Temperature float64            `json:"temperature,omitempty"`
}

type azureChatResponse struct {
	Model   string `json:"model"`
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Complete calls deployment's chat/completions endpoint. deployment is the
// Azure deployment name (a Route's Model field), not a raw OpenAI model
// name — see the type's own doc comment.
func (p *AzureOpenAIProvider) Complete(ctx context.Context, deployment string, req Request) (Response, error) {
	messages := make([]azureChatMessage, len(req.Messages))
	for i, m := range req.Messages {
		messages[i] = azureChatMessage(m)
	}
	body, err := json.Marshal(azureChatRequest{Messages: messages, MaxTokens: req.MaxTokens, Temperature: req.Temperature})
	if err != nil {
		return Response{}, fmt.Errorf("azure-openai: marshal request: %w", err)
	}

	// Endpoint is operator config (a Provider is constructed once, at
	// startup, from cmd/helmdeep-gateway's config or a caller's own code);
	// deployment is a Route.Model value New already validated at
	// construction. Neither is agent- or request-supplied, so this is not
	// the caller-controlled-URL shape G704 exists to catch.
	url := fmt.Sprintf("%sopenai/deployments/%s/chat/completions?api-version=%s", ensureTrailingSlash(p.Endpoint), deployment, p.APIVersion)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body)) //nolint:gosec // G704: see comment above
	if err != nil {
		return Response{}, fmt.Errorf("azure-openai: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("api-key", p.APIKey)

	client := p.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	httpResp, err := client.Do(httpReq) //nolint:gosec // G704: see comment above
	if err != nil {
		return Response{}, fmt.Errorf("azure-openai: request failed: %w", err)
	}
	defer func() { _ = httpResp.Body.Close() }()

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return Response{}, fmt.Errorf("azure-openai: read response: %w", err)
	}
	var parsed azureChatResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return Response{}, fmt.Errorf("azure-openai: malformed response (HTTP %d): %w", httpResp.StatusCode, err)
	}
	if parsed.Error != nil {
		return Response{}, fmt.Errorf("azure-openai: %s (HTTP %d)", parsed.Error.Message, httpResp.StatusCode)
	}
	if httpResp.StatusCode != http.StatusOK {
		return Response{}, fmt.Errorf("azure-openai: HTTP %d: %s", httpResp.StatusCode, string(respBody))
	}
	if len(parsed.Choices) == 0 {
		return Response{}, fmt.Errorf("azure-openai: response had no choices")
	}

	return Response{
		Provider: p.Name(),
		Model:    parsed.Model, // the API's own resolved version, not the deployment name we sent
		Content:  parsed.Choices[0].Message.Content,
		Usage: Usage{
			PromptTokens:     parsed.Usage.PromptTokens,
			CompletionTokens: parsed.Usage.CompletionTokens,
		},
	}, nil
}

func ensureTrailingSlash(s string) string {
	if len(s) > 0 && s[len(s)-1] != '/' {
		return s + "/"
	}
	return s
}

var _ Provider = (*AzureOpenAIProvider)(nil)
