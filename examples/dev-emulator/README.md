# Dev emulator

Milestone M4's "local emulator running the same runtime contract as
production" (`docs/01-requirements.md` FR-B3) — one command, real JWT
identity, real policy, real audit log, exactly like `examples/quickstart/`
except identity is a real Session Identity Token you mint for yourself
(`cmd/dev-issuer`) instead of one of a few shared static tokens. See
`docs/adr/0010-python-sdk-and-dev-emulator.md` for why this is a second
example rather than a change to quickstart's own, already-documented
static-token flow.

## Run it

```sh
# from this directory
docker compose up --build
```

Four mock upstreams (knowledgebase, suppliermaster, email, payments), a
`dev-issuer` on `:8444`, and the gateway on `:8443` — all with
`examples/policies/`'s scope/taint/rate-limit rules already loaded.

## Get your own identity and call a tool

```sh
# Mint a Session Identity Token for yourself — any agent id you like.
# examples/policies/scope.rego only recognizes agent:support-bot and
# agent:finance-bot; use one of those to see an allow, anything else to
# see a clean, real deny.
SIT=$(curl -s -X POST http://localhost:8444/issue \
  -d '{"agent":"agent:support-bot","delegation_chain":["user:you@example.com"]}' \
  | python3 -c 'import sys,json;print(json.load(sys.stdin)["token"])')

curl -s http://localhost:8443/mcp \
  -H 'Content-Type: application/json' -H 'Accept: application/json, text/event-stream' \
  -H "Authorization: Bearer $SIT" \
  -H 'MCP-Protocol-Version: 2026-07-28' -H 'Mcp-Method: tools/call' -H 'Mcp-Name: kb.search' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"kb.search","arguments":{"query":"reset my password"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}' | jq
```

Or, from Python (`sdk/python`):

```python
from helmdeep import Client
from helmdeep.devissuer import issue_sit

sit = issue_sit("http://localhost:8444", agent="agent:support-bot")
client = Client("http://localhost:8443/mcp", credential=sit)
print(client.call("kb.search", {"query": "reset my password"}))
```

## What's different from `examples/quickstart/`

Only `identity:` in `config.yaml` — `jwt` here instead of
`static_tokens` — and the added `dev-issuer` service. Same policies, same
mock upstreams, same Tool Registry entries. Pick quickstart to see the
whole system fastest with zero setup; pick this to develop against real,
per-developer identity the way a deployed gateway actually works.

## Known limitations

Same as `examples/quickstart/README.md`'s and `deploy/k8s/README.md`'s —
this is dev/demo infrastructure. `cmd/dev-issuer` is explicitly
DEVELOPMENT AND TEST ONLY (see its own doc comment): the signing key is
fresh and unpersisted every restart, and the human hop in
`delegation_chain` is asserted, never verified against a real IdP.
