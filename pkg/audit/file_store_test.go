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
