// Copyright (c) 2026 Overarching AI LLC
// SPDX-License-Identifier: Apache-2.0

package gateway

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/huyba/helmdeep/pkg/types"
)

// redactedPlaceholder replaces a redacted field's value in a tool result.
const redactedPlaceholder = "[REDACTED]"

// applyObligations applies every obligation the PDP attached to an
// allow_with_obligations decision, in order, before a tool result is
// returned to the agent. An obligation the gateway doesn't recognize is
// ignored rather than treated as an error — a policy author adding a new
// obligation type ahead of the gateway supporting it should not turn every
// matching call into a failure.
func applyObligations(result types.ToolResult, obligations []types.Obligation) types.ToolResult {
	for _, o := range obligations {
		if o.Type == types.ObligationRedact {
			result = redactPath(result, o.Target)
		}
	}
	return result
}

// redactPath replaces the value at a dot-separated path into a tool
// result's content with redactedPlaceholder. The path's first segment
// selects which field to operate on ("content" or "structuredContent");
// remaining segments are object keys or array indices, e.g.
// "structuredContent.account_number" or "content.0.text".
//
// This is a minimal path syntax, not JSONPath — deliberately: v1's
// obligations are simple field masks, and a fuller query language is
// scope this gateway doesn't need yet. A path that doesn't resolve (typo,
// field absent, wrong type) is a no-op, not an error: failing a whole
// response because a redaction target moved is worse than an unredacted
// field a reviewer will still see logged as an obligation that didn't
// apply — see the caller in gateway.go, which logs when a redaction
// target isn't found.
func redactPath(result types.ToolResult, path string) types.ToolResult {
	segments := strings.Split(path, ".")
	if len(segments) < 2 {
		return result
	}

	switch segments[0] {
	case "structuredContent":
		result.StructuredContent = redactInJSON(result.StructuredContent, segments[1:])
	case "content":
		result.Content = redactInJSON(result.Content, segments[1:])
	}
	return result
}

func redactInJSON(raw json.RawMessage, segments []string) json.RawMessage {
	if len(raw) == 0 {
		return raw
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return raw
	}
	if !setAtPath(v, segments, redactedPlaceholder) {
		return raw
	}
	b, err := json.Marshal(v)
	if err != nil {
		return raw
	}
	return b
}

// setAtPath navigates to the container holding the final path segment and
// replaces that entry's value in place. It reports whether it found
// something to replace.
func setAtPath(root any, segments []string, replacement any) bool {
	if len(segments) == 0 {
		return false
	}
	cur := root
	for _, seg := range segments[:len(segments)-1] {
		switch c := cur.(type) {
		case map[string]any:
			next, ok := c[seg]
			if !ok {
				return false
			}
			cur = next
		case []any:
			idx, err := strconv.Atoi(seg)
			if err != nil || idx < 0 || idx >= len(c) {
				return false
			}
			cur = c[idx]
		default:
			return false
		}
	}

	last := segments[len(segments)-1]
	switch c := cur.(type) {
	case map[string]any:
		if _, ok := c[last]; !ok {
			return false
		}
		c[last] = replacement
		return true
	case []any:
		idx, err := strconv.Atoi(last)
		if err != nil || idx < 0 || idx >= len(c) {
			return false
		}
		c[idx] = replacement
		return true
	default:
		return false
	}
}
