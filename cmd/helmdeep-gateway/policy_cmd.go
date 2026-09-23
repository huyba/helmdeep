// Copyright (c) 2026 Overarching AI LLC
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/huyba/helmdeep/pkg/policyservice"
)

// runPolicy is the Policy Service's authoring-side surface: the three
// commands an operator needs to produce and check the signed bundles
// `serve` refuses to run without, when policy.signature is configured.
// See docs/adr/0014-policy-service.md.
func runPolicy(args []string) error {
	if len(args) == 0 {
		policyUsage()
		return fmt.Errorf("policy: a subcommand is required")
	}
	switch args[0] {
	case "keygen":
		return runPolicyKeygen(args[1:])
	case "sign":
		return runPolicySign(args[1:])
	case "verify":
		return runPolicyVerify(args[1:])
	default:
		policyUsage()
		return fmt.Errorf("policy: unknown subcommand %q", args[0])
	}
}

func policyUsage() {
	fmt.Fprintln(os.Stderr, `usage: helmdeep-gateway policy <subcommand> [flags]

subcommands:
  keygen  generate an Ed25519 bundle-signing keypair
            -out-dir  directory to write policy.key (0600) and policy.pub into
  sign    sign a policy bundle, writing its manifest
            -bundle    bundle directory (required)
            -key       private key file from keygen (required)
            -version   bundle version string (required)
            -manifest  manifest path (default <bundle>/.bundle.json)
  verify  verify a policy bundle against its manifest
            -bundle      bundle directory (required)
            -public-key  public key file from keygen (required)
            -manifest    manifest path (default <bundle>/.bundle.json)`)
}

func runPolicyKeygen(args []string) error {
	fs := flag.NewFlagSet("policy keygen", flag.ExitOnError)
	outDir := fs.String("out-dir", ".", "directory to write the keypair into")
	if err := fs.Parse(args); err != nil {
		return err
	}

	privPath := filepath.Join(*outDir, "policy.key")
	pubPath := filepath.Join(*outDir, "policy.pub")
	// Refuse to clobber an existing key: overwriting the private key of a
	// bundle already in production would silently invalidate every
	// manifest signed with it, and there is no recovering the old one.
	for _, path := range []string{privPath, pubPath} {
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("%s already exists; refusing to overwrite an existing signing key", path)
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("check %s: %w", path, err)
		}
	}

	pub, priv, err := policyservice.GenerateKey()
	if err != nil {
		return err
	}
	if err := policyservice.WritePrivateKey(privPath, priv); err != nil {
		return err
	}
	if err := policyservice.WritePublicKey(pubPath, pub); err != nil {
		return err
	}
	fmt.Println("wrote", privPath, "(keep this secret) and", pubPath)
	fmt.Println("key id:", policyservice.KeyID(pub))
	return nil
}

func runPolicySign(args []string) error {
	fs := flag.NewFlagSet("policy sign", flag.ExitOnError)
	bundle := fs.String("bundle", "", "policy bundle directory to sign")
	keyPath := fs.String("key", "", "Ed25519 private key file (see policy keygen)")
	version := fs.String("version", "", "bundle version string recorded in every action record")
	manifest := fs.String("manifest", "", "manifest path (default <bundle>/.bundle.json)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *bundle == "" || *keyPath == "" || *version == "" {
		policyUsage()
		return fmt.Errorf("policy sign: -bundle, -key, and -version are all required")
	}

	key, err := policyservice.ReadPrivateKey(*keyPath)
	if err != nil {
		return err
	}
	m, err := policyservice.Sign(*bundle, *version, key)
	if err != nil {
		return err
	}
	path := manifestPathOrDefault(*manifest, *bundle)
	if err := policyservice.WriteManifest(path, m); err != nil {
		return err
	}
	digest, err := m.Digest()
	if err != nil {
		return err
	}
	fmt.Printf("signed bundle %s version %s (%d files, %s)\n", *bundle, m.Version, len(m.Files), digest)
	fmt.Println("manifest:", path)
	return nil
}

func runPolicyVerify(args []string) error {
	fs := flag.NewFlagSet("policy verify", flag.ExitOnError)
	bundle := fs.String("bundle", "", "policy bundle directory to verify")
	pubPath := fs.String("public-key", "", "Ed25519 public key file (see policy keygen)")
	manifest := fs.String("manifest", "", "manifest path (default <bundle>/.bundle.json)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *bundle == "" || *pubPath == "" {
		policyUsage()
		return fmt.Errorf("policy verify: -bundle and -public-key are both required")
	}

	pub, err := policyservice.ReadPublicKey(*pubPath)
	if err != nil {
		return err
	}
	path := manifestPathOrDefault(*manifest, *bundle)
	m, err := policyservice.ReadManifest(path)
	if err != nil {
		return err
	}
	if err := policyservice.Verify(*bundle, m, pub); err != nil {
		return fmt.Errorf("bundle %s does not match %s: %w", *bundle, path, err)
	}
	digest, err := m.Digest()
	if err != nil {
		return err
	}
	fmt.Printf("bundle intact: %s version %s (%d files, %s)\n", *bundle, m.Version, len(m.Files), digest)
	return nil
}

// manifestPathOrDefault resolves an explicit -manifest flag against the
// in-bundle default, so the flag and the config key (policy.signature.manifest)
// have exactly one defaulting rule between them.
func manifestPathOrDefault(manifest, bundle string) string {
	if manifest != "" {
		return manifest
	}
	return policyservice.DefaultManifestPath(bundle)
}
