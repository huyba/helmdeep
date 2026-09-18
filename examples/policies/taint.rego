package helmdeep.authz

# Provenance / taint policy: some arguments must derive from a trusted
# source regardless of who's calling or whether the call is in scope. This
# is the motivating example from ARCHITECTURE.md: a wire transfer's
# account_number must come from the supplier master record, never from an
# inbound email — the gateway (not this policy) is responsible for
# labeling each argument's real provenance; this policy only reads the
# label.
tainted_requirements := {"payments.wire_transfer": {"argument": "account_number"}}

default taint_violation := false

taint_violation if {
	req := tainted_requirements[input.action.tool]
	arg := input.action.arguments[req.argument]
	not arg.provenance.trusted
}
