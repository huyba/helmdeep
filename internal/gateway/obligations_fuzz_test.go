// Copyright (c) 2026 Overarching AI LLC
// SPDX-License-Identifier: Apache-2.0

package gateway

import (
	"testing"

	"github.com/huyba/helmdeep/pkg/types"
)

// FuzzRedactPath throws arbitrary JSON content and arbitrary path strings
// at redactPath. Both are meaningfully untrusted: content comes from an
// upstream MCP server (not the agent, but not something this repo
// controls either), and the path comes from a policy author's
// `obligations` config — a typo or a deliberately adversarial path
// ("../../etc", deeply nested indices, non-numeric array indices) must
// never panic the gateway; per redactPath's own doc comment, a path that
// doesn't resolve is defined to be a no-op, not an error, and this is
// where that contract gets exercised against inputs a human wouldn't
// think to write by hand.
func FuzzRedactPath(f *testing.F) {
	f.Add([]byte(`{"account_number":"123"}`), "structuredContent.account_number")
	f.Add([]byte(`{"a":{"b":{"c":"d"}}}`), "structuredContent.a.b.c")
	f.Add([]byte(`[1,2,3]`), "content.0")
	f.Add([]byte(`not json`), "content.x")
	f.Add([]byte(`{}`), "")
	f.Add([]byte(`null`), "structuredContent")
	f.Add([]byte(`{"a":[1,2,3]}`), "structuredContent.a.99999999999999999999")
	f.Add([]byte(`{"a":"b"}`), "structuredContent.a.b.c.d.e.f")

	f.Fuzz(func(t *testing.T, content []byte, path string) {
		result := types.ToolResult{
			Content:           content,
			StructuredContent: content,
		}
		_ = redactPath(result, path) // must not panic, regardless of content or path
	})
}
