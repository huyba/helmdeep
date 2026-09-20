# Test-only fixture: allows every action, regardless of anything in the
# request. Used to prove a tool result's content passes through unchanged
# on the allow path, as distinct from a deny-all fixture proving the
# content is never even reached.
package helmdeep.authz

decision := {"outcome": "allow", "policy_id": "test.allow_all", "reason": "test fixture: always allow"}
