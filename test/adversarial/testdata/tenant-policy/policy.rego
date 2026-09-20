# Test-only fixture for TestAgentCannotAccessResourceOutsideItsTenantPartition.
# Demonstrates that the policy schema and engine support resource/tenant
# partitioning via types.Action.Resource — not part of the public example
# bundle (examples/policies/), which doesn't need this dimension for its
# own story. See ARCHITECTURE.md and the test's own comment for what's
# schema-capable today versus wired end-to-end through the gateway.
package helmdeep.authz

default decision := {"outcome": "deny", "policy_id": "default.deny", "reason": "no matching rule"}

tenant_of := {"agent:acme-bot": "tenant:acme", "agent:globex-bot": "tenant:globex"}

decision := {
	"outcome": "allow",
	"policy_id": "tenant.allow",
	"reason": "resource is within the agent's own tenant partition",
} if {
	tenant_of[input.subject.id] == input.action.resource
}

decision := {
	"outcome": "deny",
	"policy_id": "tenant.deny",
	"reason": sprintf("agent %v may not access resource %v outside its tenant partition", [input.subject.id, input.action.resource]),
} if {
	tenant_of[input.subject.id] != input.action.resource
}
