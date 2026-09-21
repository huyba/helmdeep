// Copyright (c) 2026 Overarching AI LLC
// SPDX-License-Identifier: Apache-2.0

package audit

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/huyba/helmdeep/pkg/types"
)

func testRecord(tool string, outcome types.RecordOutcome) types.ActionRecord {
	return types.ActionRecord{
		Timestamp: time.Now().UTC(),
		Subject:   types.Subject{ID: "agent:test", Kind: types.SubjectKindAgent},
		Action:    types.Action{Type: "tool_call", Tool: tool},
		Decision:  types.DecisionResponse{Outcome: types.OutcomeAllow, PolicyID: "test"},
		Outcome:   outcome,
	}
}

func TestFileStore_AppendAndVerify(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.log")
	s, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}

	for i := 0; i < 5; i++ {
		if err := s.Append(context.Background(), testRecord("kb.search", types.RecordOutcomeAllowed)); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}

	if err := s.Verify(context.Background()); err != nil {
		t.Fatalf("Verify on an untampered chain: %v", err)
	}
}

func TestFileStore_ChainsRecordsTogether(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.log")
	s, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	if err := s.Append(context.Background(), testRecord("a", types.RecordOutcomeAllowed)); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := s.Append(context.Background(), testRecord("b", types.RecordOutcomeDenied)); err != nil {
		t.Fatalf("Append: %v", err)
	}

	lines := readLines(t, path)
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2", len(lines))
	}
	var first, second types.ActionRecord
	mustUnmarshal(t, lines[0], &first)
	mustUnmarshal(t, lines[1], &second)

	if first.PrevHash != "" {
		t.Fatalf("first record PrevHash = %q, want empty (genesis)", first.PrevHash)
	}
	if first.Hash == "" {
		t.Fatal("first record Hash is empty")
	}
	if second.PrevHash != first.Hash {
		t.Fatalf("second.PrevHash = %q, want %q (first.Hash)", second.PrevHash, first.Hash)
	}
}

func TestFileStore_VerifyDetectsTamperedField(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.log")
	s, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	if err := s.Append(context.Background(), testRecord("kb.search", types.RecordOutcomeAllowed)); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := s.Append(context.Background(), testRecord("kb.search", types.RecordOutcomeAllowed)); err != nil {
		t.Fatalf("Append: %v", err)
	}

	// Tamper with the first record's Action.Tool without recomputing its
	// hash — this simulates someone editing the file directly.
	lines := readLines(t, path)
	var rec types.ActionRecord
	mustUnmarshal(t, lines[0], &rec)
	rec.Action.Tool = "salesforce.delete_everything"
	tampered, err := json.Marshal(rec)
	if err != nil {
		t.Fatalf("marshal tampered record: %v", err)
	}
	lines[0] = tampered
	writeLines(t, path, lines)

	if err := s.Verify(context.Background()); err == nil {
		t.Fatal("Verify: expected an error on a tampered record, got nil")
	}
}

func TestFileStore_VerifyDetectsDeletedRecord(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.log")
	s, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	for i := 0; i < 3; i++ {
		if err := s.Append(context.Background(), testRecord("kb.search", types.RecordOutcomeAllowed)); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}

	// Delete the middle record. The tail record's PrevHash now points to a
	// hash that no longer appears anywhere in the file.
	lines := readLines(t, path)
	writeLines(t, path, append(lines[:1], lines[2:]...))

	if err := s.Verify(context.Background()); err == nil {
		t.Fatal("Verify: expected an error after deleting a record, got nil")
	}
}

func TestFileStore_RecoversChainAcrossRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.log")

	s1, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	if err := s1.Append(context.Background(), testRecord("kb.search", types.RecordOutcomeAllowed)); err != nil {
		t.Fatalf("Append: %v", err)
	}

	// Simulate a restart: a fresh FileStore over the same file must
	// continue the chain, not start a new one that would make the old
	// records look orphaned.
	s2, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("NewFileStore (reopen): %v", err)
	}
	if err := s2.Append(context.Background(), testRecord("kb.search", types.RecordOutcomeAllowed)); err != nil {
		t.Fatalf("Append after reopen: %v", err)
	}

	if err := s2.Verify(context.Background()); err != nil {
		t.Fatalf("Verify across restart: %v", err)
	}
}

