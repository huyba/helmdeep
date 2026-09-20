# Test-only fixture: denies every action, regardless of anything in the
# request. Used where a test needs to prove that some other input (a
# malicious tool description, for instance) has no influence on the
# decision — if policy said allow, the test couldn't tell whether that was
# because the malicious content worked or because the tool was genuinely
# in scope.
package helmdeep.authz

decision := {"outcome": "deny", "policy_id": "test.deny_all", "reason": "test fixture: always deny"}
