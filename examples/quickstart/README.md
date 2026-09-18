# Quickstart

Five minutes: bring up the gateway plus four mock upstream systems, watch a
policy allow one call and deny another, then verify the audit trail.

## 1. Start it

```sh
cd examples/quickstart
docker compose up --build
```

This builds `helmdeep-gateway` and four instances of the `mock-upstream`
test fixture (`internal/mockupstream`) standing in for a knowledge base, a
supplier master record, an email inbox, and a payments API — so you can see
policy in action without wiring up real credentials or real systems. The
gateway listens on `localhost:8443`, loaded with the bundle in
`examples/policies/` (see `docs/policy-guide.md`) and two dev-only static
identities: `support-token` (agent `support-bot`, may only call
`kb.search`) and `finance-token` (agent `finance-bot`, may call the
supplier/email/payments tools).

## 2. See a tool allowed, and one denied for scope

Every request needs the headers the current MCP spec (2026-07-28) requires
— see `docs/adr/0005-mcp-protocol-compatibility.md`.

```sh
# support-bot listing tools it may call — only kb.search, not the other three
curl -s http://localhost:8443/mcp \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  -H 'MCP-Protocol-Version: 2026-07-28' \
  -H 'Mcp-Method: tools/list' \
  -H 'Authorization: Bearer support-token' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}' | jq

# support-bot calling the one tool it's allowed to call
curl -s http://localhost:8443/mcp \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  -H 'MCP-Protocol-Version: 2026-07-28' \
  -H 'Mcp-Method: tools/call' \
  -H 'Mcp-Name: kb.search' \
  -H 'Authorization: Bearer support-token' \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"kb.search","arguments":{"query":"reset my password"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}' | jq

# support-bot trying a tool outside its scope — denied, isError: true, not a crash
curl -s http://localhost:8443/mcp \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  -H 'MCP-Protocol-Version: 2026-07-28' \
  -H 'Mcp-Method: tools/call' \
  -H 'Mcp-Name: payments.wire_transfer' \
  -H 'Authorization: Bearer support-token' \
  -d '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"payments.wire_transfer","arguments":{"account_number":"ACC-1","amount_usd":100},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}' | jq
```

## 3. See the taint policy: same tool, different outcome based on where the money-routing data came from

```sh
# finance-bot fetches an account number from the TRUSTED supplier master record
curl -s http://localhost:8443/mcp \
  -H 'Content-Type: application/json' -H 'Accept: application/json, text/event-stream' \
  -H 'MCP-Protocol-Version: 2026-07-28' -H 'Mcp-Method: tools/call' -H 'Mcp-Name: supplier.get_account' \
  -H 'Authorization: Bearer finance-token' \
  -d '{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"supplier.get_account","arguments":{"supplier_id":"S1"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}' | jq
# → structuredContent.account_number is "ACC-TRUSTED-0001"

# using that exact value in a wire transfer is ALLOWED
curl -s http://localhost:8443/mcp \
  -H 'Content-Type: application/json' -H 'Accept: application/json, text/event-stream' \
  -H 'MCP-Protocol-Version: 2026-07-28' -H 'Mcp-Method: tools/call' -H 'Mcp-Name: payments.wire_transfer' \
  -H 'Authorization: Bearer finance-token' \
  -d '{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"payments.wire_transfer","arguments":{"account_number":"ACC-TRUSTED-0001","amount_usd":250},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}' | jq

# now read the (fake, phishing) email upstream instead
curl -s http://localhost:8443/mcp \
  -H 'Content-Type: application/json' -H 'Accept: application/json, text/event-stream' \
  -H 'MCP-Protocol-Version: 2026-07-28' -H 'Mcp-Method: tools/call' -H 'Mcp-Name: email.read_latest' \
  -H 'Authorization: Bearer finance-token' \
  -d '{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"email.read_latest","arguments":{},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}' | jq
# → structuredContent.account_number is "ACC-PHISHING-6669"

# using THAT value in a wire transfer is DENIED — same tool, same agent, same scope,
# denied purely because of where the account number traces back to
curl -s http://localhost:8443/mcp \
  -H 'Content-Type: application/json' -H 'Accept: application/json, text/event-stream' \
  -H 'MCP-Protocol-Version: 2026-07-28' -H 'Mcp-Method: tools/call' -H 'Mcp-Name: payments.wire_transfer' \
  -H 'Authorization: Bearer finance-token' \
  -d '{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"payments.wire_transfer","arguments":{"account_number":"ACC-PHISHING-6669","amount_usd":250},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}' | jq
```

## 4. Verify the audit trail is tamper-evident

```sh
docker compose exec gateway app verify-chain -config=/etc/helmdeep/config.yaml
# chain intact: /data/audit.log
```

Every call above — allowed and denied — is in that log. See
`docs/adr/0004-action-record-format.md` for the format and what "verify"
actually checks.

## Cleanup

```sh
docker compose down
```
