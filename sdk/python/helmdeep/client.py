# Copyright (c) 2026 Overarching AI LLC
# SPDX-License-Identifier: Apache-2.0

"""The wire-protocol client. See helmdeep/__init__.py for the package-level
usage example.

Implements exactly the request shape pkg/mcp.Server (the 2026-07-28
Streamable HTTP transport) requires: the MCP-Protocol-Version, Mcp-Method,
and (for tools/call) Mcp-Name headers, all matching the body's own
_meta["io.modelcontextprotocol/protocolVersion"] and method/tool name — see
pkg/mcp/server.go's validateStandardHeaders and docs/adr/0005. A client
that gets any of this wrong is exercising a real protocol error path
(HeaderMismatch, -32020), not something this SDK papers over: agent code
should see the same errors a hand-rolled HTTP client would.

Deliberately uses only the standard library (urllib), not requests — see
docs/adr/0010-python-sdk-and-dev-emulator.md: this SDK's only job today is
correct wire-protocol calls, which urllib does without a third-party
dependency an agent author would otherwise have to pin and audit.
"""

from __future__ import annotations

import json
import urllib.error
import urllib.request
import uuid
from dataclasses import dataclass, field
from typing import Any

PROTOCOL_VERSION = "2026-07-28"


class ProtocolError(Exception):
    """A JSON-RPC-level error the gateway returned (unknown method, unknown
    tool, malformed request, unsupported protocol version, ...) — the
    request never reached policy evaluation at all. Distinct from
    CallToolError: this is "the gateway rejected the request," not "the
    tool call was denied or the upstream failed."
    """

    def __init__(self, code: int, message: str, data: Any = None):
        super().__init__(f"{message} (code {code})")
        self.code = code
        self.message = message
        self.data = data


class CallToolError(Exception):
    """Raised by ``Client.call`` (not ``call_tool``) when the tool result
    itself is an error — i.e. the call reached the gateway and a Handler,
    but the outcome was a policy denial or an upstream failure
    (``result.isError``). ``call_tool`` never raises this; it returns the
    ToolResult so the caller can inspect ``is_error`` and react, matching
    how a real agent needs to see *why* a call was denied, not just that
    an exception happened. Use ``call`` for scripts that would rather
    raise than check a flag.
    """

    def __init__(self, result: "ToolResult"):
        text = result.text() or "(no text content)"
        super().__init__(text)
        self.result = result


@dataclass
class ToolResult:
    """A tools/call result exactly as the gateway returned it — including
    isError, which is how a policy denial or an upstream failure both come
    back (docs/adr/0006-operational-failure-classification.md): both are
    a normal MCP result the calling agent can react to, never an
    exception raised by the transport.
    """

    is_error: bool
    content: list[dict[str, Any]] = field(default_factory=list)
    structured_content: Any = None

    def text(self) -> str | None:
        """Concatenates every content item of type "text", the common case
        for a denial reason or a simple tool response. Returns None if
        there is no text content at all (e.g. a purely structured result).
        """
        parts = [c.get("text", "") for c in self.content if c.get("type") == "text"]
        return "".join(parts) if parts else None


@dataclass
class Tool:
    name: str
    description: str = ""
    input_schema: Any = None


class Client:
    """A connection to one HelmDeep Tool Gateway endpoint.

    :param url: the gateway's MCP endpoint, e.g. "https://gateway.internal/mcp".
    :param credential: bearer credential presented as ``Authorization:
        Bearer <credential>`` — a Session Identity Token from a real or
        dev issuer, or empty if the gateway needs none for this call.
    :param timeout: socket timeout in seconds for each HTTP request.
    """

    def __init__(self, url: str, credential: str = "", timeout: float = 30.0):
        self._url = url
        self._credential = credential
        self._timeout = timeout

    def list_tools(self, cursor: str = "") -> list[Tool]:
        """Calls tools/list and returns every tool this credential is
        permitted to call — matching the gateway's own contract, an
        out-of-scope tool is simply absent, not an error. v1 does not
        paginate (see internal/gateway.Gateway.ListTools's own doc
        comment); a nextCursor in the response, if ever present, is
        available via the raw result but not followed automatically.
        """
        params: dict[str, Any] = {}
        if cursor:
            params["cursor"] = cursor
        result = self._request("tools/list", method_name="", params=params)
        return [
            Tool(name=t["name"], description=t.get("description", ""), input_schema=t.get("inputSchema"))
            for t in result.get("tools") or []
        ]

    def call_tool(self, name: str, arguments: dict[str, Any] | None = None) -> ToolResult:
        """Calls tools/call and returns the ToolResult as-is — including a
        denied or failed call, which comes back with is_error=True rather
        than as an exception. See docs/adr/0006-operational-failure-classification.md
        for why a denial and a transport failure are deliberately the same
        shape from the caller's side.
        """
        params = {"name": name, "arguments": arguments or {}}
        result = self._request("tools/call", method_name=name, params=params)
        return ToolResult(
            is_error=bool(result.get("isError")),
            content=result.get("content") or [],
            structured_content=result.get("structuredContent"),
        )

    def call(self, name: str, arguments: dict[str, Any] | None = None) -> Any:
        """Convenience wrapper over call_tool for scripts that would rather
        raise than check ``is_error`` themselves: returns
        ``structured_content`` (or the text content if there is none) on
        success, and raises CallToolError on a denial or upstream failure.
        """
        result = self.call_tool(name, arguments)
        if result.is_error:
            raise CallToolError(result)
        return result.structured_content if result.structured_content is not None else result.text()

    def _request(self, method: str, method_name: str, params: dict[str, Any]) -> dict[str, Any]:
        params = dict(params)
        params["_meta"] = {
            "io.modelcontextprotocol/protocolVersion": PROTOCOL_VERSION,
            "io.modelcontextprotocol/clientCapabilities": {},
        }
        body = json.dumps(
            {
                "jsonrpc": "2.0",
                "id": str(uuid.uuid4()),
                "method": method,
                "params": params,
            }
        ).encode("utf-8")

        headers = {
            "Content-Type": "application/json",
            "Accept": "application/json, text/event-stream",
            "MCP-Protocol-Version": PROTOCOL_VERSION,
            "Mcp-Method": method,
        }
        if method_name:
            headers["Mcp-Name"] = method_name
        if self._credential:
            headers["Authorization"] = f"Bearer {self._credential}"

        req = urllib.request.Request(self._url, data=body, headers=headers, method="POST")
        try:
            with urllib.request.urlopen(req, timeout=self._timeout) as resp:  # noqa: S310 - url is caller-supplied gateway endpoint, not attacker-controlled
                envelope = json.loads(resp.read())
        except urllib.error.HTTPError as e:
            # The gateway returns JSON-RPC errors with a non-2xx HTTP status
            # (see pkg/mcp/server.go's writeError) — the error body is still
            # a normal JSON-RPC error envelope, not an HTML error page.
            envelope = json.loads(e.read())

        if "error" in envelope and envelope["error"] is not None:
            err = envelope["error"]
            raise ProtocolError(err.get("code", 0), err.get("message", "unknown error"), err.get("data"))
        return envelope.get("result") or {}
