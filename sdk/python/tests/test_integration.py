# Copyright (c) 2026 Overarching AI LLC
# SPDX-License-Identifier: Apache-2.0

"""End-to-end: builds and runs the real cmd/helmdeep-gateway, cmd/dev-issuer,
and cmd/mock-upstream binaries, and drives this SDK's Client against them
exactly as a real agent would — real JWT identity from a real dev-issuer,
real OPA policy evaluation, a real upstream MCP server. This is the
strongest verification this SDK has: test_client.py proves the wire
protocol is built correctly against a fake server; this proves it actually
works against the product.

Skipped if `go` is not on PATH (e.g. a Python-only environment) rather
than failing — this suite still runs `test_client.py`'s fast unit tests
without it.
"""

from __future__ import annotations

import http.client
import shutil
import socket
import subprocess
import sys
import time
from pathlib import Path

import pytest

from helmdeep import Client
from helmdeep.devissuer import issue_sit

REPO_ROOT = Path(__file__).resolve().parents[3]
AUDIENCE = "helmdeep-gateways-sdk-test"

pytestmark = pytest.mark.skipif(shutil.which("go") is None, reason="requires the Go toolchain to build the real binaries")


def _free_port() -> int:
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as s:
        s.bind(("127.0.0.1", 0))
        return s.getsockname()[1]


def _wait_until_up(host: str, port: int, timeout: float = 15.0) -> None:
    deadline = time.time() + timeout
    while time.time() < deadline:
        try:
            with socket.create_connection((host, port), timeout=0.5):
                return
        except OSError:
            time.sleep(0.1)
    raise TimeoutError(f"nothing is listening on {host}:{port} after {timeout}s")


@pytest.fixture(scope="module")
def binaries(tmp_path_factory) -> dict[str, Path]:
    bin_dir = tmp_path_factory.mktemp("bin")
    built = {}
    for name, pkg in (
        ("gateway", "./cmd/helmdeep-gateway"),
        ("dev-issuer", "./cmd/dev-issuer"),
        ("mock-upstream", "./cmd/mock-upstream"),
    ):
        out = bin_dir / name
        subprocess.run(["go", "build", "-o", str(out), pkg], cwd=REPO_ROOT, check=True, capture_output=True, text=True)
        built[name] = out
    return built


@pytest.fixture(scope="module")
def stack(binaries, tmp_path_factory):
    """Starts mock-upstream (knowledgebase profile), dev-issuer, and the
    gateway wired together with a real policy bundle and Tool Registry
    entry, and tears every process down afterward. Yields (gateway_url,
    issuer_url).
    """
    procs: list[subprocess.Popen] = []

    def start(cmd: list[str]) -> subprocess.Popen:
        p = subprocess.Popen(cmd, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)
        procs.append(p)
        return p

    upstream_port = _free_port()
    start([str(binaries["mock-upstream"]), "-profile=knowledgebase", f"-addr=127.0.0.1:{upstream_port}"])
    _wait_until_up("127.0.0.1", upstream_port)

    # A second, live upstream whose tool is never added to `tools:` below —
    # real at the MCP transport level, but undeclared in the Tool Registry.
    # Calling it must be a policy-shaped denial (Milestone M2), not the
    # protocol error an upstream mcp.Registry has never even heard of
    # would be.
    unregistered_port = _free_port()
    start([str(binaries["mock-upstream"]), "-profile=payments", f"-addr=127.0.0.1:{unregistered_port}"])
    _wait_until_up("127.0.0.1", unregistered_port)

    issuer_port = _free_port()
    start([str(binaries["dev-issuer"]), f"-addr=127.0.0.1:{issuer_port}", f"-audience={AUDIENCE}"])
    _wait_until_up("127.0.0.1", issuer_port)

    audit_path = tmp_path_factory.mktemp("audit") / "audit.log"
    config_path = tmp_path_factory.mktemp("config") / "config.yaml"
    gateway_port = _free_port()
    config_path.write_text(
        f"""
listen: "127.0.0.1:{gateway_port}"
path: "/mcp"
policy:
  path: {REPO_ROOT / "examples" / "policies"}
audit:
  path: {audit_path}
identity:
  jwt:
    audience: {AUDIENCE}
    jwks_url: http://127.0.0.1:{issuer_port}/.well-known/jwks.json
upstreams:
  - name: knowledgebase
    url: http://127.0.0.1:{upstream_port}/mcp
  - name: payments
    url: http://127.0.0.1:{unregistered_port}/mcp
tools:
  - id: kb.search
    upstream: knowledgebase
    risk: low
agents:
  - id: agent:support-bot
    owner: support-team
    risk: low
"""
    )
    start([str(binaries["gateway"]), "serve", f"-config={config_path}"])
    _wait_until_up("127.0.0.1", gateway_port)

    try:
        yield f"http://127.0.0.1:{gateway_port}/mcp", f"http://127.0.0.1:{issuer_port}"
    finally:
        for p in procs:
            p.terminate()
        for p in procs:
            try:
                p.wait(timeout=5)
            except subprocess.TimeoutExpired:
                p.kill()


def test_scoped_agent_lists_and_calls_its_allowed_tool(stack):
    gateway_url, issuer_url = stack
    # examples/policies/scope.rego scopes agent:support-bot to kb.search.
    sit = issue_sit(issuer_url, agent="agent:support-bot")
    client = Client(gateway_url, credential=sit)

    tools = client.list_tools()
    assert [t.name for t in tools] == ["kb.search"]

    result = client.call_tool("kb.search", {"query": "reset my password"})
    assert result.is_error is False, result.text()
    assert result.structured_content is not None


def test_agent_cannot_call_an_undeclared_tool(stack):
    gateway_url, issuer_url = stack
    sit = issue_sit(issuer_url, agent="agent:support-bot")
    client = Client(gateway_url, credential=sit)

    # payments.wire_transfer is real at the payments upstream but absent
    # from `tools:` in this test's config — the Tool Registry's own
    # refusal (Milestone M2), a policy-shaped denial, not the protocol
    # error a tool no upstream exposes at all would be.
    result = client.call_tool("payments.wire_transfer", {"account_number": "x", "amount_usd": 1})
    assert result.is_error is True
    assert "not registered" in (result.text() or "")


def test_forged_credential_is_rejected(stack):
    gateway_url, _ = stack
    client = Client(gateway_url, credential="not-a-real-jwt")
    result = client.call_tool("kb.search", {"query": "x"})
    assert result.is_error is True
    assert "denied" in (result.text() or "")
