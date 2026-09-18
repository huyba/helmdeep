// Copyright (c) 2026 Overarching AI LLC
// SPDX-License-Identifier: Apache-2.0

// Command helmdeep-gateway is the single binary this repo ships today: the
// Tool Gateway. As of Step 1 it is scaffold only — no subcommand does real
// work yet. See ROADMAP.md for what Step 2 adds.
package main

import (
	"fmt"
	"os"
)

// version is overridden at build time via -ldflags; see Makefile.
var version = "dev"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "version":
		fmt.Println("helmdeep-gateway", version)
	case "serve":
		notImplemented()
	case "verify-chain":
		notImplemented()
	default:
		usage()
		os.Exit(1)
	}
}

func notImplemented() {
	fmt.Fprintln(os.Stderr, "helmdeep-gateway: not implemented yet — scaffold only (Step 1). See ROADMAP.md.")
	os.Exit(1)
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage: helmdeep-gateway <command>

commands:
  serve         run the gateway (not implemented yet)
  verify-chain  verify the action record hash chain (not implemented yet)
  version       print the build version`)
}
