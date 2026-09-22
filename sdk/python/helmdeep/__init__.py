# Copyright (c) 2026 Overarching AI LLC
# SPDX-License-Identifier: Apache-2.0

"""helmdeep: a minimal, framework-agnostic Python client for the HelmDeep
Tool Gateway (docs/05-tool-gateway.md), speaking the same 2026-07-28
Streamable HTTP MCP wire protocol as ``pkg/mcp.Server``.

This is Milestone M4's Python SDK slice of docs/01-requirements.md FR-B2
("SDK in Python + TypeScript to implement agent logic; framework-agnostic
host interface") — Python only; no TypeScript SDK exists yet. See
docs/adr/0010-python-sdk-and-dev-emulator.md for what is deliberately not
here (retries, streaming/SSE responses, a higher-level "agent" abstraction)
and why: nothing in this milestone needs them yet.

Typical use::

    from helmdeep import Client

    client = Client("https://gateway.internal/mcp", credential=sit)
    tools = client.list_tools()
    result = client.call_tool("kb.search", {"query": "reset my password"})
    if result.is_error:
        ...  # denied, or the upstream failed — see result.content

``credential`` is whatever the gateway's configured identity.Resolver
expects: a real Session Identity Token from a real issuer (see
``helmdeep.devissuer`` for getting one from a local dev-issuer), or a
static dev token if the gateway is (insecurely) configured that way.
This SDK never mints or verifies credentials itself — that is the
gateway's and the issuer's job, not the agent's.
"""

from .client import CallToolError, Client, ProtocolError, ToolResult

__all__ = ["Client", "ToolResult", "ProtocolError", "CallToolError"]
