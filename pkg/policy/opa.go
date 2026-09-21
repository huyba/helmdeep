// Copyright (c) 2026 Overarching AI LLC
// SPDX-License-Identifier: Apache-2.0

package policy

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/open-policy-agent/opa/v1/loader"
	"github.com/open-policy-agent/opa/v1/rego"

	"github.com/huyba/helmdeep/pkg/types"
)

// hiddenFileFilter excludes any file or directory below the bundle root
// whose name starts with "." — in particular Kubernetes ConfigMap volumes,
// which mount as a visible top-level symlink per key pointing through a
// hidden "..data" symlink to a hidden, timestamped "..<timestamp>"
// directory holding the real files (this is how kubelet swaps in updated
// ConfigMap content atomically). Without this filter, rego.Load's
// recursive walk finds the same *.rego file three times — once via each
// path — and OPA's compiler then reports "multiple default rules" for a
// bundle that, from a plain filesystem's point of view, has exactly one.
// minDepth 1 so this only excludes things *below* the bundle root, not the
// root path itself, which may legitimately start with "." (e.g. a
// dotfile-prefixed temp dir in a test).
var hiddenFileFilter = loader.GlobExcludeName(".*", 1)

// regoQuery is the rule every policy bundle must define. See
// docs/policy-guide.md for the input contract and docs/adr/0002 for why
// this is one rule rather than separate allow/deny/obligation rules: a
// single rule returning a complete decision object means there is exactly
// one thing to be undefined, and "undefined" has one meaning (fail closed),
// not several.
const regoQuery = "data.helmdeep.authz.decision"

// regoDecision mirrors the JSON object a policy's `decision` rule must
// produce. It intentionally does not reuse types.DecisionResponse directly:
// keeping the wire shape a policy author writes against separate from the
// Go type the rest of the gateway uses means a future change to
// DecisionResponse's Go representation doesn't silently change what field
// names policies must produce.
type regoDecision struct {
	Outcome     string `json:"outcome"`
	PolicyID    string `json:"policy_id"`
	Reason      string `json:"reason"`
	Obligations []struct {
		Type   string `json:"type"`
		Target string `json:"target"`
	} `json:"obligations"`
}

// OPADecider is a Decider backed by an embedded OPA/Rego evaluator. It
// compiles the policy bundle once (on Load and on every Reload) via
// rego.PrepareForEval and reuses the prepared query for every Decide call —
// see docs/adr/0002-policy-engine-choice.md. Parsing or compiling Rego on
// the request path is exactly what would blow the sub-5ms latency target,
// so Decide never does it.
//
// The zero value is not usable; construct with NewOPADecider and call Load
// before the first Decide.
type OPADecider struct {
	mu       sync.RWMutex
	prepared *rego.PreparedEvalQuery
	path     string
}

// NewOPADecider returns an OPADecider with no policy loaded yet. Call Load
// before using it as a Decider.
func NewOPADecider() *OPADecider {
	return &OPADecider{}
}

// Load compiles the Rego policy bundle at path (a directory of .rego files)
// and makes it the active policy. path is remembered so Reload can be
// called with no arguments.
func (d *OPADecider) Load(path string) error {
	pq, err := rego.New(
		rego.Query(regoQuery),
		rego.Load([]string{path}, hiddenFileFilter),
	).PrepareForEval(context.Background())
	if err != nil {
		return fmt.Errorf("compile policy bundle at %q: %w", path, err)
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	d.path = path
	d.prepared = &pq
	return nil
}

// Ready reports whether a policy bundle is loaded and evaluable. Before the
// first successful Load every Decide is a deny (fail closed), so an
// unloaded decider is running but useless — exactly what a readiness probe
// should refuse to route traffic to. A failed Reload keeps the previous
// policy active, so it does not make Ready fail.
func (d *OPADecider) Ready() error {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if d.prepared == nil {
		return fmt.Errorf("no policy bundle loaded")
	}
	return nil
}

// Reload recompiles the policy bundle from the path passed to the last
// successful Load, and swaps it in atomically. A compile error leaves the
// previously active policy in place and is returned to the caller — a
// broken edit to a policy file must never leave the gateway without any
// policy loaded, which combined with fail-closed evaluation would make
// every single request deny for an unrelated typo in someone's rego file.
func (d *OPADecider) Reload() error {
	d.mu.RLock()
	path := d.path
	d.mu.RUnlock()
	if path == "" {
		return fmt.Errorf("reload called before an initial Load")
	}
	return d.Load(path)
}

// Decide evaluates req against the currently loaded policy. Any condition
// that isn't an explicit, well-formed "allow" from the policy — no policy
// loaded, a compile/eval error, an undefined result, a result that fails to
// parse as regoDecision, an outcome value we don't recognize — returns an
// explicit deny. See docs/adr/0003-fail-closed-behavior.md: this method has
// exactly one success path and every other path is deny, on purpose.
func (d *OPADecider) Decide(ctx context.Context, req types.DecisionRequest) (types.DecisionResponse, error) {
	d.mu.RLock()
	pq := d.prepared
	d.mu.RUnlock()

	if pq == nil {
		return denyf("no_policy_loaded", "no policy bundle has been loaded"), nil
	}

	inputJSON, err := json.Marshal(req)
	if err != nil {
		return denyf("input_marshal_error", "could not build policy input: %v", err), nil
	}
	var input map[string]any
	if err := json.Unmarshal(inputJSON, &input); err != nil {
		return denyf("input_marshal_error", "could not build policy input: %v", err), nil
	}

	results, err := pq.Eval(ctx, rego.EvalInput(input))
	if err != nil {
		return denyf("policy_eval_error", "policy evaluation failed: %v", err), nil
	}
	if len(results) == 0 || len(results[0].Expressions) == 0 {
		// The decision rule was undefined for this input — no rule body
		// matched, including the bundle's own default. Treat exactly like
		// any other failure to get a real answer.
		return denyf("policy_undefined", "policy did not produce a decision for this input"), nil
	}

	raw, err := json.Marshal(results[0].Expressions[0].Value)
	if err != nil {
		return denyf("result_marshal_error", "could not read policy result: %v", err), nil
	}
	var rd regoDecision
	if err := json.Unmarshal(raw, &rd); err != nil {
		return denyf("result_parse_error", "policy result did not match the expected shape: %v", err), nil
	}

	resp := types.DecisionResponse{
		PolicyID: rd.PolicyID,
		Reason:   rd.Reason,
	}
	switch types.Outcome(rd.Outcome) {
	case types.OutcomeAllow:
		resp.Outcome = types.OutcomeAllow
	case types.OutcomeAllowWithObligations:
		resp.Outcome = types.OutcomeAllowWithObligations
		for _, o := range rd.Obligations {
			resp.Obligations = append(resp.Obligations, types.Obligation{
				Type:   types.ObligationType(o.Type),
				Target: o.Target,
			})
		}
	case types.OutcomeDeny:
		resp.Outcome = types.OutcomeDeny
	default:
		// Includes the empty string (field absent) and any value that
		// isn't one of the three the gateway understands.
		return denyf("unrecognized_outcome", "policy returned unrecognized outcome %q", rd.Outcome), nil
	}
	return resp, nil
}

func denyf(policyID, format string, args ...any) types.DecisionResponse {
	return types.DecisionResponse{
		Outcome:  types.OutcomeDeny,
		PolicyID: policyID,
		Reason:   fmt.Sprintf(format, args...),
	}
}

var _ Decider = (*OPADecider)(nil)
var _ Loader = (*OPADecider)(nil)
