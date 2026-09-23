// Copyright (c) 2026 Overarching AI LLC
// SPDX-License-Identifier: Apache-2.0

// STATUS: interface only. Not implemented. See docs/ROADMAP.md.
//
// Package controlplane will hold the remaining control-plane services doc
// 02-architecture.md §2 lists that have no real implementation anywhere
// yet: Trust Engine, Approval Service, Eval Service, and Tenant/Org
// Service. This package was not in the originally proposed layout; it's
// added because the component status table lists a Control plane stub, and
// every other stub component got its own pkg/ directory. See
// ARCHITECTURE.md "Repo layout notes" for why.
//
// Four Control Plane components listed in doc 02 §2 are NOT here, because
// each got a real implementation in its own package before this one got
// any: Tool Registry (pkg/toolregistry, Milestone M2), Agent Registry
// (pkg/agentregistry), Model Catalog (pkg/modelcatalog), and Policy
// Service (pkg/policyservice, which implements only the signed-bundle
// half of its responsibility) — see each package's own doc comment.
package controlplane

// TODO: not implemented.
