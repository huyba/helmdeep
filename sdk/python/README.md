# helmdeep (Python SDK)

Milestone M4's Python SDK slice of `docs/01-requirements.md` FR-B2 ("SDK in
Python + TypeScript to implement agent logic; framework-agnostic host
interface") — Python only; no TypeScript SDK exists yet. See
`docs/adr/0010-python-sdk-and-dev-emulator.md` for scope and what's
deliberately not here.

A minimal, dependency-free client for the HelmDeep Tool Gateway, speaking
the same 2026-07-28 Streamable HTTP MCP wire protocol as `pkg/mcp.Server`.

## Install (from source; not yet published to PyPI)

```sh
pip install -e sdk/python
```

## Use

```python
from helmdeep import Client

client = Client("https://gateway.internal/mcp", credential=sit)

for tool in client.list_tools():
    print(tool.name, tool.description)

result = client.call_tool("kb.search", {"query": "reset my password"})
if result.is_error:
    print("denied or failed:", result.text())
else:
    print(result.structured_content)

# Or, for scripts that would rather raise than check is_error:
answer = client.call("kb.search", {"query": "reset my password"})
```

`credential` is whatever the gateway's configured `identity.Resolver`
expects — a real Session Identity Token, or a static dev token if the
gateway is (insecurely) configured that way. This SDK never mints or
verifies credentials; see `helmdeep.devissuer.issue_sit` below for getting
a real one from a local dev-issuer.

## Local development: a real SIT from `cmd/dev-issuer`

```python
from helmdeep.devissuer import issue_sit
from helmdeep import Client

sit = issue_sit("http://localhost:8444", agent="agent:my-bot", scope=["kb.read"])
client = Client("http://localhost:8443/mcp", credential=sit)
```

Run a local stack this points at with `examples/dev-emulator/` — see that
directory's own README for the one-command version (`docker compose up`),
built on `cmd/dev-issuer` and `cmd/mock-upstream`.

## Test

```sh
pip install -e "sdk/python[test]"
pytest sdk/python/tests
```

`tests/test_client.py` is fast and needs nothing but Python: it drives the
client against a fake gateway (stdlib `http.server`) and checks the exact
request shape the real `pkg/mcp.Server` requires. `tests/test_integration.py`
builds and runs the real `cmd/helmdeep-gateway`, `cmd/dev-issuer`, and
`cmd/mock-upstream` binaries and drives this SDK against them end to
end — real JWT identity, real OPA policy, a real upstream. It's skipped
automatically if `go` isn't on `PATH`.

## What this is not

No retries, no streaming/SSE response handling (`pkg/mcp.HTTPUpstream`
doesn't support SSE upstreams either — see its own doc comment), no
higher-level "agent" abstraction (memory, model calls, orchestration —
none of that exists in this repo yet), no TypeScript SDK. This is a
correct wire-protocol client and nothing more; see
`docs/adr/0010-python-sdk-and-dev-emulator.md`.
