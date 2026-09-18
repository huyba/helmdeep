// Copyright (c) 2026 Overarching AI LLC
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"

	"github.com/huyba/helmdeep/pkg/types"
)

// StaticRegistry is a Registry over a fixed set of Upstreams, built once at
// startup from config. It builds its tool→upstream index by calling
// ListTools on every upstream up front; if the upstream tool catalog can
// change at runtime, call Refresh.
type StaticRegistry struct {
	upstreams []Upstream
	byTool    map[string]Upstream
	tools     []types.Tool
}

// NewStaticRegistry builds a StaticRegistry and populates its tool index by
// calling ListTools on every upstream. An upstream that's unreachable at
// startup makes this fail loudly rather than silently serving an empty
// tool list for it — an operator should know immediately, not discover it
// when an agent's tools/list comes back short.
func NewStaticRegistry(ctx context.Context, upstreams []Upstream) (*StaticRegistry, error) {
	r := &StaticRegistry{upstreams: upstreams, byTool: map[string]Upstream{}}
	if err := r.Refresh(ctx); err != nil {
		return nil, err
	}
	return r, nil
}

// Refresh re-queries every upstream's tool catalog and rebuilds the index.
func (r *StaticRegistry) Refresh(ctx context.Context) error {
	index := map[string]Upstream{}
	var all []types.Tool
	for _, u := range r.upstreams {
		tools, err := u.ListTools(ctx)
		if err != nil {
			return err
		}
		for _, t := range tools {
			index[t.Name] = u
			all = append(all, t)
		}
	}
	r.byTool = index
	r.tools = all
	return nil
}

func (r *StaticRegistry) Upstreams() []Upstream { return r.upstreams }

func (r *StaticRegistry) Resolve(toolName string) (Upstream, bool) {
	u, ok := r.byTool[toolName]
	return u, ok
}

func (r *StaticRegistry) Tools() []types.Tool { return r.tools }

var _ Registry = (*StaticRegistry)(nil)
