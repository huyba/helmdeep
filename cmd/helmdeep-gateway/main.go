// Copyright (c) 2026 Overarching AI LLC
// SPDX-License-Identifier: Apache-2.0

// Command helmdeep-gateway is the Tool Gateway: it wires together the
// interfaces in pkg/identity, pkg/policy, pkg/audit, and pkg/mcp into a
// running server. See ARCHITECTURE.md for the component decomposition and
// SECURITY.md for what this does and doesn't defend against.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/huyba/helmdeep/internal/gateway"
	"github.com/huyba/helmdeep/pkg/audit"
	"github.com/huyba/helmdeep/pkg/mcp"
	"github.com/huyba/helmdeep/pkg/policy"
)

// version is overridden at build time via -ldflags; see Makefile.
var version = "dev"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	var err error
	switch os.Args[1] {
	case "version":
		fmt.Println("helmdeep-gateway", version)
	case "serve":
		err = runServe(os.Args[2:])
	case "verify-chain":
		err = runVerifyChain(os.Args[2:])
	default:
		usage()
		os.Exit(1)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "helmdeep-gateway:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage: helmdeep-gateway <command> [flags]

commands:
  serve         run the gateway (flags: -config path/to/config.yaml)
  verify-chain  verify the action record hash chain (flags: -config path/to/config.yaml)
  version       print the build version`)
}

func runServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	configPath := fs.String("config", "config.yaml", "path to the gateway's YAML config file")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := loadConfig(*configPath)
	if err != nil {
		return err
	}

	identityResolver := gateway.NewStaticTokenResolver(cfg.staticIdentities())

	pdp := policy.NewOPADecider()
	if err := pdp.Load(cfg.Policy.Path); err != nil {
		return fmt.Errorf("load policy: %w", err)
	}

	auditStore, err := audit.NewFileStore(cfg.Audit.Path)
	if err != nil {
		return fmt.Errorf("open audit log: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	var upstreams []mcp.Upstream
	for _, u := range cfg.Upstreams {
		token := ""
		if u.BearerTokenEnv != "" {
			token = os.Getenv(u.BearerTokenEnv)
		}
		upstreams = append(upstreams, mcp.NewHTTPUpstream(u.Name, u.URL, token))
	}
	registry, err := mcp.NewStaticRegistry(ctx, upstreams)
	if err != nil {
		return fmt.Errorf("reach configured upstreams: %w", err)
	}

	gw := gateway.New(identityResolver, pdp, auditStore, registry, cfg.upstreamProvenance())
	server := mcp.NewServer(cfg.Listen, cfg.Path, gw, version)

	// SIGHUP reloads the policy bundle without restarting the gateway or
	// interrupting in-flight sessions — see pkg/policy.Loader and
	// docs/adr/0002-policy-engine-choice.md.
	reload := make(chan os.Signal, 1)
	signal.Notify(reload, syscall.SIGHUP)
	go func() {
		for range reload {
			if err := pdp.Reload(); err != nil {
				slog.Error("policy reload failed, previous policy remains active", "error", err)
				continue
			}
			slog.Info("policy reloaded", "path", cfg.Policy.Path)
		}
	}()

	slog.Info("helmdeep-gateway starting", "listen", cfg.Listen, "path", cfg.Path, "version", version)
	return server.Serve(ctx)
}

func runVerifyChain(args []string) error {
	fs := flag.NewFlagSet("verify-chain", flag.ExitOnError)
	configPath := fs.String("config", "config.yaml", "path to the gateway's YAML config file")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := loadConfig(*configPath)
	if err != nil {
		return err
	}

	store, err := audit.NewFileStore(cfg.Audit.Path)
	if err != nil {
		return fmt.Errorf("open audit log: %w", err)
	}
	if err := store.Verify(context.Background()); err != nil {
		return fmt.Errorf("chain is broken: %w", err)
	}
	fmt.Println("chain intact:", cfg.Audit.Path)
	return nil
}
