// Copyright (c) 2026 Overarching AI LLC
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"fmt"
	"net"
)

// blockedDial wraps dial to refuse any connection whose resolved remote
// address is link-local (169.254.0.0/16, fe80::/10) — the address space
// every major cloud's instance metadata service (IMDS) lives in, precisely
// because it is always reachable and never routed off the host. Doc
// 05-tool-gateway.md §3's "Blocked always: cloud metadata endpoints" is
// this check: nothing a legitimate upstream MCP server needs lives there,
// so this has no legitimate deployment to break.
//
// The check runs on the connection actually established (conn.RemoteAddr),
// not on a separate DNS lookup of the hostname first — resolving the name
// ourselves and dialing separately would leave a window for the two
// lookups to disagree (DNS rebinding: the name resolves somewhere safe for
// our check, then somewhere else for the real connection). Checking the
// live connection has nothing left to rebind.
func blockedDial(dial func(ctx context.Context, network, addr string) (net.Conn, error)) func(ctx context.Context, network, addr string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		conn, err := dial(ctx, network, addr)
		if err != nil {
			return nil, err
		}
		if ip := remoteIP(conn); ip != nil && ip.IsLinkLocalUnicast() {
			_ = conn.Close()
			return nil, fmt.Errorf("connection to %s refused: %s is a link-local address, reserved for cloud metadata services and always blocked (doc 05-tool-gateway.md §3)", addr, ip)
		}
		return conn, nil
	}
}

func remoteIP(conn net.Conn) net.IP {
	switch a := conn.RemoteAddr().(type) {
	case *net.TCPAddr:
		return a.IP
	case *net.UDPAddr:
		return a.IP
	default:
		return nil
	}
}
