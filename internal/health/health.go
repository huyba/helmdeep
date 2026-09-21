// Copyright (c) 2026 Overarching AI LLC
// SPDX-License-Identifier: Apache-2.0

// Package health serves the gateway's liveness and readiness endpoints for
// Kubernetes probes (or any load balancer): /livez says the process is up
// and serving HTTP, /healthz says its dependencies actually work.
//
// The split is deliberate. A failing liveness probe makes kubelet *restart*
// the container, which cannot fix a full or read-only disk and would turn a
// storage problem into a crash loop, so /livez checks nothing but the
// process itself. A failing readiness probe only takes the pod out of
// Service rotation, which is the right response to "policy not loaded" or
// "audit log unwritable" — see deploy/k8s/deployment.yaml.
package health

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"
)

// checkTimeout bounds how long one /healthz request may spend in checks, so
// a hung dependency (a stalled disk, say) makes the probe fail promptly
// instead of piling up blocked requests.
const checkTimeout = 2 * time.Second

// Check is one named dependency check. Name is what an unhealthy response
// reports; Fn's error is logged server-side and never sent to the caller —
// the endpoint is unauthenticated, and an error string can carry file
// paths or other internals.
type Check struct {
	Name string
	Fn   func(ctx context.Context) error
}

type response struct {
	Status string   `json:"status"`
	Failed []string `json:"failed,omitempty"`
}

// Livez reports 200 whenever the process can serve an HTTP request.
func Livez() http.Handler {
	return methodGuard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, response{Status: "ok"})
	}))
}

// Healthz runs every check and reports 200 only if all pass, 503 otherwise
// (naming the failed checks). With no checks it is equivalent to Livez.
func Healthz(checks ...Check) http.Handler {
	return methodGuard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), checkTimeout)
		defer cancel()

		var failed []string
		for _, c := range checks {
			if err := runCheck(ctx, c); err != nil {
				slog.Warn("health check failed", "check", c.Name, "error", err)
				failed = append(failed, c.Name)
			}
		}
		if len(failed) > 0 {
			writeJSON(w, http.StatusServiceUnavailable, response{Status: "unhealthy", Failed: failed})
			return
		}
		writeJSON(w, http.StatusOK, response{Status: "ok"})
	}))
}

// runCheck runs c in its own goroutine so a check stuck in a blocking
// syscall (a stalled disk ignores ctx) still lets the probe fail on time.
// The stuck goroutine outlives the request until the syscall returns; that
// is the cost of reporting a hung dependency promptly rather than hanging
// with it, and the channel is buffered so it can always exit afterward.
func runCheck(ctx context.Context, c Check) error {
	done := make(chan error, 1)
	go func() { done <- c.Fn(ctx) }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func methodGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, v response) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
