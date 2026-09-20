// Copyright (c) 2026 Overarching AI LLC
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"testing"
)

// TestCheckDevInsecureGate is the single highest-value test in this
// package: it's the whole enforcement of "static token only works with
// -dev-insecure" (Milestone M1's exit criterion), and it must not
// silently regress to "static tokens always work" if this function's
// logic is ever touched.
func TestCheckDevInsecureGate(t *testing.T) {
	staticConfigured := config{}
	staticConfigured.Identity.StaticTokens = map[string]struct {
		ID         string `json:"id"`
		Kind       string `json:"kind"`
		TrustLevel int    `json:"trust_level"`
	}{"tok": {ID: "agent:test"}}

	tests := []struct {
		name        string
		cfg         config
		devInsecure bool
		wantErr     bool
	}{
		{"static tokens without -dev-insecure is refused", staticConfigured, false, true},
		{"static tokens with -dev-insecure is allowed", staticConfigured, true, false},
		{"no static tokens configured: flag value doesn't matter", config{}, false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkDevInsecureGate(tt.cfg, tt.devInsecure)
			if (err != nil) != tt.wantErr {
				t.Fatalf("checkDevInsecureGate(devInsecure=%v) error = %v, wantErr %v", tt.devInsecure, err, tt.wantErr)
			}
		})
	}
}

func TestLoadConfig_IdentitySourceValidation(t *testing.T) {
	writeTestConfig := func(t *testing.T, body string) string {
		t.Helper()
		dir := t.TempDir()
		path := dir + "/config.yaml"
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatalf("write config: %v", err)
		}
		return path
	}

	base := `
policy:
  path: /tmp/policies
audit:
  path: /tmp/audit.log
`

	tests := []struct {
		name       string
		identity   string
		wantErrSub string // empty means no error expected
	}{
		{
			name:       "neither static_tokens nor jwt configured",
			identity:   "identity: {}\n",
			wantErrSub: "identity.static_tokens or identity.jwt is required",
		},
		{
			name: "both static_tokens and jwt configured",
			identity: `identity:
  static_tokens:
    tok:
      id: agent:test
  jwt:
    audience: helmdeep-gateways
    jwks_url: http://issuer/.well-known/jwks.json
`,
			wantErrSub: "mutually exclusive",
		},
		{
			name: "jwt configured without audience",
			identity: `identity:
  jwt:
    jwks_url: http://issuer/.well-known/jwks.json
`,
			wantErrSub: "identity.jwt.audience is required",
		},
		{
			name: "static_tokens alone is valid at the config-parsing level",
			identity: `identity:
  static_tokens:
    tok:
      id: agent:test
`,
			wantErrSub: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeTestConfig(t, base+tt.identity)
			_, err := loadConfig(path)
			if tt.wantErrSub == "" {
				if err != nil {
					t.Fatalf("loadConfig: unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("loadConfig: expected an error containing %q, got nil", tt.wantErrSub)
			}
		})
	}
}
