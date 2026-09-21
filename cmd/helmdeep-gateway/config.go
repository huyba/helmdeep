// Copyright (c) 2026 Overarching AI LLC
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"os"
	"time"

	"sigs.k8s.io/yaml"

	"github.com/huyba/helmdeep/internal/gateway"
	"github.com/huyba/helmdeep/pkg/toolregistry"
	"github.com/huyba/helmdeep/pkg/types"
)

// config is this binary's on-disk configuration. It's parsed with
// sigs.k8s.io/yaml, which converts YAML to JSON and decodes with
// encoding/json — so the json tags below are the actual config schema, and
// the same tags this repo already uses everywhere else. This adds no new
// third-party dependency: sigs.k8s.io/yaml is already compiled into this
// binary because OPA's Rego builtins (yaml.marshal/yaml.unmarshal) depend
// on it — see docs/adr/0002-policy-engine-choice.md.
type config struct {
	Listen string `json:"listen"`
	Path   string `json:"path"`

	Policy struct {
		Path string `json:"path"`
		// DecisionTimeout bounds each PDP decision, e.g. "200ms". Empty
		// means no bound. See internal/gateway.Gateway's decisionTimeout
		// field and docs/adr/0003-fail-closed-behavior.md — a timeout is
		// treated exactly like any other Decide error: deny.
		DecisionTimeout string `json:"decision_timeout"`
	} `json:"policy"`

	Audit struct {
		Path string `json:"path"`
	} `json:"audit"`

	Identity struct {
		// StaticTokens is the pre-Milestone-M1 dev/test identity source: a
		// fixed token→identity map, no verification beyond map lookup. It
		// is refused at startup unless the -dev-insecure flag is passed —
		// see main.go's runServe. Prefer JWT below for anything else.
		StaticTokens map[string]struct {
			ID         string   `json:"id"`
			Kind       string   `json:"kind"`
			TrustLevel int      `json:"trust_level"`
			Scopes     []string `json:"scopes"`
		} `json:"static_tokens"`

		// JWT configures the real identity path: Session Identity Tokens
		// (docs/04-identity-authz.md §1.1) verified against a JWKS. In
		// Phase 0 the only issuer that exists is internal/devissuer; see
		// docs/adr/0007-credential-broker-scope.md.
		JWT *struct {
			Audience string `json:"audience"` // must match the issuer's audience exactly
			JWKSURL  string `json:"jwks_url"` // fetched once at startup — see runServe
			// ExchangeURL, if set, enables the Credential Broker: the
			// gateway calls this RFC-8693-shaped endpoint to mint a
			// per-call scoped credential for each allowed tool call
			// (docs/04-identity-authz.md §3) instead of using an
			// upstream's static bearer_token_env. Leave empty to keep
			// upstreams on their static credentials while still verifying
			// callers via JWT.
			ExchangeURL string `json:"exchange_url"`
		} `json:"jwt"`
	} `json:"identity"`

	Upstreams []struct {
		Name           string `json:"name"`
		URL            string `json:"url"`
		BearerTokenEnv string `json:"bearer_token_env"`
		Provenance     struct {
			Source  string `json:"source"`
			Trusted bool   `json:"trusted"`
		} `json:"provenance"`
	} `json:"upstreams"`

	// Tools is the Tool Registry (docs/05-tool-gateway.md §1,
	// pkg/toolregistry, Milestone M2): every tool this deployment declares.
	// A tool absent from this list is refused outright, regardless of
	// whether an upstream exposes it — see internal/gateway.Gateway's
	// toolReg field and docs/adr/0008-tool-registry.md.
	Tools []struct {
		ID          string   `json:"id"`
		Upstream    string   `json:"upstream"`
		Risk        string   `json:"risk"`
		DataClasses []string `json:"data_classes"`
		Scopes      []string `json:"scopes"`
	} `json:"tools"`
}

