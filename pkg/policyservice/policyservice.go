// Copyright (c) 2026 Overarching AI LLC
// SPDX-License-Identifier: Apache-2.0

// Package policyservice is the Policy Service (docs/02-architecture.md §2's
// Control Plane: "Authoring, compilation, testing, distribution of signed
// policy bundles"), narrowed to the one half of that responsibility a
// single-process gateway can actually enforce today: bundle integrity at
// the point of consumption. docs/09-governance-trust.md §2.2 states the
// rule this package implements — policy is "compiled to signed bundles,
// versioned, distributed to sidecar PDPs, pinned per session" — and §4
// lists policy bundles among the inputs that must be versioned artifacts.
//
// Concretely, three things:
//
//   - A Manifest: the list of files in a bundle with their SHA-256 digests,
//     an operator-chosen version string, and an Ed25519 signature over all
//     of it. Sign produces one; Verify checks a bundle on disk against one.
//   - VerifyingLoader: decorates a pkg/policy.Loader so an unsigned or
//     modified bundle is refused rather than compiled (see loader.go).
//   - StampingStore: decorates a pkg/audit.Store so every Action Record
//     names the bundle version and digest that decided it (see stamp.go).
//
// This is not the full component doc 02 describes: there is no authoring
// UI, no compilation service, no test runner, no Postgres, and no OCI
// registry to distribute bundles through — see
// docs/adr/0014-policy-service.md for the scoping reasoning and the full
// list of what is left out.
package policyservice