func TestFileStore_VerifyDetectsReorderedRecords(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.log")
	s, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	for _, tool := range []string{"a", "b", "c"} {
		if err := s.Append(context.Background(), testRecord(tool, types.RecordOutcomeAllowed)); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}

	// Swap the first two records. Each one's prev_hash still points at the
	// real hash of whatever preceded it originally — which, after the
	// swap, is no longer what's actually there.
	lines := readLines(t, path)
	lines[0], lines[1] = lines[1], lines[0]
	writeLines(t, path, lines)

	if err := s.Verify(context.Background()); err == nil {
		t.Fatal("Verify: expected an error after reordering records, got nil")
	}
}

func TestFileStore_VerifyDetectsForgedRecordWithWrongPrevHash(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.log")
	s, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	if err := s.Append(context.Background(), testRecord("a", types.RecordOutcomeAllowed)); err != nil {
		t.Fatalf("Append: %v", err)
	}

	// Build a record that is internally self-consistent — its own Hash is
	// correctly computed from its own contents plus its (fabricated)
	// PrevHash — but whose PrevHash doesn't match the real chain's actual
	// tail. This is what an attacker with write access but no way to
	// recompute the real chain would produce: a plausible-looking record
	// appended without knowing (or bothering to match) what really came
	// before it.
	forged := testRecord("forged", types.RecordOutcomeAllowed)
	forged.PrevHash = "0000000000000000000000000000000000000000000000000000000000000000000000000000"
	hash, err := computeHash(forged)
	if err != nil {
		t.Fatalf("computeHash: %v", err)
	}
	forged.Hash = hash

	line, err := json.Marshal(forged)
	if err != nil {
		t.Fatalf("marshal forged record: %v", err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600) // #nosec G304 -- test's own t.TempDir() fixture
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		t.Fatalf("write forged record: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	if err := s.Verify(context.Background()); err == nil {
		t.Fatal("Verify: expected an error for a forged record with a wrong prev_hash, got nil")
	}
}

func readLines(t *testing.T, path string) [][]byte {
	t.Helper()
	data, err := os.ReadFile(path) // #nosec G304 -- path is always this test's own t.TempDir() fixture, not external input
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var lines [][]byte
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := append([]byte(nil), scanner.Bytes()...)
		if len(line) > 0 {
			lines = append(lines, line)
		}
	}
	return lines
}

func writeLines(t *testing.T, path string, lines [][]byte) {
	t.Helper()
	var buf bytes.Buffer
	for _, l := range lines {
		buf.Write(l)
		buf.WriteByte('\n')
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func mustUnmarshal(t *testing.T, data []byte, v any) {
	t.Helper()
	if err := json.Unmarshal(data, v); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
}

// Check must succeed on a healthy store and — the property a readiness probe
// actually needs — must not write anything: a byte written here would be a
// forged-looking record breaking the hash chain.
func TestFileStore_CheckPassesAndWritesNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.log")
	s, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	if err := s.Append(context.Background(), testRecord("kb.search", types.RecordOutcomeAllowed)); err != nil {
		t.Fatalf("Append: %v", err)
	}
	before, err := os.ReadFile(path) // #nosec G304 -- test-controlled temp path
	if err != nil {
		t.Fatalf("read log: %v", err)
	}

	for i := 0; i < 3; i++ {
		if err := s.Check(context.Background()); err != nil {
			t.Fatalf("Check %d on a healthy store: %v", i, err)
		}
	}

	after, err := os.ReadFile(path) // #nosec G304 -- test-controlled temp path
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("Check changed the log file's contents")
	}
	if err := s.Verify(context.Background()); err != nil {
		t.Fatalf("chain no longer verifies after Check: %v", err)
	}
}

// A store whose directory becomes unwritable (read-only mount, detached
// volume) must report unhealthy. Skipped as root, which ignores permission
// bits and would open the file anyway.
func TestFileStore_CheckFailsWhenDirectoryUnwritable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: permission bits are not enforced")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.log")
	s, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	if err := os.Chmod(path, 0o400); err != nil { // read-only file: opening for append must now fail
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })

	if err := s.Check(context.Background()); err == nil {
		t.Fatal("Check passed on a log file that cannot be opened for append")
	}
}

func TestFileStore_CheckHonoursCancelledContext(t *testing.T) {
	s, err := NewFileStore(filepath.Join(t.TempDir(), "audit.log"))
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.Check(ctx); err == nil {
		t.Fatal("Check ignored an already-cancelled context")
	}
}
