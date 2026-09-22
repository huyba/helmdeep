# Copyright (c) 2026 Overarching AI LLC
# SPDX-License-Identifier: Apache-2.0

"""Optional helper for local development: fetch a real Session Identity
Token from a running cmd/dev-issuer, so a developer's agent code can
exercise the gateway with its own delegated identity (docs/01-requirements.md
FR-B3) instead of a shared static dev token.

This module talks to internal/devissuer's own HTTP surface (POST /issue) —
see that package's doc comment for exactly what "development and test
only" means here. It has no other relationship to the rest of this SDK;
``helmdeep.Client`` never imports it and works with any bearer credential
from anywhere.
"""

from __future__ import annotations

import json
import urllib.request


def issue_sit(
    issuer_url: str,
    agent: str,
    delegation_chain: list[str] | None = None,
    scope: list[str] | None = None,
    timeout: float = 10.0,
) -> str:
    """POSTs to a dev-issuer's /issue endpoint and returns the minted
    Session Identity Token. Raises urllib.error.HTTPError if the issuer
    refuses the request (e.g. an empty agent id).

    :param issuer_url: the dev-issuer's base URL, e.g. "http://localhost:8444".
    :param agent: the agent definition id to assert, e.g. "agent:my-bot".
    :param delegation_chain: principal identifiers, outermost first —
        see pkg/types.Subject.DelegationChain's doc comment. Asserted by
        this dev issuer, not independently verified against a real IdP.
    :param scope: scopes this token grants — compared against a tool's
        required scopes by policy (pkg/toolregistry.Entry.Scopes).
    """
    body = json.dumps(
        {
            "agent": agent,
            "delegation_chain": delegation_chain or [],
            "scope": scope or [],
        }
    ).encode("utf-8")
    req = urllib.request.Request(
        issuer_url.rstrip("/") + "/issue",
        data=body,
        headers={"Content-Type": "application/json"},
        method="POST",
    )
    with urllib.request.urlopen(req, timeout=timeout) as resp:  # noqa: S310 - issuer_url is caller-supplied, not attacker-controlled
        return json.loads(resp.read())["token"]
