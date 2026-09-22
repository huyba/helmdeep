// Copyright (c) 2026 Overarching AI LLC
// SPDX-License-Identifier: Apache-2.0

package agentregistry

import (
	"reflect"
	"testing"
)

func TestNew_RejectsEmptyAgentID(t *testing.T) {
	_, err := New([]Entry{{AgentID: ""}})
	if err == nil {
		t.Fatal("New: expected an error for an empty agent id")
	}
}

func TestNew_RejectsDuplicateAgentID(t *testing.T) {
	_, err := New([]Entry{
		{AgentID: "agent:support-bot"},
		{AgentID: "agent:support-bot"},
	})
	if err == nil {
		t.Fatal("New: expected an error for a duplicate agent id")
	}
}

func TestNew_AcceptsDistinctAgents(t *testing.T) {
	entries := []Entry{
		{AgentID: "agent:support-bot", Version: "1.0.0", Owner: "support-team", Risk: "low"},
		{AgentID: "agent:finance-bot", Version: "2.1.0", Owner: "finance-team", Risk: "high", DataClasses: []string{"financial"}},
	}
	r, err := New(entries)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	for _, e := range entries {
		got, ok := r.Lookup(e.AgentID)
		if !ok || !reflect.DeepEqual(got, e) {
			t.Fatalf("Lookup(%q) = %+v, %v; want %+v, true", e.AgentID, got, ok, e)
		}
	}
}

func TestLookup_UnregisteredAgentReturnsFalse(t *testing.T) {
	r, err := New([]Entry{{AgentID: "agent:support-bot"}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, ok := r.Lookup("agent:does-not-exist"); ok {
		t.Fatal("Lookup: expected false for an unregistered agent id")
	}
}
