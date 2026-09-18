// Copyright (c) 2026 Overarching AI LLC
// SPDX-License-Identifier: Apache-2.0

// Package gateway implements the Tool Gateway's core request handling:
// resolving caller identity, building a decision request, consulting the
// PDP, enforcing the verdict (allow / deny / allow-with-obligations),
// writing an action record, and forwarding allowed calls upstream with the
// gateway's own credentials.
//
// STATUS: not implemented yet. This package holds only this comment through
// Step 1. Implementation lands in Step 2, once the architecture in
// ARCHITECTURE.md is approved. See ROADMAP.md.
//
// It is internal because nothing outside cmd/helmdeep-gateway should import
// gateway internals directly — the stable, importable contracts are the
// interfaces in pkg/.
package gateway