func loadConfig(path string) (config, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- path is an operator-supplied CLI flag, not attacker-controlled input
	if err != nil {
		return config{}, fmt.Errorf("read config %s: %w", path, err)
	}

	cfg := config{
		Listen: ":8443",
		Path:   "/mcp",
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return config{}, fmt.Errorf("parse config %s: %w", path, err)
	}

	if cfg.Path == "/healthz" || cfg.Path == "/livez" {
		return config{}, fmt.Errorf("config %s: path %q is reserved for the health endpoints", path, cfg.Path)
	}
	if cfg.Policy.Path == "" {
		return config{}, fmt.Errorf("config %s: policy.path is required", path)
	}
	if cfg.Audit.Path == "" {
		return config{}, fmt.Errorf("config %s: audit.path is required", path)
	}
	for _, u := range cfg.Upstreams {
		if u.Name == "" || u.URL == "" {
			return config{}, fmt.Errorf("config %s: every upstream needs a name and url", path)
		}
	}
	if _, err := cfg.decisionTimeout(); err != nil {
		return config{}, fmt.Errorf("config %s: policy.decision_timeout: %w", path, err)
	}
	if _, err := cfg.toolRegistry(); err != nil {
		return config{}, fmt.Errorf("config %s: tools: %w", path, err)
	}
	hasStatic := len(cfg.Identity.StaticTokens) > 0
	hasJWT := cfg.Identity.JWT != nil
	switch {
	case hasStatic && hasJWT:
		return config{}, fmt.Errorf("config %s: identity.static_tokens and identity.jwt are mutually exclusive", path)
	case !hasStatic && !hasJWT:
		return config{}, fmt.Errorf("config %s: identity.static_tokens or identity.jwt is required", path)
	case hasJWT && cfg.Identity.JWT.Audience == "":
		return config{}, fmt.Errorf("config %s: identity.jwt.audience is required", path)
	case hasJWT && cfg.Identity.JWT.JWKSURL == "":
		return config{}, fmt.Errorf("config %s: identity.jwt.jwks_url is required", path)
	}
	return cfg, nil
}

// decisionTimeout parses Policy.DecisionTimeout, returning 0 (no timeout)
// if it's unset.
func (c config) decisionTimeout() (time.Duration, error) {
	if c.Policy.DecisionTimeout == "" {
		return 0, nil
	}
	return time.ParseDuration(c.Policy.DecisionTimeout)
}

// staticIdentities converts the config's identity.static_tokens section into
// the map gateway.NewStaticTokenResolver expects.
func (c config) staticIdentities() map[string]gateway.StaticIdentity {
	out := make(map[string]gateway.StaticIdentity, len(c.Identity.StaticTokens))
	for token, id := range c.Identity.StaticTokens {
		out[token] = gateway.StaticIdentity{
			ID:         id.ID,
			Kind:       types.SubjectKind(id.Kind),
			TrustLevel: id.TrustLevel,
			Scopes:     id.Scopes,
		}
	}
	return out
}

// upstreamProvenance converts the config's per-upstream provenance section
// into the map gateway.New expects, keyed by upstream name.
func (c config) upstreamProvenance() map[string]types.Provenance {
	out := make(map[string]types.Provenance, len(c.Upstreams))
	for _, u := range c.Upstreams {
		out[u.Name] = types.Provenance{Source: u.Provenance.Source, Trusted: u.Provenance.Trusted}
	}
	return out
}

// toolRegistry builds the Tool Registry gateway.New requires from the
// config's tools section. An empty (but non-nil) registry is a valid
// config — it just refuses every tool call, per
// docs/05-tool-gateway.md §1's fail-closed default.
func (c config) toolRegistry() (*toolregistry.Registry, error) {
	entries := make([]toolregistry.Entry, len(c.Tools))
	for i, t := range c.Tools {
		entries[i] = toolregistry.Entry{
			ToolID:      t.ID,
			Upstream:    t.Upstream,
			Risk:        toolregistry.RiskRating(t.Risk),
			DataClasses: t.DataClasses,
			Scopes:      t.Scopes,
		}
	}
	return toolregistry.New(entries)
}
