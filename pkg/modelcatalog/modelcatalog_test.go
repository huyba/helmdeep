// Copyright (c) 2026 Overarching AI LLC
// SPDX-License-Identifier: Apache-2.0

package modelcatalog

import (
	"reflect"
	"testing"
)

func TestNew_RejectsEmptyProviderOrModel(t *testing.T) {
	if _, err := New([]Entry{{Provider: "", Model: "gpt-4o-chat"}}); err == nil {
		t.Fatal("New: expected an error for an empty provider")
	}
	if _, err := New([]Entry{{Provider: "azure-openai", Model: ""}}); err == nil {
		t.Fatal("New: expected an error for an empty model")
	}
}

func TestNew_RejectsDuplicateProviderModelPair(t *testing.T) {
	_, err := New([]Entry{
		{Provider: "azure-openai", Model: "gpt-4o-chat"},
		{Provider: "azure-openai", Model: "gpt-4o-chat"},
	})
	if err == nil {
		t.Fatal("New: expected an error for a duplicate (provider, model) pair")
	}
}

func TestNew_AllowsSameModelNameAcrossDifferentProviders(t *testing.T) {
	// Provider+model is the join key precisely because a model string
	// alone isn't unique across providers.
	_, err := New([]Entry{
		{Provider: "azure-openai", Model: "shared-name"},
		{Provider: "anthropic", Model: "shared-name"},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
}

func TestNew_AcceptsDistinctEntries(t *testing.T) {
	entries := []Entry{
		{Provider: "azure-openai", Model: "gpt-4o-chat", Approved: true, Residency: "us"},
		{Provider: "anthropic", Model: "claude-haiku-4-5-20251001", Approved: false, AllowedDataClasses: []string{"public"}},
	}
	r, err := New(entries)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	for _, e := range entries {
		got, ok := r.Lookup(e.Provider, e.Model)
		if !ok || !reflect.DeepEqual(got, e) {
			t.Fatalf("Lookup(%q, %q) = %+v, %v; want %+v, true", e.Provider, e.Model, got, ok, e)
		}
	}
}

func TestLookup_UnregisteredPairReturnsFalse(t *testing.T) {
	r, err := New([]Entry{{Provider: "azure-openai", Model: "gpt-4o-chat", Approved: true}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, ok := r.Lookup("azure-openai", "does-not-exist"); ok {
		t.Fatal("Lookup: expected false for an unregistered model")
	}
	if _, ok := r.Lookup("does-not-exist", "gpt-4o-chat"); ok {
		t.Fatal("Lookup: expected false for an unregistered provider")
	}
}
