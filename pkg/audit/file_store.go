// Copyright (c) 2026 Overarching AI LLC
// SPDX-License-Identifier: Apache-2.0

package audit

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/huyba/helmdeep/pkg/types"
)

// hashable is the subset of an ActionRecord that gets hashed: everything
// except Hash itself, which is the output, not the input, of the
// computation — a field can't authenticate itself. Field order here is
// exactly ActionRecord's field order, and Go's encoding/json sorts map keys
// alphabetically, so the same record always hashes to the same value
// regardless of which process computed it.
type hashable struct {
	Timestamp time.Time              `json:"timestamp"`
	Subject   types.Subject          `json:"subject"`
	Action    types.Action           `json:"action"`
	Decision  types.DecisionResponse `json:"decision"`
	Outcome   types.RecordOutcome    `json:"outcome"`
	PrevHash  string                 `json:"prev_hash"`
}

func computeHash(rec types.ActionRecord) (string, error) {
	h := hashable{
		Timestamp: rec.Timestamp,
		Subject:   rec.Subject,
		Action:    rec.Action,
		Decision:  rec.Decision,
		Outcome:   rec.Outcome,
		PrevHash:  rec.PrevHash,
	}
	b, err := json.Marshal(h)
	if err != nil {
		return "", fmt.Errorf("marshal record for hashing: %w", err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

// FileStore is a Store backed by a local append-only file, one JSON record
// per line, hash-chained. See docs/adr/0004-action-record-format.md for why
// this is a flat file rather than a database, and what "tamper-evident"
// does and doesn't guarantee.
//
// FileStore is safe for concurrent use.
type FileStore struct {
	mu       sync.Mutex
	path     string
	lastHash string // hash of the most recently appended record; "" before the first record
}

// NewFileStore opens (creating if necessary) the record file at path and
// recovers the chain's current tail hash from it, so restarting the gateway
// continues the same chain instead of silently starting a new one.
func NewFileStore(path string) (*FileStore, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, fmt.Errorf("create audit log directory: %w", err)
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDONLY, 0o600) // #nosec G304 -- path is operator-supplied gateway config, not attacker-controlled input
	if err != nil {
		return nil, fmt.Errorf("open audit log: %w", err)
	}
	defer func() { _ = f.Close() }()

	fs := &FileStore{path: path}
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var rec types.ActionRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			return nil, fmt.Errorf("recover chain tail from %q: %w", path, err)
		}
		fs.lastHash = rec.Hash
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read audit log: %w", err)
	}
	return fs, nil
}

// Append writes rec to the log, chained to the current tail, and fsyncs
// before returning. The fsync is deliberate: for irreversible actions the
// gateway must not proceed until this write is durable (see
// docs/adr/0003-fail-closed-behavior.md) — an Append that returns success
// but didn't survive a crash defeats the point of an audit trail.
func (s *FileStore) Append(ctx context.Context, rec types.ActionRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	rec.PrevHash = s.lastHash
	hash, err := computeHash(rec)
	if err != nil {
		return err
	}
	rec.Hash = hash

	line, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("marshal action record: %w", err)
	}
	line = append(line, '\n')

	f, err := os.OpenFile(s.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open audit log for append: %w", err)
	}
	defer func() { _ = f.Close() }()

	if _, err := f.Write(line); err != nil {
		return fmt.Errorf("write action record: %w", err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("fsync audit log: %w", err)
	}

	s.lastHash = hash
	return nil
}

// Check reports whether the log file can currently be opened for append and
// fsynced — the same two operations Append depends on — without writing a
// byte, since anything written here would be a record in the hash chain. It
// catches a read-only or vanished filesystem, a detached volume, and an I/O
// error on sync; it does not detect a merely full disk, which only a real
// write would reveal. It shares Append's lock so a probe can't interleave
// with a record being written.
func (s *FileStore) Check(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return err
	}
	f, err := os.OpenFile(s.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600) // #nosec G304 -- path is operator-supplied gateway config, not attacker-controlled input
	if err != nil {
		return fmt.Errorf("open audit log for append: %w", err)
	}
	defer func() { _ = f.Close() }()
	if err := f.Sync(); err != nil {
		return fmt.Errorf("fsync audit log: %w", err)
	}
	return nil
}

// Verify walks the log from the beginning and reports the first break in
// the hash chain, or nil if the whole chain is intact.
func (s *FileStore) Verify(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := os.Open(s.path)
	if err != nil {
		return fmt.Errorf("open audit log: %w", err)
	}
	defer func() { _ = f.Close() }()

	reader := bufio.NewReader(f)
	prevHash := ""
	lineNum := 0
	for {
		lineNum++
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			var rec types.ActionRecord
			if unmarshalErr := json.Unmarshal(line, &rec); unmarshalErr != nil {
				return fmt.Errorf("line %d: not valid JSON: %w", lineNum, unmarshalErr)
			}
			if rec.PrevHash != prevHash {
				return fmt.Errorf("line %d: prev_hash %q does not match the hash of the previous record %q — chain is broken", lineNum, rec.PrevHash, prevHash)
			}
			claimedHash := rec.Hash
			wantHash, hashErr := computeHash(rec)
			if hashErr != nil {
				return fmt.Errorf("line %d: %w", lineNum, hashErr)
			}
			if claimedHash != wantHash {
				return fmt.Errorf("line %d: record hash %q does not match its own contents (recomputed %q) — record was altered after being written", lineNum, claimedHash, wantHash)
			}
			prevHash = claimedHash
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read audit log: %w", err)
		}
	}
	return nil
}

var _ Recorder = (*FileStore)(nil)
var _ Store = (*FileStore)(nil)
