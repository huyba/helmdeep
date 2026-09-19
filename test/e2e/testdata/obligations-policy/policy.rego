# Test-only fixture — not the public example bundle (examples/policies/).
# Exists solely to give TestObligationsRedactField in gateway_test.go
# something that actually returns allow_with_obligations, since the
# public example intentionally keeps scope/taint/limits as its whole
# story and doesn't demonstrate redaction. See docs/policy-guide.md.
package helmdeep.authz

default decision := {"outcome": "deny", "policy_id": "default.deny", "reason": "no matching rule"}

decision := {
	"outcome": "allow_with_obligations",
	"policy_id": "test.redact_account_number",
	"reason": "allowed, but the account number is redacted before it reaches the agent",
	"obligations": [{"type": "redact", "target": "structuredContent.account_number"}],
} if {
	input.action.tool == "supplier.get_account"
}
