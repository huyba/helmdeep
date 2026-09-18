// Copyright (c) 2026 Overarching AI LLC
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"os"

	"sigs.k8s.io/yaml"

	"github.com/huyba/helmdeep/internal/gateway"
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
	} `json:"policy"`

	Audit struct {
		Path string `json:"path"`
	} `json:"audit"`

	Identity struct {
		StaticTokens map[string]struct {
			ID         string `json:"id"`
			Kind       string `json:"kind"`
			TrustLevel int    `json:"trust_level"`
		} `json:"static_tokens"`
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
	return cfg, nil
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
