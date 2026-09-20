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
// v1 tracks call rate only, as a fixed window per subject (a counter that
// resets once `window` has elapsed since it was first incremented) rather
// than a sliding log of every call timestamp. That's a deliberate accuracy
// tradeoff: a fixed window can undercount slightly right at its boundary
// (a burst split across the reset instant isn't capped as tightly as a
// true sliding window would cap it), in exchange for O(1) work per call
// and O(active subjects) memory. A sliding log was the original design and
// was replaced after benchmarking (test/e2e/gateway_bench_test.go) showed
// it re-scans a subject's entire call history on every single call — an
// aggregate-limit mechanism whose own cost grows with call volume is a
// bad trade for the exact thing it exists to bound.
//
// CumulativeCost and SessionBudget are always reported as zero: there is
// no cost model wired up yet (that's Model Gateway territory — pkg/modelgw
// is a stub). The fields exist on types.Usage now so a policy can
// reference them without a schema change later; until a real cost model
// lands, any threshold a policy sets on them is trivially satisfied. This
// is stated here, not hidden: see docs/policy-guide.md.
//
// Safe for concurrent use.
type usageTracker struct {
	mu     sync.Mutex
	window time.Duration
	state  map[string]*windowState
}

type windowState struct {
	start time.Time
	count int
}

const defaultUsageWindow = time.Minute

func newUsageTracker() *usageTracker {
	return &usageTracker{
		window: defaultUsageWindow,
		state:  map[string]*windowState{},
	}
}

// recordAndCount records a call attempt for subjectID at now and returns
// the count for its current window, including this attempt. It's called
// once per tools/call, before asking the PDP for a decision — a denied
// call still counts against the rate, deliberately: a rate limit that
// resets on every denial isn't a rate limit.
func (t *usageTracker) recordAndCount(subjectID string, now time.Time) int {
	t.mu.Lock()
	defer t.mu.Unlock()

	s, ok := t.state[subjectID]
	if !ok || now.Sub(s.start) >= t.window {
		s = &windowState{start: now}
		t.state[subjectID] = s
	}
	s.count++
	return s.count
}

// peek reports the current call count without recording a new attempt —
// used for tools/list visibility checks, which aren't call attempts and
// must not consume rate-limit budget just by being asked about.
func (t *usageTracker) peek(subjectID string, now time.Time) int {
	t.mu.Lock()
	defer t.mu.Unlock()

	s, ok := t.state[subjectID]
	if !ok || now.Sub(s.start) >= t.window {
		return 0
	}
	return s.count
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
