// Copyright (c) 2026 Overarching AI LLC
// SPDX-License-Identifier: Apache-2.0

package devissuer

import (
	"context"
	"testing"
	"time"

	"github.com/lestrrat-go/jwx/v3/jwt"
)

const testAudience = "helmdeep-gateways"

func TestIssueSITRoundTrips(t *testing.T) {
	iss, err := New(testAudience)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	token, err := iss.IssueSIT("agent:finance-bot", []string{"user:marcus@corp.com", "agent:finance-bot"}, []string{"supplier.read"}, "1.2.0", 15*time.Minute)
	if err != nil {
		t.Fatalf("IssueSIT: %v", err)
	}

	tok, err := jwt.Parse([]byte(token), jwt.WithKeySet(iss.PublicKeySet()), jwt.WithValidate(true), jwt.WithAudience(testAudience),
		jwt.WithTypedClaim("delegation_chain", []string{}), jwt.WithTypedClaim("scope", []string{}))
	if err != nil {
		t.Fatalf("Parse/verify a token this same issuer minted: %v", err)
	}

	var agent string
	if err := tok.Get("agent", &agent); err != nil || agent != "agent:finance-bot" {
		t.Fatalf("agent claim = %q, err=%v, want agent:finance-bot", agent, err)
	}
	var chain []string
	if err := tok.Get("delegation_chain", &chain); err != nil || len(chain) != 2 {
		t.Fatalf("delegation_chain = %v, err=%v, want 2 entries", chain, err)
	}
	var scope []string
	if err := tok.Get("scope", &scope); err != nil || len(scope) != 1 || scope[0] != "supplier.read" {
		t.Fatalf("scope = %v, err=%v, want [supplier.read]", scope, err)
	}
	var agentVersion string
	if err := tok.Get("agent_version", &agentVersion); err != nil || agentVersion != "1.2.0" {
		t.Fatalf("agent_version = %q, err=%v, want 1.2.0", agentVersion, err)
	}
}

func TestIssueSITRejectsEmptyAgent(t *testing.T) {
	iss, err := New(testAudience)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := iss.IssueSIT("", nil, nil, "", time.Minute); err == nil {
		t.Fatal("IssueSIT with an empty agent ID should error")
	}
}

func TestExpiredSITFailsVerification(t *testing.T) {
	iss, err := New(testAudience)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	fixedNow := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	iss.now = func() time.Time { return fixedNow }

	token, err := iss.IssueSIT("agent:test", nil, nil, "", time.Minute)
	if err != nil {
		t.Fatalf("IssueSIT: %v", err)
	}

	iss.now = func() time.Time { return fixedNow.Add(2 * time.Minute) } // past expiry
	_, err = jwt.Parse([]byte(token), jwt.WithKeySet(iss.PublicKeySet()), jwt.WithValidate(true),
		jwt.WithAudience(testAudience), jwt.WithClock(jwt.ClockFunc(iss.now)))
	if err == nil {
		t.Fatal("an expired SIT verified successfully")
	}
}

func TestForgedSITFailsVerification(t *testing.T) {
	iss, err := New(testAudience)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	other, err := New(testAudience) // a different issuer, different key
	if err != nil {
		t.Fatalf("New (other): %v", err)
	}

	forged, err := other.IssueSIT("agent:attacker", nil, nil, "", time.Minute)
	if err != nil {
		t.Fatalf("IssueSIT (other): %v", err)
	}

	// Verifying "other"'s token against "iss"'s key set must fail — this
	// is the whole point of asymmetric signing: possessing a validly
	// *shaped* token isn't possessing a validly *signed* one.
	_, err = jwt.Parse([]byte(forged), jwt.WithKeySet(iss.PublicKeySet()), jwt.WithValidate(true), jwt.WithAudience(testAudience))
	if err == nil {
		t.Fatal("a token signed by a different issuer's key verified successfully")
	}
}

func TestExchangeMintsScopedShortLivedToken(t *testing.T) {
	iss, err := New(testAudience)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	sit, err := iss.IssueSIT("agent:finance-bot", []string{"user:marcus@corp.com"}, []string{"supplier.read"}, "", 15*time.Minute)
	if err != nil {
		t.Fatalf("IssueSIT: %v", err)
	}

	result, err := iss.Exchange(context.Background(), sit, "suppliermaster", "supplier.get_account", time.Minute)
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	if result.Token == sit {
		t.Fatal("Exchange returned the subject_token unchanged instead of minting a new one")
	}

	downstream, err := jwt.Parse([]byte(result.Token), jwt.WithKeySet(iss.PublicKeySet()), jwt.WithValidate(true), jwt.WithAudience("suppliermaster"))
	if err != nil {
		t.Fatalf("the minted downstream token doesn't verify: %v", err)
	}
	var tool string
	if err := downstream.Get("tool", &tool); err != nil || tool != "supplier.get_account" {
		t.Fatalf("tool claim = %q, err=%v, want supplier.get_account", tool, err)
	}
	exp, ok := downstream.Expiration()
	if !ok || time.Until(exp) > time.Minute+time.Second {
		t.Fatalf("downstream token expiration = %v, want ~1 minute out", exp)
	}
}

func TestExchangeRejectsForgedSubjectToken(t *testing.T) {
	iss, err := New(testAudience)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := iss.Exchange(context.Background(), "not-even-a-jwt", "suppliermaster", "supplier.get_account", time.Minute); err == nil {
		t.Fatal("Exchange accepted a malformed subject_token")
	}
}
