// Copyright (c) 2026 Overarching AI LLC
// SPDX-License-Identifier: Apache-2.0

package gateway

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/huyba/helmdeep/pkg/identity"
	"github.com/huyba/helmdeep/pkg/types"
)

// StaticTokenResolver is a bootstrap identity.Resolver backed by a fixed
// token→identity map from config. It exists so the Tool Gateway is usable
// before the real identity & credential exchange component (pkg/identity)
// has an implementation — see ARCHITECTURE.md's "Identity & credential
// exchange" section for why this lives here and not in pkg/identity.
//
// DEVELOPMENT AND TEST ONLY. This resolver trusts a static, unrotating
// bearer token exactly as much as the config file trusts it — there is no
// expiry, no revocation, no cryptographic binding to a workload. It must
// never run in production. NewStaticTokenResolver logs a warning every time
// one is constructed for exactly this reason.
type StaticTokenResolver struct {
	tokens map[string]types.Subject
}

// StaticIdentity is one entry in a StaticTokenResolver's token map,
// matching the shape of this gateway's identity config section.
type StaticIdentity struct {
	ID         string
	Kind       types.SubjectKind
	TrustLevel int
}

// NewStaticTokenResolver builds a resolver from a fixed token→identity map.
// It logs a startup warning: see the type's doc comment.
func NewStaticTokenResolver(tokens map[string]StaticIdentity) *StaticTokenResolver {
	slog.Warn("static token identity resolver active: development/test only, do not run in production")

	resolved := make(map[string]types.Subject, len(tokens))
	for token, id := range tokens {
		resolved[token] = types.Subject{
			ID:         id.ID,
			Kind:       id.Kind,
			TrustLevel: id.TrustLevel,
		}
	}
	return &StaticTokenResolver{tokens: resolved}
}

// Resolve looks credential up in the static map. An unrecognized or empty
// credential is an error, not a zero-value Subject — the caller must not be
// able to mistake "we don't know who this is" for "this is an anonymous but
// valid subject."
func (r *StaticTokenResolver) Resolve(ctx context.Context, credential string) (types.Subject, error) {
	if credential == "" {
		return types.Subject{}, fmt.Errorf("no credential presented")
	}
	subj, ok := r.tokens[credential]
	if !ok {
		return types.Subject{}, fmt.Errorf("credential not recognized")
	}
	return subj, nil
}

var _ identity.Resolver = (*StaticTokenResolver)(nil)
