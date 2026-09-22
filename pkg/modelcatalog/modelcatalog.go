// Copyright (c) 2026 Overarching AI LLC
// SPDX-License-Identifier: Apache-2.0

// Package modelcatalog is the Model Catalog (docs/02-architecture.md §2's
// Control Plane; docs/07-model-gateway.md §3's routing filter 2: "models
// approved in the tenant's Model Catalog (hard constraint)"): the
// governed list of provider+model pairs a deployment has actually
// approved for use, as distinct from whatever a pkg/modelgw.Route happens
// to be configured to call. Mirrors pkg/toolregistry's and
// pkg/agentregistry's shape and reasoning exactly — the same governance
// question ("is this identifier one an operator declared, or just one a
// config value asserts?") applied a third time, now to a model version
// rather than a tool or an agent.
//
// This is a deliberately narrow slice of doc 07 §3's full Model Catalog
// entry (approved status, version pinning rule, allowed data classes,
// residency, cost per token, quality tier per purpose, deprecation date,
// a link to an eval scorecard) — see docs/adr/0013-model-catalog.md for
// the scoping reasoning. In particular: no per-request data-class
// enforcement, because nothing in this repo classifies what data a
// request actually contains (no PII/content-classification pipeline
// exists — see docs/adr/0011-model-gateway.md's own list of gaps); doc
// 07's "models permitted for these data_classes" filter needs a real
// classification signal this milestone does not have, so AllowedDataClasses
// here is descriptive governance metadata only, exactly like
// pkg/toolregistry.Entry.DataClasses already is for tools.
package modelcatalog

import "fmt"

// Entry is one approved provider+model pair's catalog metadata — the
// narrow slice of doc 07 §3's full Model Catalog entry this milestone
// implements.
type Entry struct {
	// Provider matches pkg/modelgw.Provider.Name() (e.g. "azure-openai",
	// "anthropic") — the join key's first half.
	Provider string
	// Model matches the exact string a pkg/modelgw.Route pins (an Azure
	// deployment name, an Anthropic model id — whatever Provider.Complete
	// receives as its model parameter) — the join key's second half.
	Model string
	// Approved gates usability outright: doc 07 §3's "a model is not
	// usable until it has an eval scorecard for the purposes it is
	// approved for." No eval scorecard exists in this repo (Eval Service
	// is unimplemented — see docs/adr/0013-model-catalog.md), so Approved
	// is an operator's direct assertion, not derived from one.
	Approved bool
	// AllowedDataClasses names the data categories this model is approved
	// to process (doc 07 §3's "allowed data classes") — descriptive
	// governance metadata only; see the package doc comment for why
	// nothing compares a request's actual content against it yet.
	AllowedDataClasses []string
	// Residency names the approved processing region(s) (doc 07 §3) —
	// descriptive metadata; nothing cross-checks a provider's actual
	// processing region against it.
	Residency string
}

// key joins Provider and Model into the map key — a model id alone is not
// unique across providers (two providers could coincidentally use the
// same string), so both halves are always required together.
type key struct{ provider, model string }

// Registry is the config-backed Model Catalog. Safe for concurrent read
// access after construction; there is no runtime mutation, matching
// pkg/toolregistry.Registry and pkg/agentregistry.Registry.
type Registry struct {
	byKey map[key]Entry
}

// New builds a Registry from entries, rejecting an entry with a missing
// Provider or Model, or a duplicate (Provider, Model) pair, outright — a
// misconfigured catalog should fail loudly at startup, matching
// pkg/toolregistry.New's and pkg/agentregistry.New's own convention.
func New(entries []Entry) (*Registry, error) {
	byKey := make(map[key]Entry, len(entries))
	for _, e := range entries {
		if e.Provider == "" || e.Model == "" {
			return nil, fmt.Errorf("model catalog entry has an empty provider or model")
		}
		k := key{e.Provider, e.Model}
		if _, exists := byKey[k]; exists {
			return nil, fmt.Errorf("provider %q model %q is registered more than once", e.Provider, e.Model)
		}
		byKey[k] = e
	}
	return &Registry{byKey: byKey}, nil
}

// Lookup returns the catalog entry for (provider, model), or false if
// that exact pair has never been declared — the case pkg/modelgw.Gateway's
// own "unapproved provider+model is refused" rule exists for, enforced
// there, not by this package (Registry only answers "is this declared
// and approved," not "should this call be allowed" — matching
// pkg/toolregistry.Registry.Lookup's identical division of labor).
func (r *Registry) Lookup(provider, model string) (Entry, bool) {
	e, ok := r.byKey[key{provider, model}]
	return e, ok
}
