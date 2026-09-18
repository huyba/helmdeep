// Copyright (c) 2026 Overarching AI LLC
// SPDX-License-Identifier: Apache-2.0

// Command mock-upstream runs one internal/mockupstream profile as a
// standalone HTTP server, for the docker-compose quickstart. It is a test
// fixture, not part of the gateway product — see internal/mockupstream's
// doc comment.
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/huyba/helmdeep/internal/mockupstream"
)

func main() {
	addr := flag.String("addr", ":9000", "address to listen on")
	profile := flag.String("profile", "", fmt.Sprintf("mock upstream profile to serve (one of: %s)", profileNames()))
	path := flag.String("path", "/mcp", "MCP endpoint path")
	flag.Parse()

	handler, err := mockupstream.NewHandler(*profile)
	if err != nil {
		log.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.Handle(*path, handler)
	srv := &http.Server{Addr: *addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}

	log.Printf("mock-upstream: serving profile %q at %s%s", *profile, *addr, *path)
	log.Fatal(srv.ListenAndServe())
}

func profileNames() string {
	names := make([]string, 0, len(mockupstream.Profiles))
	for name := range mockupstream.Profiles {
		names = append(names, name)
	}
	return fmt.Sprintf("%v", names)
}
