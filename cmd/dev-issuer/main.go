// Copyright (c) 2026 Overarching AI LLC
// SPDX-License-Identifier: Apache-2.0

// Command dev-issuer runs internal/devissuer as a standalone HTTP server:
// the "thin cmd/ binary" ADR 0007 and ARCHITECTURE.md both named as
// Milestone M4's job when M1 built the library it wraps. It exists so a
// local dev/emulator stack (examples/quickstart) and a developer's own
// tooling (sdk/python) can exercise real, verified JWT identity — an
// actual Session Identity Token, actually signature-checked by
// pkg/identity.JWTResolver — instead of the pre-M1 static-token bootstrap
// (identity.static_tokens, gated behind -dev-insecure for exactly this
// reason).
//
// DEVELOPMENT AND TEST ONLY. See internal/devissuer's own doc comment:
// the signing key is generated fresh every process start, never
// persisted, and every human-hop claim is asserted, not independently
// verified against a real IdP. This must never be exposed as a real
// deployment's identity provider.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/huyba/helmdeep/internal/devissuer"
)

// version is overridden at build time via -ldflags; see Makefile.
var version = "dev"

func main() {
	addr := flag.String("addr", ":8444", "address to listen on")
	audience := flag.String("audience", "helmdeep-gateways", "audience SITs and exchanged tokens are restricted to — must match the gateway's identity.jwt.audience exactly")
	sitTTL := flag.Duration("sit-ttl", 15*time.Minute, "how long a Session Identity Token minted by /issue is valid")
	exchangeTTL := flag.Duration("exchange-ttl", time.Minute, "how long a token minted by /token-exchange is valid")
	showVersion := flag.Bool("version", false, "print the build version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println("dev-issuer", version)
		return
	}

	issuer, err := devissuer.New(*audience)
	if err != nil {
		fmt.Fprintln(os.Stderr, "dev-issuer:", err)
		os.Exit(1)
	}

	server := devissuer.NewServer(issuer, *sitTTL, *exchangeTTL)
	httpServer := &http.Server{
		Addr:              *addr,
		Handler:           server,
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() { errCh <- httpServer.ListenAndServe() }()

	slog.Warn("dev-issuer: DEVELOPMENT AND TEST ONLY — signing key is fresh and unpersisted every start, never run this as a real identity provider")
	slog.Info("dev-issuer listening", "addr", *addr, "audience", *audience, "jwks_url", "http://"+hostForLog(*addr)+"/.well-known/jwks.json")

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			fmt.Fprintln(os.Stderr, "dev-issuer: shutdown:", err)
			os.Exit(1)
		}
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			fmt.Fprintln(os.Stderr, "dev-issuer:", err)
			os.Exit(1)
		}
	}
}

// hostForLog turns a listen address like ":8444" into something a human
// can paste into a browser or curl command, e.g. "localhost:8444" — purely
// cosmetic, never used to actually bind or dial anything.
func hostForLog(addr string) string {
	if len(addr) > 0 && addr[0] == ':' {
		return "localhost" + addr
	}
	return addr
}
