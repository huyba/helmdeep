# Adversarial tests

This suite is documentation as much as verification. Every other test
package in this repo proves a mechanism works; this one proves the
mechanisms defend against *named threats* — each test is titled after the
attack it defeats, not the code path it exercises, and maps (where one
applies cleanly) to an [OWASP Top 10 for Agentic Applications
(2026)](https://genai.owasp.org/) ASI code.

**On the ASI codes:** the mapping in each test's comment comes from
secondary sources — blog summaries of the OWASP document — not the primary
PDF, which wasn't directly accessible while writing these. Treat each
`(unverified, see README)` code as "best available guess, worth five
minutes to confirm," not as a citation. If a code turns out to be wrong,
fix the comment; the test itself doesn't depend on the code being right.

## What's covered

| Test | Threat |
|---|---|
| `TestTaintedParameterCannotForceAnUnauthorizedPayment` | An agent routes payment details sourced from an untrusted channel (email) instead of the trusted system of record |
| `TestAgentCannotReachToolOutsideItsScope` | An agent reaches for a tool its task never granted it — denied, and invisible in `tools/list` |
| `TestAgentCannotAccessResourceOutsideItsTenantPartition` | Cross-tenant/region resource access (policy-engine level; see the test's own comment on what's wired end-to-end today vs. schema-capable) |
| `TestAggregateLimitExhaustionIsDenied` | An agent within scope hammers a tool past its configured rate ceiling |
| `TestMaliciousToolDescriptionIsNeverInterpretedAsInstructions` | A compromised MCP server's tool description contains an instruction aimed at whoever reads it |
| `TestMaliciousToolResultIsNeverInterpretedAsInstructions` | Same threat, via a tool's response content instead of its description |
| `TestArgumentCannotInjectIntoPolicyEvaluation` | A tool argument crafted to look like policy-language syntax |
| `TestDelegationChainNeverWidensScope` | Delegated authority widening beyond what was granted — see the test's comment: delegation itself doesn't exist yet, so this documents the current (empty-chain) invariant, not a real widening-rejection |

## What this suite is not

It is not a fuzzer, a red-team exercise, or a claim of completeness. Every
test here encodes a threat someone already thought of. The exercise that
actually finds the gaps in this list — sitting down and trying to think of
three ways around this gateway that aren't on it — has to happen by hand,
by someone who didn't write the code. This suite is where the result of
that exercise belongs once it happens, not a substitute for it.
