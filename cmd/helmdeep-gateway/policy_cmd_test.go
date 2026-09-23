// Copyright (c) 2026 Overarching AI LLC
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPolicyCommandRoundTrip walks the sequence an operator actually runs —
// keygen, sign, verify — and then the sequence an attacker's edit produces:
// verify fails, naming the file that changed.
func TestPolicyCommandRoundTrip(t *testing.T) {
	keys := t.TempDir()
	bundle := t.TempDir()
	if err := os.WriteFile(filepath.Join(bundle, "decision.rego"), []byte("package helmdeep.authz\n"), 0o600); err != nil {
		t.Fatalf("write bundle file: %v", err)
	}

	if err := runPolicy([]string{"keygen", "-out-dir", keys}); err != nil {
		t.Fatalf("policy keygen: %v", err)
	}
	priv := filepath.Join(keys, "policy.key")
	pub := filepath.Join(keys, "policy.pub")

	if err := runPolicy([]string{"sign", "-bundle", bundle, "-key", priv, "-version", "v1"}); err != nil {
		t.Fatalf("policy sign: %v", err)
	}
	if _, err := os.Stat(filepath.Join(bundle, ".bundle.json")); err != nil {
		t.Fatalf("sign did not write the default in-bundle manifest: %v", err)
	}
	if err := runPolicy([]string{"verify", "-bundle", bundle, "-public-key", pub}); err != nil {
		t.Fatalf("policy verify: %v", err)
	}

	if err := os.WriteFile(filepath.Join(bundle, "decision.rego"), []byte("package helmdeep.authz\n\n# edited\n"), 0o600); err != nil {
		t.Fatalf("edit bundle file: %v", err)
	}
	err := runPolicy([]string{"verify", "-bundle", bundle, "-public-key", pub})
	if err == nil {
		t.Fatal("policy verify: expected an error after the bundle was edited without re-signing")
	}
	if !strings.Contains(err.Error(), "decision.rego") {
		t.Fatalf("verify error should name the edited file, got: %v", err)
	}
}

func TestPolicyKeygen_RefusesToOverwriteAnExistingKey(t *testing.T) {
	// Overwriting a signing key silently invalidates every manifest already
	// signed with it, and the old key is unrecoverable.
	keys := t.TempDir()
	if err := runPolicy([]string{"keygen", "-out-dir", keys}); err != nil {
		t.Fatalf("policy keygen: %v", err)
	}
	before, err := os.ReadFile(filepath.Join(keys, "policy.key")) // #nosec G304 -- test-controlled temp path
	if err != nil {
		t.Fatalf("read key: %v", err)
	}
	if err := runPolicy([]string{"keygen", "-out-dir", keys}); err == nil {
		t.Fatal("policy keygen: expected an error when a key already exists")
	}
	after, err := os.ReadFile(filepath.Join(keys, "policy.key")) // #nosec G304 -- test-controlled temp path
	if err != nil {
		t.Fatalf("read key: %v", err)
	}
	if string(before) != string(after) {
		t.Fatal("policy keygen overwrote an existing private key")
	}
}

func TestPolicySubcommandsRequireTheirFlags(t *testing.T) {
	for _, args := range [][]string{
		{},
		{"nonsense"},
		{"sign", "-bundle", t.TempDir()}, // no -key, no -version
		{"sign", "-bundle", t.TempDir(), "-key", "/nonexistent", "-version", "v1"},    // unreadable key
		{"verify", "-bundle", t.TempDir()},                                            // no -public-key
		{"verify", "-bundle", t.TempDir(), "-public-key", "/nonexistent/policy.pub"},  // unreadable key
		{"sign", "-bundle", "/nonexistent", "-key", "/nonexistent", "-version", "v1"}, // unreadable bundle
	} {
		if err := runPolicy(args); err == nil {
			t.Fatalf("runPolicy(%v): expected an error", args)
		}
	}
}

func TestLoadConfig_PolicySignatureValidation(t *testing.T) {
	identity := `identity:
  static_tokens:
    tok:
      id: agent:test
`
	withoutKey := `
policy:
  path: /tmp/policies
  signature: {}
audit:
  path: /tmp/audit.log
` + identity
	if _, err := loadConfig(writeTestConfig(t, withoutKey)); err == nil {
		t.Fatal("loadConfig accepted policy.signature with no public_key, which would start the gateway with verification silently off")
	}

	withKey := `
policy:
  path: /tmp/policies
  signature:
    public_key: /etc/helmdeep/policy.pub
audit:
  path: /tmp/audit.log
` + identity
	cfg, err := loadConfig(writeTestConfig(t, withKey))
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if got, want := cfg.policyManifestPath(), "/tmp/policies/.bundle.json"; got != want {
		t.Fatalf("policyManifestPath() = %q, want the in-bundle default %q", got, want)
	}

	withManifest := `
policy:
  path: /tmp/policies
  signature:
    public_key: /etc/helmdeep/policy.pub
    manifest: /etc/helmdeep/bundle.json
audit:
  path: /tmp/audit.log
` + identity
	cfg, err = loadConfig(writeTestConfig(t, withManifest))
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if got, want := cfg.policyManifestPath(), "/etc/helmdeep/bundle.json"; got != want {
		t.Fatalf("policyManifestPath() = %q, want the configured %q", got, want)
	}

	// Absent signature section: manifest path is unused, and nothing about
	// the config fails to load — verification is off, loudly (see runServe).
	unsigned := baseTestConfig + identity
	cfg, err = loadConfig(writeTestConfig(t, unsigned))
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.Policy.Signature != nil {
		t.Fatal("loadConfig invented a policy.signature section")
	}
}
