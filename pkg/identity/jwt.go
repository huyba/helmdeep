// Copyright (c) 2026 Overarching AI LLC
// SPDX-License-Identifier: Apache-2.0

package identity

import (
	"context"
	"fmt"

	"github.com/lestrrat-go/jwx/v3/jwk"
	"github.com/lestrrat-go/jwx/v3/jwt"

	"github.com/huyba/helmdeep/pkg/types"
)

// JWTResolver verifies a Session Identity Token (SIT, docs/04-identity-authz.md
// §1.1) against a public key set and turns it into a types.Subject. It
// never holds a private key — only internal/devissuer (or, later, a real
// SPIRE-backed issuer) does.
//
// Claims read: `agent` (required — the agent definition ID),
// `delegation_chain` (optional — an ordered list of principal identifiers,
// outermost first), and `scope` (optional — the scopes this token grants,
// types.Subject.Scopes; docs/04-identity-authz.md §1.1). The signature and
// `exp`/`aud` are verified by the underlying jwt.Parse call; a Subject is
// only ever returned for a token whose signature and standard claims both
// check out.
type JWTResolver struct {
	keySet   jwk.Set
	audience string
}

// NewJWTResolver builds a JWTResolver that accepts tokens signed by a key
// in keySet, addressed to aud.
func NewJWTResolver(keySet jwk.Set, aud string) *JWTResolver {
	return &JWTResolver{keySet: keySet, audience: aud}
}

func (r *JWTResolver) Resolve(ctx context.Context, credential string) (types.Subject, error) {
	if credential == "" {
		return types.Subject{}, fmt.Errorf("no credential presented")
	}

	tok, err := jwt.Parse([]byte(credential),
		jwt.WithKeySet(r.keySet),
		jwt.WithValidate(true),
		jwt.WithAudience(r.audience),
		jwt.WithContext(ctx),
		jwt.WithTypedClaim("delegation_chain", []string{}),
		jwt.WithTypedClaim("scope", []string{}),
	)
	if err != nil {
		return types.Subject{}, fmt.Errorf("credential invalid: %w", err)
	}

	var agentID string
	if err := tok.Get("agent", &agentID); err != nil || agentID == "" {
		return types.Subject{}, fmt.Errorf("credential missing required 'agent' claim")
	}

	var chain []string
	_ = tok.Get("delegation_chain", &chain) // optional; absence isn't an error

	var scope []string
	_ = tok.Get("scope", &scope) // optional; absence isn't an error

	return types.Subject{
		ID:              agentID,
		Kind:            types.SubjectKindAgent,
		DelegationChain: chain,
		Scopes:          scope,
	}, nil
}

var _ Resolver = (*JWTResolver)(nil)
