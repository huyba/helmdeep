// Copyright (c) 2026 Overarching AI LLC
// SPDX-License-Identifier: Apache-2.0

package identity

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// HTTPCredentialBroker implements CredentialBroker by calling an
// RFC-8693-shaped token-exchange endpoint over HTTP — in Phase 0, always
// internal/devissuer's /token-exchange (see
// docs/adr/0007-credential-broker-scope.md for why there's no real
// external authorization server to call instead).
type HTTPCredentialBroker struct {
	endpoint   string
	httpClient *http.Client
}

// NewHTTPCredentialBroker builds a broker that exchanges tokens at
// endpoint (e.g. "http://dev-issuer:8080/token-exchange").
func NewHTTPCredentialBroker(endpoint string) *HTTPCredentialBroker {
	return &HTTPCredentialBroker{endpoint: endpoint, httpClient: &http.Client{Timeout: 10 * time.Second}}
}

type exchangeResponse struct {
	AccessToken string `json:"access_token"`
	Error       string `json:"error"`
	ErrorDesc   string `json:"error_description"`
}

func (b *HTTPCredentialBroker) Exchange(ctx context.Context, subjectCredential, upstream, tool string) (string, error) {
	form := url.Values{
		"grant_type":    {"urn:ietf:params:oauth:grant-type:token-exchange"},
		"subject_token": {subjectCredential},
		"resource":      {upstream},
		"tool":          {tool},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("build token-exchange request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := b.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("call token-exchange endpoint: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var out exchangeResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("decode token-exchange response (HTTP %d): %w", resp.StatusCode, err)
	}
	if out.Error != "" {
		return "", fmt.Errorf("token exchange denied: %s: %s", out.Error, out.ErrorDesc)
	}
	if out.AccessToken == "" {
		return "", fmt.Errorf("token-exchange response had no access_token (HTTP %d)", resp.StatusCode)
	}
	return out.AccessToken, nil
}

var _ CredentialBroker = (*HTTPCredentialBroker)(nil)
