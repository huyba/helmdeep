package helmdeep.authz

# Aggregate-limits policy: per-tool call-rate ceilings within the gateway's
# usage window. The gateway computes context.usage.calls_in_window (and
# maintains the counter across calls); this policy only compares it to a
# threshold — it holds no state of its own. See
# docs/adr/0002-policy-engine-choice.md for why that split matters, and
# internal/gateway/usage.go for what the gateway actually tracks in v1
# (call rate only — cumulative cost and session budget are always zero
# until a real cost model exists).
rate_limits := {"kb.search": 3}

default limit_exceeded := false

limit_exceeded if {
	limit := rate_limits[input.action.tool]
	input.context.usage.calls_in_window > limit
}