import (
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// FileDigest is one file in a bundle and the SHA-256 of its contents. Path
// is always slash-separated and relative to the bundle root, so a manifest
// signed on one machine verifies on another regardless of path separator.
type FileDigest struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

// Manifest is a bundle's signed inventory. The signature covers Version,
// CreatedAt, and Files — everything that describes the bundle's contents —
// but not KeyID or Signature themselves, which are how the signature is
// found and checked rather than part of what it attests to.
type Manifest struct {
	// Version is the operator-chosen bundle version. It is the string that
	// ends up in every Action Record's policy_bundle.version, so it should
	// be something a reviewer can trace back to a git tag or commit.
	Version string `json:"version"`
	// CreatedAt is when the bundle was signed, normalized to UTC.
	CreatedAt time.Time `json:"created_at"`
	// Files is every file in the bundle, sorted by Path.
	Files []FileDigest `json:"files"`
	// KeyID identifies the signing key (see KeyID). Verify checks it
	// against the public key it was handed, so "you are holding the wrong
	// key" is a distinguishable error from "this signature is invalid."
	KeyID string `json:"key_id"`
	// Signature is the Ed25519 signature over signedPayload, base64
	// (standard encoding).
	Signature string `json:"signature"`
}

// signedPayload is the exact byte sequence that gets signed and hashed.
// Keeping it a separate type from Manifest is what makes "the signature
// covers the contents, not the signature" a property of the code rather
// than of a comment: there is no way to include KeyID or Signature in it.
type signedPayload struct {
	Version   string       `json:"version"`
	CreatedAt time.Time    `json:"created_at"`
	Files     []FileDigest `json:"files"`
}

func (m Manifest) signedPayload() ([]byte, error) {
	// CreatedAt is normalized to UTC on both the signing and verifying
	// side, so a manifest written in one zone verifies byte-identically in
	// another: encoding/json emits a time.Time in whatever offset it
	// carries, and a stored "+07:00" would otherwise not reproduce the
	// bytes that were signed.
	b, err := json.Marshal(signedPayload{
		Version:   m.Version,
		CreatedAt: m.CreatedAt.UTC(),
		Files:     m.Files,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal manifest payload: %w", err)
	}
	return b, nil
}

// Digest is the bundle's content digest: SHA-256 over the signed payload,
// "sha256:"-prefixed. This is what a StampingStore writes into every
// Action Record — it changes if any file, the file list, the version, or
// the signing time changes, so two records carrying the same digest were
// decided by byte-identical policy.
func (m Manifest) Digest() (string, error) {
	payload, err := m.signedPayload()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// Sign inventories the bundle at bundleDir and returns a signed Manifest
// for it. version is the operator's own version string and is required —
// an unversioned bundle would defeat the point of recording which bundle
// decided an action (docs/09-governance-trust.md §4: "every
// behavior-affecting input a versioned artifact").
func Sign(bundleDir, version string, key ed25519.PrivateKey) (Manifest, error) {
	if version == "" {
		return Manifest{}, fmt.Errorf("bundle version is required")
	}
	if len(key) != ed25519.PrivateKeySize {
		return Manifest{}, fmt.Errorf("signing key is not a valid Ed25519 private key")
	}
	files, err := inventory(bundleDir)
	if err != nil {
		return Manifest{}, err
	}
	if len(files) == 0 {
		// A signature over an empty bundle would be valid and useless: the
		// PDP would then fail to compile it (OPA needs at least one module)
		// or, worse, compile an empty policy that denies everything for a
		// reason nobody can trace. Refuse at signing time instead.
		return Manifest{}, fmt.Errorf("bundle at %q contains no files", bundleDir)
	}

	pub, ok := key.Public().(ed25519.PublicKey)
	if !ok {
		return Manifest{}, fmt.Errorf("signing key does not yield an Ed25519 public key")
	}
	m := Manifest{
		Version:   version,
		CreatedAt: time.Now().UTC(),
		Files:     files,
		KeyID:     KeyID(pub),
	}
	payload, err := m.signedPayload()
	if err != nil {
		return Manifest{}, err
	}
	m.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(key, payload))
	return m, nil
}

// Verify reports whether the bundle on disk at bundleDir is exactly the
// bundle m was signed over, and that m was signed by pub. Every failure is
// an error naming what specifically failed — a caller's only correct
// response to any of them is to refuse the bundle, but an operator needs
// to know which of "wrong key", "bad signature", "file changed", "file
// added", and "file missing" they are looking at.
func Verify(bundleDir string, m Manifest, pub ed25519.PublicKey) error {
	if len(pub) != ed25519.PublicKeySize {
		return fmt.Errorf("public key is not a valid Ed25519 public key")
	}
	if m.KeyID == "" || m.Signature == "" {
		return fmt.Errorf("manifest is not signed (missing key_id or signature)")
	}
	if m.KeyID != KeyID(pub) {
		return fmt.Errorf("manifest was signed by key %s, but the configured public key is %s", m.KeyID, KeyID(pub))
	}
	sig, err := base64.StdEncoding.DecodeString(m.Signature)
	if err != nil {
		return fmt.Errorf("decode manifest signature: %w", err)
	}
	payload, err := m.signedPayload()
	if err != nil {
		return err
	}
	if !ed25519.Verify(pub, payload, sig) {
		return fmt.Errorf("manifest signature is invalid for key %s", m.KeyID)
	}

	// Only now, with the manifest itself authenticated, is it worth
	// comparing it against the filesystem: comparing first would report
	// "file changed" for a bundle whose real problem is that anyone could
	// have written the manifest.
	onDisk, err := inventory(bundleDir)
	if err != nil {
		return err
	}
	signed := make(map[string]string, len(m.Files))
	for _, f := range m.Files {
		if _, dup := signed[f.Path]; dup {
			return fmt.Errorf("manifest lists %q more than once", f.Path)
		}
		signed[f.Path] = f.SHA256
	}
	for _, f := range onDisk {
		want, ok := signed[f.Path]
		if !ok {
			// An added file is as much a tamper as a modified one: a new
			// .rego file in the bundle directory changes what the PDP
			// compiles, even though no signed file was touched.
			return fmt.Errorf("bundle contains %q, which the manifest does not cover", f.Path)
		}
		if want != f.SHA256 {
			return fmt.Errorf("bundle file %q has digest %s, but the manifest signed %s", f.Path, f.SHA256, want)
		}
		delete(signed, f.Path)
	}
	if len(signed) > 0 {
		missing := make([]string, 0, len(signed))
		for path := range signed {
			missing = append(missing, path)
		}
		sort.Strings(missing)
		return fmt.Errorf("bundle is missing %d file(s) the manifest signed: %s", len(missing), strings.Join(missing, ", "))
	}
	return nil
}

// inventory lists every file in the bundle with its digest, sorted by path.
//
// The traversal deliberately mirrors pkg/policy's hiddenFileFilter — it
// skips any entry below the bundle root whose name starts with "." — so the
// file set signed here is exactly the file set OPA compiles. Two
// consequences worth naming: a Kubernetes ConfigMap volume's hidden
// "..data"/"..<timestamp>" indirection is skipped here just as it is
// there (its visible top-level symlinks per key are what gets hashed, and
// os.ReadFile follows them), and a manifest stored inside the bundle as a
// dotfile — the default, ".bundle.json" — is excluded from its own
// inventory without needing a special case for it.
// The walk is rooted with os.OpenRoot so every read resolves inside the
// bundle directory: a symlink pointing out of the bundle can't make this
// function hash — or a later signature vouch for — a file the bundle
// doesn't contain.
func inventory(bundleDir string) ([]FileDigest, error) {
	root, err := os.OpenRoot(bundleDir)
	if err != nil {
		return nil, fmt.Errorf("open bundle at %q: %w", bundleDir, err)
	}
	defer func() { _ = root.Close() }()

	var out []FileDigest
	err = fs.WalkDir(root.FS(), ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == "." {
			return nil // the bundle root itself, which may legitimately be a dotdir
		}
		if strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 {
			// WalkDir does not descend into symlinked directories, so a
			// bundle containing one would have unhashed files inside the
			// signed tree. Refuse rather than sign a partial inventory.
			// Symlinks to files are fine and common (see ConfigMap above).
			info, statErr := root.Stat(path)
			if statErr != nil {
				return fmt.Errorf("resolve bundle symlink %q: %w", path, statErr)
			}
			if info.IsDir() {
				return fmt.Errorf("bundle entry %q is a symlink to a directory, which is not supported", path)
			}
		}
		f, err := root.Open(path)
		if err != nil {
			return fmt.Errorf("read bundle file %q: %w", path, err)
		}
		defer func() { _ = f.Close() }()
		digest := sha256.New()
		if _, err := io.Copy(digest, f); err != nil {
			return fmt.Errorf("read bundle file %q: %w", path, err)
		}
		// fs.WalkDir paths are already slash-separated and relative to the
		// bundle root, which is exactly the manifest's own path format.
		out = append(out, FileDigest{Path: path, SHA256: hex.EncodeToString(digest.Sum(nil))})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("inventory bundle at %q: %w", bundleDir, err)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

// KeyID is a stable, non-secret identifier for a public key: SHA-256 over
// its raw bytes, "sha256:"-prefixed.
func KeyID(pub ed25519.PublicKey) string {
	sum := sha256.Sum256(pub)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// DefaultManifestPath is where a bundle's manifest lives when an operator
// doesn't say. It's a dotfile inside the bundle so the bundle stays one
// self-contained directory to copy, mount, or check into git, and so
// neither OPA's loader nor this package's own inventory picks it up as
// bundle content — see inventory.
func DefaultManifestPath(bundleDir string) string {
	return filepath.Join(bundleDir, ".bundle.json")
}

// ReadManifest reads and parses a manifest from path. It does not verify
// anything — Verify does that, and a caller that only reads is a caller
// that trusts whatever is on disk.
func ReadManifest(path string) (Manifest, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- path is operator-supplied gateway config, not attacker-controlled input
	if err != nil {
		return Manifest{}, fmt.Errorf("read policy bundle manifest: %w", err)
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return Manifest{}, fmt.Errorf("parse policy bundle manifest %q: %w", path, err)
	}
	return m, nil
}

// WriteManifest writes m to path as indented JSON, creating it if needed.
// The manifest is not a secret — it is the thing you distribute alongside
// the bundle — so it is written world-readable.
func WriteManifest(path string, m Manifest) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o644); err != nil { // #nosec G306 -- a signed manifest is public by design; its integrity comes from the signature, not from file permissions
		return fmt.Errorf("write manifest %q: %w", path, err)
	}
	return nil
}

// GenerateKey returns a new Ed25519 signing keypair.
func GenerateKey() (ed25519.PublicKey, ed25519.PrivateKey, error) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		return nil, nil, fmt.Errorf("generate signing key: %w", err)
	}
	return pub, priv, nil
}

// WritePrivateKey writes key as a PKCS#8 PEM file with 0600 permissions.
func WritePrivateKey(path string, key ed25519.PrivateKey) error {
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return fmt.Errorf("marshal private key: %w", err)
	}
	block := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
	if err := os.WriteFile(path, block, 0o600); err != nil {
		return fmt.Errorf("write private key %q: %w", path, err)
	}
	return nil
}

