// Copyright (c) 2026 Overarching AI LLC
// SPDX-License-Identifier: Apache-2.0

package gateway

import (
	"sync"
	"time"

	"github.com/huyba/helmdeep/pkg/types"
)

// provenanceCache correlates a value the gateway has seen returned by a
// tool call with the provenance of the upstream that returned it, so a
// later tool call that reuses that value as an argument can be tagged with
// where it really came from — without trusting anything the agent or model
// says about it. The gateway is the only party well-positioned to do this:
// it mediates every tool call and result, so it can build this correlation
// itself from data it directly observed, rather than from a self-report.
//
// Scope, stated plainly: this tracks top-level string argument values only.
// A value nested inside an object or array argument, or a non-string value,
// is not tracked in v1 and always resolves to the untrusted default (see
// resolve). This is a real limitation, not an oversight — see
// ARCHITECTURE.md and docs/policy-guide.md. It is also, by construction,
// fail-closed: anything the gateway can't positively trace to a trusted
// source is untrusted, never the reverse.
//
// The cache is scoped per subject (agent identity) so one agent's tool
// results can never taint another agent's calls, deliberately crude about
// bounding its own size (a full clear on overflow, not an LRU), and
// TTL-based so a stale correlation doesn't outlive its usefulness
// indefinitely. Safe for concurrent use.
type provenanceCache struct {
	mu        sync.Mutex
	ttl       time.Duration
	maxSize   int
	bySubject map[string]map[string]cachedProvenance
}

type cachedProvenance struct {
	provenance types.Provenance
	expiresAt  time.Time
}

const (
	provenanceCacheTTL     = 15 * time.Minute
	provenanceCacheMaxSize = 256 // per subject; overflow clears that subject's cache
)

func newProvenanceCache() *provenanceCache {
	return &provenanceCache{
		ttl:       provenanceCacheTTL,
		maxSize:   provenanceCacheMaxSize,
		bySubject: map[string]map[string]cachedProvenance{},
	}
}

// record associates value with prov for subjectID, to be picked up by a
// later resolve call for the same subject and exact value.
func (c *provenanceCache) record(subjectID, value string, prov types.Provenance, now time.Time) {
	if value == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	m, ok := c.bySubject[subjectID]
	if !ok {
		m = map[string]cachedProvenance{}
		c.bySubject[subjectID] = m
	}
	if len(m) >= c.maxSize {
		// Deliberately crude: see the type's doc comment. Losing
		// correlations early is safe (they fall back to untrusted); an
		// unbounded map is not.
		m = map[string]cachedProvenance{}
		c.bySubject[subjectID] = m
	}
	m[value] = cachedProvenance{provenance: prov, expiresAt: now.Add(c.ttl)}
}

// resolve returns the provenance recorded for value under subjectID, or the
// untrusted default if there is no live entry. It never returns an error:
// "we don't know where this came from" is exactly the untrusted case, not
// a failure.
func (c *provenanceCache) resolve(subjectID, value string, now time.Time) types.Provenance {
	c.mu.Lock()
	defer c.mu.Unlock()

	if m, ok := c.bySubject[subjectID]; ok {
		if cp, ok := m[value]; ok && now.Before(cp.expiresAt) {
			return cp.provenance
		}
	}
	return untrustedProvenance
}

// untrustedProvenance is the fail-closed default for any value the gateway
// cannot positively trace to a configured, trusted upstream.
var untrustedProvenance = types.Provenance{Source: "unspecified", Trusted: false}

// recordTopLevelStrings walks the top-level fields of a decoded JSON object
// or array and records every string value found, tagged with prov. Nested
// objects/arrays are not descended into — see the package-level scope note.
func (c *provenanceCache) recordTopLevelStrings(subjectID string, decoded any, prov types.Provenance, now time.Time) {
	switch v := decoded.(type) {
	case map[string]any:
		for _, val := range v {
			if s, ok := val.(string); ok {
				c.record(subjectID, s, prov, now)
			}
		}
	case []any:
		for _, val := range v {
			if s, ok := val.(string); ok {
				c.record(subjectID, s, prov, now)
			}
		}
	case string:
		c.record(subjectID, v, prov, now)
	}
}
