package helmdeep.authz

# Milestone M2 demonstration: policy driven by the Tool Registry's own
# governance metadata (docs/05-tool-gateway.md §1, pkg/toolregistry),
# rather than examples/policies/scope.rego's per-agent, per-tool-ID
# allow-list. input.action.risk, input.action.data_classes, and
# input.action.required_scopes are populated by the gateway from the
# tool's registry entry — never asserted by the tool call itself. See
# docs/adr/0008-tool-registry.md.
#
# This is a separate bundle from examples/policies/ on purpose: that
# bundle is what the quickstart and most of the e2e suite already
# exercise, and this repo's own don't-change-a-working-example-for-one-
# more-demonstration convention (see docs/policy-guide.md) argues for a
# second, focused bundle over folding new rules into the first.
#
# Two rules:
#
#   1. A tool whose data_classes includes "pii" requires the caller to
#      hold every one of the tool's required_scopes (input.subject.scopes,
#      sourced from the caller's SIT `scope` claim — docs/04-identity-authz.md
#      §1.1). Missing even one scope is a deny, regardless of risk.
#   2. A tool rated "high" or "critical" risk is allowed but its result is
#      redacted (obligations, docs/02-architecture.md) pending the
#      approval workflow doc 05 §1.1 describes — not implemented in Phase
#      0, so an obligation is the enforcement doc 05 gets today.
#
# Everything else — low/medium risk, no PII — is allowed outright.

default decision := {
	"outcome": "deny",
	"policy_id": "default.deny",
	"reason": "no matching rule (fail closed)",
}

decision := {
	"outcome": "deny",
	"policy_id": "registry.pii_scope_required",
	"reason": sprintf("%v handles data class 'pii'; agent %v is missing a required scope", [input.action.tool, input.subject.id]),
} if {
	has_pii
	count(missing_scopes) > 0
}

decision := {
	"outcome": "allow_with_obligations",
	"policy_id": "registry.high_risk_redacted",
	"obligations": [{"type": "redact", "target": "structuredContent.notes"}],
	"reason": sprintf("%v is risk tier %q: allowed, result redacted pending review", [input.action.tool, input.action.risk]),
} if {
	count(missing_scopes) == 0
	input.action.risk in {"high", "critical"}
}

decision := {
	"outcome": "allow",
	"policy_id": "registry.allowed",
} if {
	count(missing_scopes) == 0
	not input.action.risk in {"high", "critical"}
}

# has_pii and missing_scopes are both defined via comprehension rather than
# direct membership tests (e.g. "pii" in input.action.data_classes) so that
# an action with no data_classes or no required_scopes at all — the common
# case for a low-risk tool — produces an empty set rather than an
# undefined expression that would make every decision rule above silently
# not match. See the OPA docs on iterating a possibly-absent input field.
has_pii if {
	input.action.data_classes[_] == "pii"
}

# subject_scopes defaults to a concrete empty array when the caller's
# credential granted no scopes at all (subject.scopes is omitted from the
# JSON input entirely, not just empty) — object.get, not a bare
# input.subject.scopes reference, because "s in <undefined>" is itself
# undefined rather than false, which would silently drop that scope out of
# missing_scopes below instead of correctly counting it as missing.
subject_scopes := object.get(input.subject, "scopes", [])

required_scopes := object.get(input.action, "required_scopes", [])

missing_scopes := {s |
	s := required_scopes[_]
	not s in subject_scopes
}
