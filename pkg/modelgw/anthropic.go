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

// DefaultAnthropicBaseURL is Anthropic's public API endpoint.
const DefaultAnthropicBaseURL = "https://api.anthropic.com"

// AnthropicAPIVersion is the Anthropic Messages API version this provider
// speaks (the anthropic-version header) — pinned for the same reason
// pkg/mcp.ProtocolVersionCurrent is pinned rather than left to float.
const AnthropicAPIVersion = "2023-06-01"

// AnthropicProvider calls the real Anthropic Messages API. Unlike Azure
// OpenAI's deployment indirection, Anthropic's model parameter in a
// request is already the specific, dated model id (e.g.
// "claude-haiku-4-5-20251001") — Complete's model parameter (a Route's
// Model field) is passed straight through, and Response.Model still comes
// from the API's own response body rather than being assumed equal to
// what was requested, for the same reason AzureOpenAIProvider does: the
// Action Record should reflect what the provider says it did, not what
// was asked for.
type AnthropicProvider struct {
	// APIKey is an Anthropic API key (console.anthropic.com -> API Keys).
	APIKey string
	// WorkspaceID is required only for an API key that isn't scoped to a
	// single workspace (an org-level key) — the real API rejects such a
	// key with HTTP 400 ("This API key is not scoped to a workspace...")
	// unless the anthropic-workspace-id header is also sent. Leave empty
	// for a normal, workspace-scoped key, which needs no such header.
	WorkspaceID string
	// BaseURL defaults to DefaultAnthropicBaseURL if empty.
	BaseURL string
	// HTTPClient is used for the request if non-nil; otherwise a client
	// with a 60s timeout is used. Exposed for tests.
	HTTPClient *http.Client
}

func (p *AnthropicProvider) Name() string { return "anthropic" }

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicRequest struct {
	Model       string             `json:"model"`
	System      string             `json:"system,omitempty"`
	Messages    []anthropicMessage `json:"messages"`
	MaxTokens   int                `json:"max_tokens"`
	Temperature float64            `json:"temperature,omitempty"`
}

type anthropicResponse struct {
	Model   string `json:"model"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Complete calls POST /v1/messages. The Messages API takes "system" as its
// own top-level field rather than a message with role "system" — any such
// messages in req.Messages are concatenated into it (joined with a blank
// line if there is more than one) and excluded from the messages array;
// every other message passes through with its role unchanged.
func (p *AnthropicProvider) Complete(ctx context.Context, model string, req Request) (Response, error) {
	var system string
	messages := make([]anthropicMessage, 0, len(req.Messages))
	for _, m := range req.Messages {
		if m.Role == "system" {
			if system != "" {
				system += "\n\n"
			}
			system += m.Content
			continue
		}
		messages = append(messages, anthropicMessage(m))
	}

	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		// Anthropic requires max_tokens; unlike Azure OpenAI it has no
		// server-side default to fall back on if the caller left it unset.
		maxTokens = 1024
	}

	body, err := json.Marshal(anthropicRequest{
		Model:       model,
		System:      system,
		Messages:    messages,
		MaxTokens:   maxTokens,
		Temperature: req.Temperature,
	})
	if err != nil {
		return Response{}, fmt.Errorf("anthropic: marshal request: %w", err)
	}

	baseURL := p.BaseURL
	if baseURL == "" {
		baseURL = DefaultAnthropicBaseURL
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, ensureTrailingSlash(baseURL)+"v1/messages", bytes.NewReader(body))
	if err != nil {
		return Response{}, fmt.Errorf("anthropic: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", p.APIKey)
	httpReq.Header.Set("anthropic-version", AnthropicAPIVersion)
	if p.WorkspaceID != "" {
		httpReq.Header.Set("anthropic-workspace-id", p.WorkspaceID)
	}

	client := p.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	httpResp, err := client.Do(httpReq)
	if err != nil {
		return Response{}, fmt.Errorf("anthropic: request failed: %w", err)
	}
	defer func() { _ = httpResp.Body.Close() }()

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return Response{}, fmt.Errorf("anthropic: read response: %w", err)
	}
	var parsed anthropicResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return Response{}, fmt.Errorf("anthropic: malformed response (HTTP %d): %w", httpResp.StatusCode, err)
	}
	if parsed.Error != nil {
		return Response{}, fmt.Errorf("anthropic: %s (HTTP %d)", parsed.Error.Message, httpResp.StatusCode)
	}
	if httpResp.StatusCode != http.StatusOK {
		return Response{}, fmt.Errorf("anthropic: HTTP %d: %s", httpResp.StatusCode, string(respBody))
	}

	var text string
	for _, c := range parsed.Content {
		if c.Type == "text" {
			text += c.Text
		}
	}

	return Response{
		Provider: p.Name(),
		Model:    parsed.Model,
		Content:  text,
		Usage: Usage{
			PromptTokens:     parsed.Usage.InputTokens,
			CompletionTokens: parsed.Usage.OutputTokens,
		},
	}, nil
}

var _ Provider = (*AnthropicProvider)(nil)
