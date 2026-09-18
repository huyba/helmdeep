package helmdeep.authz

# Combines scope.rego, taint.rego, and limits.rego into the one rule the
# gateway actually queries (see docs/adr/0002-policy-engine-choice.md for
# why it's one rule producing a complete decision object, not separate
# allow/deny/obligation rules). The four rule bodies below partition every
# combination of (scope_allowed, taint_violation, limit_exceeded) with
# deny taking precedence over both other checks, and scope over taint and
# limits — an agent not in scope for a tool is denied for that reason even
# if the call would also have failed a taint or limit check.
default decision := {
	"outcome": "deny",
	"policy_id": "default.deny",
	"reason": "no matching rule (fail closed)",
}

decision := {
	"outcome": "allow",
	"policy_id": "combined.allow",
	"reason": "scope, taint, and rate-limit checks all passed",
} if {
	scope_allowed
	not taint_violation
	not limit_exceeded
}

decision := {
	"outcome": "deny",
	"policy_id": "scope.deny",
	"reason": sprintf("agent %v is not permitted to call %v", [input.subject.id, input.action.tool]),
} if {
	not scope_allowed
}

decision := {
	"outcome": "deny",
	"policy_id": "taint.deny",
	"reason": sprintf("an argument to %v does not derive from a trusted source", [input.action.tool]),
} if {
	scope_allowed
	taint_violation
}

decision := {
	"outcome": "deny",
	"policy_id": "limits.deny",
	"reason": sprintf("call-rate limit exceeded for %v", [input.action.tool]),
} if {
	scope_allowed
	not taint_violation
	limit_exceeded
}
