package helmdeep.authz

# Scope policy: which tools each agent may call. Real deployments would
# likely source this from the Tool Registry (docs/02-architecture.md)
# rather than hardcoding it in Rego, but for a self-contained example this
# keeps everything readable in one place — see docs/policy-guide.md.
allowed_tools := {
	"agent:support-bot": {"kb.search"},
	"agent:finance-bot": {"supplier.get_account", "email.read_latest", "payments.wire_transfer"},
}

default scope_allowed := false

scope_allowed if {
	input.action.tool in allowed_tools[input.subject.id]
}
