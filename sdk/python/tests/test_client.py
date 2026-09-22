# Copyright (c) 2026 Overarching AI LLC
# SPDX-License-Identifier: Apache-2.0

"""Unit tests against a minimal fake gateway (stdlib http.server), verifying
the client sends exactly the headers/body shape pkg/mcp.Server requires and
correctly interprets every response shape it can send back. See
test_integration.py for the same client driven against the real Go
gateway binary end to end.
"""

from __future__ import annotations

import json
import threading
from http.server import BaseHTTPRequestHandler, HTTPServer

import pytest

from helmdeep import CallToolError, Client, ProtocolError


class _FakeGateway(BaseHTTPRequestHandler):
    # Set by each test via server.canned_response / server.received.
    def do_POST(self):  # noqa: N802 - BaseHTTPRequestHandler's naming convention
        length = int(self.headers.get("Content-Length", 0))
        body = json.loads(self.rfile.read(length)) if length else {}
        self.server.received.append({"headers": dict(self.headers), "body": body, "path": self.path})

        status, payload = self.server.canned_response
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(json.dumps(payload).encode("utf-8"))

    def log_message(self, *args):  # silence test output
        pass


@pytest.fixture
def fake_gateway():
    server = HTTPServer(("127.0.0.1", 0), _FakeGateway)
    server.received = []
    server.canned_response = (200, {"jsonrpc": "2.0", "id": "1", "result": {}})
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    try:
        yield server
    finally:
        server.shutdown()
        thread.join()


def _url(server) -> str:
    return f"http://127.0.0.1:{server.server_port}/mcp"


def test_call_tool_sends_required_headers_and_meta(fake_gateway):
    fake_gateway.canned_response = (
        200,
        {"jsonrpc": "2.0", "id": "1", "result": {"isError": False, "content": []}},
    )
    client = Client(_url(fake_gateway), credential="sit-123")
    client.call_tool("kb.search", {"query": "x"})

    assert len(fake_gateway.received) == 1
    req = fake_gateway.received[0]
    assert req["headers"]["Mcp-Method"] == "tools/call"
    assert req["headers"]["Mcp-Name"] == "kb.search"
    assert req["headers"]["Mcp-Protocol-Version"] == "2026-07-28"
    assert req["headers"]["Authorization"] == "Bearer sit-123"
    assert req["body"]["params"]["_meta"]["io.modelcontextprotocol/protocolVersion"] == "2026-07-28"
    assert req["body"]["params"]["name"] == "kb.search"
    assert req["body"]["params"]["arguments"] == {"query": "x"}


def test_list_tools_sends_no_mcp_name_header(fake_gateway):
    fake_gateway.canned_response = (
        200,
        {"jsonrpc": "2.0", "id": "1", "result": {"tools": [{"name": "kb.search", "description": "d"}]}},
    )
    client = Client(_url(fake_gateway))
    tools = client.list_tools()

    assert "Mcp-Name" not in fake_gateway.received[0]["headers"]
    assert fake_gateway.received[0]["headers"]["Mcp-Method"] == "tools/list"
    assert [t.name for t in tools] == ["kb.search"]


def test_no_credential_sends_no_authorization_header(fake_gateway):
    client = Client(_url(fake_gateway))
    client.list_tools()
    assert "Authorization" not in fake_gateway.received[0]["headers"]


def test_denied_call_returns_error_result_not_an_exception(fake_gateway):
    fake_gateway.canned_response = (
        200,
        {
            "jsonrpc": "2.0",
            "id": "1",
            "result": {"isError": True, "content": [{"type": "text", "text": "denied: out of scope"}]},
        },
    )
    result = Client(_url(fake_gateway)).call_tool("payments.wire_transfer", {})
    assert result.is_error is True
    assert result.text() == "denied: out of scope"


def test_call_raises_calltoolerror_on_denial(fake_gateway):
    fake_gateway.canned_response = (
        200,
        {"jsonrpc": "2.0", "id": "1", "result": {"isError": True, "content": [{"type": "text", "text": "denied: no"}]}},
    )
    with pytest.raises(CallToolError, match="denied: no"):
        Client(_url(fake_gateway)).call("payments.wire_transfer", {})


def test_structured_content_is_parsed(fake_gateway):
    fake_gateway.canned_response = (
        200,
        {
            "jsonrpc": "2.0",
            "id": "1",
            "result": {"isError": False, "content": [], "structuredContent": {"answer": 42}},
        },
    )
    result = Client(_url(fake_gateway)).call_tool("kb.search", {})
    assert result.structured_content == {"answer": 42}


def test_protocol_error_raises_protocolerror(fake_gateway):
    fake_gateway.canned_response = (
        400,
        {"jsonrpc": "2.0", "id": "1", "error": {"code": -32602, "message": "unknown tool: nope"}},
    )
    with pytest.raises(ProtocolError) as exc_info:
        Client(_url(fake_gateway)).call_tool("nope", {})
    assert exc_info.value.code == -32602
    assert "unknown tool" in exc_info.value.message
