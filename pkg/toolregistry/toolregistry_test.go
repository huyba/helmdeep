// Copyright (c) 2026 Overarching AI LLC
// SPDX-License-Identifier: Apache-2.0

package toolregistry

import "testing"

func TestNew_RejectsEmptyToolID(t *testing.T) {
	_, err := New([]Entry{{ToolID: "", Risk: RiskLow}})
	if err == nil {
		t.Fatal("New: expected an error for an empty tool id")
	}
}

func TestNew_RejectsInvalidRiskRating(t *testing.T) {
	_, err := New([]Entry{{ToolID: "kb.search", Risk: RiskRating("apocalyptic")}})
	if err == nil {
		t.Fatal("New: expected an error for an invalid risk rating")
	}
}

func TestNew_RejectsDuplicateToolID(t *testing.T) {
	_, err := New([]Entry{
		{ToolID: "kb.search", Risk: RiskLow},
		{ToolID: "kb.search", Risk: RiskHigh},
	})
	if err == nil {
		t.Fatal("New: expected an error for a duplicate tool id")
	}
}

func TestNew_AcceptsAllFourRiskRatings(t *testing.T) {
	entries := []Entry{
		{ToolID: "a", Risk: RiskLow},
		{ToolID: "b", Risk: RiskMedium},
		{ToolID: "c", Risk: RiskHigh},
		{ToolID: "d", Risk: RiskCritical},
	}
	r, err := New(entries)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	for _, e := range entries {
		got, ok := r.Lookup(e.ToolID)
		if !ok || got.Risk != e.Risk {
			t.Fatalf("Lookup(%q) = %+v, %v; want %+v, true", e.ToolID, got, ok, e)
		}
	}
}

func TestLookup_UnregisteredToolReturnsFalse(t *testing.T) {
	r, err := New([]Entry{{ToolID: "kb.search", Risk: RiskLow}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, ok := r.Lookup("does.not.exist"); ok {
		t.Fatal("Lookup: expected false for an unregistered tool id")
	}
}
