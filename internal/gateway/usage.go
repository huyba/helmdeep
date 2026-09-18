// Copyright (c) 2026 Overarching AI LLC
// SPDX-License-Identifier: Apache-2.0

package gateway

import (
	"sync"
	"time"

	"github.com/huyba/helmdeep/pkg/types"
)

// usageTracker maintains the running counters a policy needs to enforce
// aggregate limits. It owns state so policy doesn't have to — see
// docs/adr/0002-policy-engine-choice.md and types.Usage's doc comment.
//
// v1 tracks call rate only (calls within a sliding window). CumulativeCost
// and SessionBudget are always reported as zero: there is no cost model
// wired up yet (that's Model Gateway territory — pkg/modelgw is a stub).
// The fields exist on types.Usage now so a policy can reference them
// without a schema change later; until a real cost model lands, any
// threshold a policy sets on them is trivially satisfied. This is stated
// here, not hidden: see docs/policy-guide.md.
//
// Safe for concurrent use.
type usageTracker struct {
	mu     sync.Mutex
	window time.Duration
	calls  map[string][]time.Time // subject ID -> call timestamps within the window
}

const defaultUsageWindow = time.Minute

func newUsageTracker() *usageTracker {
	return &usageTracker{
		window: defaultUsageWindow,
		calls:  map[string][]time.Time{},
	}
}

// recordAndCount records a call attempt for subjectID at now, prunes
// timestamps that have fallen outside the window, and returns the count
// including this attempt. It's called once per tools/call, before asking
// the PDP for a decision — a denied call still counts against the rate,
// deliberately: a rate limit that resets on every denial isn't a rate
// limit.
func (t *usageTracker) recordAndCount(subjectID string, now time.Time) int {
	t.mu.Lock()
	defer t.mu.Unlock()

	cutoff := now.Add(-t.window)
	kept := t.calls[subjectID][:0]
	for _, ts := range t.calls[subjectID] {
		if ts.After(cutoff) {
			kept = append(kept, ts)
		}
	}
	kept = append(kept, now)
	t.calls[subjectID] = kept
	return len(kept)
}

// peek reports the current call count without recording a new attempt —
// used for tools/list visibility checks, which aren't call attempts and
// must not consume rate-limit budget just by being asked about.
func (t *usageTracker) peek(subjectID string, now time.Time) int {
	t.mu.Lock()
	defer t.mu.Unlock()

	cutoff := now.Add(-t.window)
	count := 0
	for _, ts := range t.calls[subjectID] {
		if ts.After(cutoff) {
			count++
		}
	}
	return count
}

func (t *usageTracker) usage(subjectID string, now time.Time) types.Usage {
	return types.Usage{
		CallsInWindow: t.recordAndCount(subjectID, now),
		WindowSeconds: int(t.window.Seconds()),
	}
}

func (t *usageTracker) usagePeek(subjectID string, now time.Time) types.Usage {
	return types.Usage{
		CallsInWindow: t.peek(subjectID, now),
		WindowSeconds: int(t.window.Seconds()),
	}
}
