// Copyright (c) 2026 Overarching AI LLC
// SPDX-License-Identifier: Apache-2.0

// Package devissuer is Phase 0's dev-only Session Identity Token issuer and
// credential broker, standing in for the real Enterprise IdP integration
// and OAuth-token-exchange authorization server that docs/04-identity-authz.md
// describes (§1.1, §3, §6) — neither exists yet. It plays both roles at
// once for the same reason there's no upstream in this repo that
// implements RFC 8693 itself: see
// docs/adr/0007-credential-broker-scope.md for why this is self-contained
// rather than talking to a real external AS.
//
// This is dev/test infrastructure, not the product — like
// internal/mockupstream, it's meant to be run in-process in tests and
// wrapped by a thin cmd/ binary later (that binary is Milestone M4's
// "one-command emulator," not this milestone's).
package devissuer

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jwk"
	"github.com/lestrrat-go/jwx/v3/jwt"
)

// KeyID is the (single, dev-only) signing key's kid. A real issuer would
// rotate keys and publish several; Phase 0 has exactly one, generated
// fresh every process start.
const KeyID = "dev-1"

// Issuer mints and verifies Session Identity Tokens (SITs) and performs
// the token-exchange half of the Credential Broker (doc 04 §3). It holds
// the only private key in the system; everything else — the Gateway
// included — only ever sees the public half.
//
// Claims are deliberately a minimal subset of doc 04 §1.1's full SIT
// shape: `sub`, `agent`, `delegation_chain`, `aud`, `iat`, `exp`. The
// doc's richer fields (`agent_version`, `scope`, `constraints` with
// `data_classes`/`row_filter`/`trust_level`/`residency`) are not included
// because nothing in this repo consumes them yet — see
// docs/adr/0007-credential-broker-scope.md, following the same
// don't-add-fields-nothing-produces-behavior-for principle as
// docs/adr/0004-action-record-format.md.
type Issuer struct {
	private  jwk.Key
	public   jwk.Set
	audience string
	now      func() time.Time
}

// New generates a fresh ES256 keypair (dev-only: not persisted, not
// rotated, gone on process restart) and returns an Issuer that mints SITs
// audience-restricted to aud — matching doc 04 §1.1's "audience-restricted
// to our own gateways, worthless outside them."
func New(aud string) (*Issuer, error) {
	raw, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate signing key: %w", err)
	}

	priv, err := jwk.Import(raw)
	if err != nil {
		return nil, fmt.Errorf("import signing key: %w", err)
	}
	if err := priv.Set(jwk.KeyIDKey, KeyID); err != nil {
		return nil, fmt.Errorf("set key id: %w", err)
	}
	if err := priv.Set(jwk.AlgorithmKey, jwa.ES256()); err != nil {
		return nil, fmt.Errorf("set key algorithm: %w", err)
	}

	pub, err := jwk.PublicKeyOf(priv)
	if err != nil {
		return nil, fmt.Errorf("derive public key: %w", err)
	}
	if err := pub.Set(jwk.KeyIDKey, KeyID); err != nil {
		return nil, fmt.Errorf("set public key id: %w", err)
	}
	if err := pub.Set(jwk.AlgorithmKey, jwa.ES256()); err != nil {
		return nil, fmt.Errorf("set public key algorithm: %w", err)
	}

	set := jwk.NewSet()
	if err := set.AddKey(pub); err != nil {
		return nil, fmt.Errorf("build JWKS: %w", err)
	}

	return &Issuer{private: priv, public: set, audience: aud, now: time.Now}, nil
}

// PublicKeySet returns the JWKS a verifier (the Gateway's
// pkg/identity.JWTResolver) needs to check SIT and exchanged-token
// signatures. It never exposes the private key.
func (i *Issuer) PublicKeySet() jwk.Set { return i.public }

// IssueSIT mints a Session Identity Token for agentID, carrying
// delegationChain as an ordered list of principal identifiers (outermost
// first — a human, then the agents between it and this one; see
// pkg/types.Subject.DelegationChain's doc comment) and scope as the list
// of scopes this token grants (pkg/types.Subject.Scopes; doc 04 §1.1's
// `scope` claim — added in Milestone M2, once pkg/toolregistry gave it a
// reader). Simulates doc 04 §1.1's "runtime attests the sandbox... and
// receives a short-lived SIT" — there is no real sandbox attestation in
// Phase 0, so this simply signs whatever it's asked to assert. The human
// hop in delegationChain is therefore an asserted claim, not independently
// verified against a real IdP — see
// docs/adr/0007-credential-broker-scope.md.
func (i *Issuer) IssueSIT(agentID string, delegationChain, scope []string, ttl time.Duration) (string, error) {
	if agentID == "" {
		return "", fmt.Errorf("agentID is required")
	}

	instanceID, err := randomHex(8)
	if err != nil {
		return "", err
	}

	now := i.now()
	tok, err := jwt.NewBuilder().
		Subject("agent_instance:"+instanceID).
		Claim("agent", agentID).
		Claim("delegation_chain", delegationChain).
		Claim("scope", scope).
		Audience([]string{i.audience}).
		IssuedAt(now).
		Expiration(now.Add(ttl)).
		Build()
	if err != nil {
		return "", fmt.Errorf("build SIT: %w", err)
	}

	signed, err := jwt.Sign(tok, jwt.WithKey(jwa.ES256(), i.private))
	if err != nil {
		return "", fmt.Errorf("sign SIT: %w", err)
	}
	return string(signed), nil
}

// ExchangeResult is the per-call credential Exchange mints.
type ExchangeResult struct {
	Token     string
	ExpiresAt time.Time
}

// Exchange is the Credential Broker's on-behalf-of step (doc 04 §3,
// credential ladder rung 1): it verifies subjectToken (a SIT this same
// Issuer minted) and, if valid, mints a new token scoped to exactly one
// upstream and one tool, with its own short TTL — never a copy or a
// re-audience of the SIT itself. The Gateway presents this minted token
// upstream; it never presents the agent's own SIT to anything but this
// Issuer and the PDP.
func (i *Issuer) Exchange(ctx context.Context, subjectToken, upstream, tool string, ttl time.Duration) (ExchangeResult, error) {
	tok, err := jwt.Parse([]byte(subjectToken),
		jwt.WithKeySet(i.public),
		jwt.WithValidate(true),
		jwt.WithAudience(i.audience),
		jwt.WithContext(ctx),
	)
	if err != nil {
		return ExchangeResult{}, fmt.Errorf("subject_token invalid: %w", err)
	}

	var agentID string
	if err := tok.Get("agent", &agentID); err != nil || agentID == "" {
		return ExchangeResult{}, fmt.Errorf("subject_token missing required 'agent' claim")
	}
	instanceID, _ := tok.Subject()

	now := i.now()
	exp := now.Add(ttl)
	downstream, err := jwt.NewBuilder().
		Subject(instanceID).
		Claim("agent", agentID).
		Claim("upstream", upstream).
		Claim("tool", tool).
		Audience([]string{upstream}).
		IssuedAt(now).
		Expiration(exp).
		Build()
	if err != nil {
		return ExchangeResult{}, fmt.Errorf("build downstream token: %w", err)
	}

	signed, err := jwt.Sign(downstream, jwt.WithKey(jwa.ES256(), i.private))
	if err != nil {
		return ExchangeResult{}, fmt.Errorf("sign downstream token: %w", err)
	}
	return ExchangeResult{Token: string(signed), ExpiresAt: exp}, nil
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate random id: %w", err)
	}
	return hex.EncodeToString(b), nil
}
