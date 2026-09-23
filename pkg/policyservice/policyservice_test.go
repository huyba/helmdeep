// Copyright (c) 2026 Overarching AI LLC
// SPDX-License-Identifier: Apache-2.0

package policyservice

import (
	"crypto/ed25519"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// signedBundle writes a two-file bundle, signs it, and returns the bundle
// directory, the manifest, and the keypair.
func signedBundle(t *testing.T) (string, Manifest, ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	dir := t.TempDir()
	write(t, filepath.Join(dir, "decision.rego"), "package helmdeep.authz\n\ndefault decision := {\"outcome\": \"deny\"}\n")
	write(t, filepath.Join(dir, "scope.rego"), "package helmdeep.authz\n\nscope_allowed if false\n")

	pub, priv, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	m, err := Sign(dir, "v1", priv)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	return dir, m, pub, priv
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestSignAndVerify_UntouchedBundleVerifies(t *testing.T) {
	dir, m, pub, _ := signedBundle(t)
	if err := Verify(dir, m, pub); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if len(m.Files) != 2 {
		t.Fatalf("manifest covers %d files, want 2: %+v", len(m.Files), m.Files)
	}
	if m.Files[0].Path != "decision.rego" || m.Files[1].Path != "scope.rego" {
		t.Fatalf("manifest files are not sorted by path: %+v", m.Files)
	}
}

func TestSign_RequiresAVersion(t *testing.T) {
	dir, _, _, priv := signedBundle(t)
	if _, err := Sign(dir, "", priv); err == nil {
		t.Fatal("Sign: expected an error for an empty version")
	}
}

func TestSign_RefusesAnEmptyBundle(t *testing.T) {
	_, _, _, priv := signedBundle(t)
	if _, err := Sign(t.TempDir(), "v1", priv); err == nil {
		t.Fatal("Sign: expected an error for a bundle with no files")
	}
}

func TestVerify_ModifiedFileFails(t *testing.T) {
	dir, m, pub, _ := signedBundle(t)
	write(t, filepath.Join(dir, "scope.rego"), "package helmdeep.authz\n\nscope_allowed if true\n")
	err := Verify(dir, m, pub)
	if err == nil {
		t.Fatal("Verify: expected an error after a bundle file was modified")
	}
	if !strings.Contains(err.Error(), "scope.rego") {
		t.Fatalf("Verify error should name the changed file, got: %v", err)
	}
}

func TestVerify_AddedFileFails(t *testing.T) {
	// An added .rego file changes what OPA compiles even though no signed
	// file was touched — the case a digest-per-file check without a
	// completeness check would miss.
	dir, m, pub, _ := signedBundle(t)
	write(t, filepath.Join(dir, "backdoor.rego"), "package helmdeep.authz\n\ndecision := {\"outcome\": \"allow\"}\n")
	err := Verify(dir, m, pub)
	if err == nil {
		t.Fatal("Verify: expected an error after an unsigned file was added to the bundle")
	}
	if !strings.Contains(err.Error(), "backdoor.rego") {
		t.Fatalf("Verify error should name the added file, got: %v", err)
	}
}

func TestVerify_RemovedFileFails(t *testing.T) {
	dir, m, pub, _ := signedBundle(t)
	if err := os.Remove(filepath.Join(dir, "scope.rego")); err != nil {
		t.Fatalf("remove: %v", err)
	}
	err := Verify(dir, m, pub)
	if err == nil {
		t.Fatal("Verify: expected an error after a signed file was removed")
	}
	if !strings.Contains(err.Error(), "scope.rego") {
		t.Fatalf("Verify error should name the missing file, got: %v", err)
	}
}

func TestVerify_WrongKeyFails(t *testing.T) {
	dir, m, _, _ := signedBundle(t)
	otherPub, _, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	verifyErr := Verify(dir, m, otherPub)
	if verifyErr == nil {
		t.Fatal("Verify: expected an error for a manifest signed by a different key")
	}
	// "wrong key" and "invalid signature" are distinguishable on purpose:
	// an operator holding the wrong public key needs a different fix from
	// one looking at a tampered manifest.
	if !strings.Contains(verifyErr.Error(), "signed by key") {
		t.Fatalf("Verify error should report a key mismatch, got: %v", verifyErr)
	}
}

func TestVerify_TamperedManifestFieldFails(t *testing.T) {
	// The signature covers the manifest's own contents, so rewriting the
	// file list or the version without the private key is detectable.
	dir, m, pub, _ := signedBundle(t)
	m.Files[1].SHA256 = "0000000000000000000000000000000000000000000000000000000000000000"
	if err := Verify(dir, m, pub); err == nil {
		t.Fatal("Verify: expected an error for a manifest whose file digests were rewritten")
	}

	_, fresh, freshPub, _ := signedBundle(t)
	fresh.Version = "v999"
	if err := Verify(dir, fresh, freshPub); err == nil {
		t.Fatal("Verify: expected an error for a manifest whose version was rewritten")
	}
}

func TestVerify_UnsignedManifestFails(t *testing.T) {
	dir, m, pub, _ := signedBundle(t)
	m.Signature = ""
	if err := Verify(dir, m, pub); err == nil {
		t.Fatal("Verify: expected an error for a manifest with no signature")
	}
}

func TestInventory_SkipsDotfilesLikeOPADoes(t *testing.T) {
	// pkg/policy's loader filter skips anything below the bundle root whose
	// name starts with "." — the signed file set has to match the compiled
	// file set exactly, including the manifest's own default location
	// inside the bundle.
	dir, m, pub, priv := signedBundle(t)
	if err := WriteManifest(DefaultManifestPath(dir), m); err != nil {
		t.Fatalf("WriteManifest: %v", err)
	}
	if err := Verify(dir, m, pub); err != nil {
		t.Fatalf("Verify after writing the manifest into the bundle: %v", err)
	}

	if err := os.MkdirAll(filepath.Join(dir, "..2026_09_22"), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	write(t, filepath.Join(dir, "..2026_09_22", "decision.rego"), "package helmdeep.authz\n")
	if err := Verify(dir, m, pub); err != nil {
		t.Fatalf("Verify with a ConfigMap-style hidden directory present: %v", err)
	}

	// And re-signing the same bundle yields the same file set, not one
	// that has grown to include the manifest or the hidden directory.
	resigned, err := Sign(dir, "v2", priv)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if len(resigned.Files) != 2 {
		t.Fatalf("re-signed manifest covers %d files, want 2: %+v", len(resigned.Files), resigned.Files)
	}
}

func TestInventory_CoversSubdirectories(t *testing.T) {
	dir, _, _, priv := signedBundle(t)
	if err := os.MkdirAll(filepath.Join(dir, "lib"), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	write(t, filepath.Join(dir, "lib", "helpers.rego"), "package helmdeep.lib\n")
	m, err := Sign(dir, "v1", priv)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	var found bool
	for _, f := range m.Files {
		if f.Path == "lib/helpers.rego" {
			found = true
		}
	}
	if !found {
		t.Fatalf("manifest does not cover a nested bundle file: %+v", m.Files)
	}
}

func TestDigest_ChangesWithContentAndIsStable(t *testing.T) {
	_, m, _, _ := signedBundle(t)
	first, err := m.Digest()
	if err != nil {
		t.Fatalf("Digest: %v", err)
	}
	again, err := m.Digest()
	if err != nil {
		t.Fatalf("Digest: %v", err)
	}
	if first != again {
		t.Fatalf("Digest is not stable: %q then %q", first, again)
	}

	// Same signing time, one different file digest: the bundle's content is
	// the only thing that changed, and the digest has to move with it — a
	// digest that only tracked the version string or the timestamp would
	// make two different bundles indistinguishable in the ledger.
	altered := m
	altered.Files = append([]FileDigest(nil), m.Files...)
	altered.Files[0].SHA256 = "0000000000000000000000000000000000000000000000000000000000000000"
	alteredDigest, err := altered.Digest()
	if err != nil {
		t.Fatalf("Digest: %v", err)
	}
	if alteredDigest == first {
		t.Fatal("digest did not change when a covered file's digest did")
	}
}

func TestManifestRoundTripsThroughDiskAndStillVerifies(t *testing.T) {
	// Marshal/unmarshal must not change the bytes the signature covers —
	// in particular CreatedAt's timezone rendering.
	dir, m, pub, _ := signedBundle(t)
	path := filepath.Join(t.TempDir(), "bundle.json")
	if err := WriteManifest(path, m); err != nil {
		t.Fatalf("WriteManifest: %v", err)
	}
	read, err := ReadManifest(path)
	if err != nil {
		t.Fatalf("ReadManifest: %v", err)
	}
	if err := Verify(dir, read, pub); err != nil {
		t.Fatalf("Verify after a disk round trip: %v", err)
	}
	wantDigest, err := m.Digest()
	if err != nil {
		t.Fatalf("Digest: %v", err)
	}
	gotDigest, err := read.Digest()
	if err != nil {
		t.Fatalf("Digest: %v", err)
	}
	if gotDigest != wantDigest {
		t.Fatalf("digest changed across a disk round trip: %q != %q", gotDigest, wantDigest)
	}
}

func TestKeyFilesRoundTrip(t *testing.T) {
	dir := t.TempDir()
	pub, priv, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	privPath := filepath.Join(dir, "policy.key")
	pubPath := filepath.Join(dir, "policy.pub")
	if err := WritePrivateKey(privPath, priv); err != nil {
		t.Fatalf("WritePrivateKey: %v", err)
	}
	if err := WritePublicKey(pubPath, pub); err != nil {
		t.Fatalf("WritePublicKey: %v", err)
	}

	info, err := os.Stat(privPath)
	if err != nil {
		t.Fatalf("stat private key: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("private key mode is %o, want 600", perm)
	}

	readPriv, err := ReadPrivateKey(privPath)
	if err != nil {
		t.Fatalf("ReadPrivateKey: %v", err)
	}
	readPub, err := ReadPublicKey(pubPath)
	if err != nil {
		t.Fatalf("ReadPublicKey: %v", err)
	}
	if KeyID(readPub) != KeyID(pub) {
		t.Fatal("public key changed across a disk round trip")
	}

	bundle := t.TempDir()
	write(t, filepath.Join(bundle, "decision.rego"), "package helmdeep.authz\n")
	m, err := Sign(bundle, "v1", readPriv)
	if err != nil {
		t.Fatalf("Sign with a key read from disk: %v", err)
	}
	if err := Verify(bundle, m, readPub); err != nil {
		t.Fatalf("Verify with a key read from disk: %v", err)
	}
}

func TestReadPublicKey_RejectsAPrivateKeyFile(t *testing.T) {
	dir := t.TempDir()
	_, priv, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	path := filepath.Join(dir, "policy.key")
	if err := WritePrivateKey(path, priv); err != nil {
		t.Fatalf("WritePrivateKey: %v", err)
	}
	if _, err := ReadPublicKey(path); err == nil {
		t.Fatal("ReadPublicKey: expected an error when handed a private key file")
	}
}