// WritePublicKey writes pub as a PKIX PEM file. This is the file a gateway
// is configured with; it is public by definition.
func WritePublicKey(path string, pub ed25519.PublicKey) error {
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return fmt.Errorf("marshal public key: %w", err)
	}
	block := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})
	if err := os.WriteFile(path, block, 0o644); err != nil { // #nosec G306 -- a public key is public by design
		return fmt.Errorf("write public key %q: %w", path, err)
	}
	return nil
}

// ReadPrivateKey reads a PKCS#8 PEM Ed25519 private key.
func ReadPrivateKey(path string) (ed25519.PrivateKey, error) {
	der, err := readPEM(path, "PRIVATE KEY")
	if err != nil {
		return nil, err
	}
	parsed, err := x509.ParsePKCS8PrivateKey(der)
	if err != nil {
		return nil, fmt.Errorf("parse private key %q: %w", path, err)
	}
	key, ok := parsed.(ed25519.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("private key %q is %T, not Ed25519", path, parsed)
	}
	return key, nil
}

// ReadPublicKey reads a PKIX PEM Ed25519 public key.
func ReadPublicKey(path string) (ed25519.PublicKey, error) {
	der, err := readPEM(path, "PUBLIC KEY")
	if err != nil {
		return nil, err
	}
	parsed, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		return nil, fmt.Errorf("parse public key %q: %w", path, err)
	}
	pub, ok := parsed.(ed25519.PublicKey)
	if !ok {
		return nil, fmt.Errorf("public key %q is %T, not Ed25519", path, parsed)
	}
	return pub, nil
}

func readPEM(path, wantType string) ([]byte, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- path is operator-supplied gateway config, not attacker-controlled input
	if err != nil {
		return nil, fmt.Errorf("read key %q: %w", path, err)
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("key %q is not PEM-encoded", path)
	}
	if block.Type != wantType {
		return nil, fmt.Errorf("key %q is a %q PEM block, expected %q", path, block.Type, wantType)
	}
	return block.Bytes, nil
}
