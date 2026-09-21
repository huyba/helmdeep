// Copyright (c) 2026 Overarching AI LLC
// SPDX-License-Identifier: Apache-2.0

package health

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func do(t *testing.T, h http.Handler, method string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, "/", nil))
	return rec
}

func TestLivezIsAlwaysOK(t *testing.T) {
	rec := do(t, Livez(), http.MethodGet)
	if rec.Code != http.StatusOK {
		t.Fatalf("Livez status = %d, want 200", rec.Code)
	}
}

func TestHealthzAllChecksPass(t *testing.T) {
	h := Healthz(
		Check{Name: "policy", Fn: func(context.Context) error { return nil }},
		Check{Name: "audit", Fn: func(context.Context) error { return nil }},
	)
	rec := do(t, h, http.MethodGet)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", rec.Code, rec.Body)
	}
}

func TestHealthzNoChecksIsOK(t *testing.T) {
	if rec := do(t, Healthz(), http.MethodGet); rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

// A failed check must turn the probe red, name what failed, and never leak
// the underlying error text (this endpoint is unauthenticated).
func TestHealthzFailureIs503NamesCheckAndHidesError(t *testing.T) {
	const internalDetail = "open /data/audit.log: read-only file system"
	h := Healthz(
		Check{Name: "policy", Fn: func(context.Context) error { return nil }},
		Check{Name: "audit", Fn: func(context.Context) error { return errors.New(internalDetail) }},
	)
	rec := do(t, h, http.MethodGet)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"audit"`) {
		t.Fatalf("body %q does not name the failed check", body)
	}
	if strings.Contains(body, "policy") {
		t.Fatalf("body %q names a check that passed", body)
	}
	if strings.Contains(body, internalDetail) || strings.Contains(body, "/data") {
		t.Fatalf("body %q leaks the underlying error", body)
	}
}

// A check that ignores its context (a disk stuck in a syscall) must not be
// able to hang the probe past the deadline.
func TestHealthzHungCheckFailsPromptly(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	h := Healthz(Check{Name: "audit", Fn: func(context.Context) error {
		<-release // ignores ctx on purpose
		return nil
	}})

	start := time.Now()
	rec := do(t, h, http.MethodGet)
	elapsed := time.Since(start)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 for a hung check", rec.Code)
	}
	if elapsed > checkTimeout+time.Second {
		t.Fatalf("probe took %s, want about %s", elapsed, checkTimeout)
	}
}

func TestOnlyGetAndHeadAreAllowed(t *testing.T) {
	for _, h := range []http.Handler{Livez(), Healthz()} {
		if rec := do(t, h, http.MethodPost); rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("POST status = %d, want 405", rec.Code)
		}
		if rec := do(t, h, http.MethodHead); rec.Code != http.StatusOK {
			t.Fatalf("HEAD status = %d, want 200", rec.Code)
		}
	}
}
