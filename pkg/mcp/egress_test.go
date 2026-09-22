// Copyright (c) 2026 Overarching AI LLC
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"errors"
	"net"
	"testing"
)

// fakeConn is the minimum net.Conn needed to exercise blockedDial's
// RemoteAddr check without opening a real socket.
type fakeConn struct {
	net.Conn
	remote  net.Addr
	closed  bool
	closeCh chan struct{}
}

func (c *fakeConn) RemoteAddr() net.Addr { return c.remote }
func (c *fakeConn) Close() error {
	c.closed = true
	if c.closeCh != nil {
		close(c.closeCh)
	}
	return nil
}

func dialReturning(addr net.Addr, conn *fakeConn) func(context.Context, string, string) (net.Conn, error) {
	return func(context.Context, string, string) (net.Conn, error) {
		conn.remote = addr
		return conn, nil
	}
}

func TestBlockedDial_RefusesLinkLocalIPv4(t *testing.T) {
	conn := &fakeConn{}
	dial := blockedDial(dialReturning(&net.TCPAddr{IP: net.ParseIP("169.254.169.254"), Port: 80}, conn))

	_, err := dial(context.Background(), "tcp", "169.254.169.254:80")
	if err == nil {
		t.Fatal("expected the cloud metadata address to be refused, got nil error")
	}
	if !conn.closed {
		t.Fatal("blockedDial returned an error but left the underlying connection open")
	}
}

func TestBlockedDial_RefusesLinkLocalIPv6(t *testing.T) {
	conn := &fakeConn{}
	dial := blockedDial(dialReturning(&net.TCPAddr{IP: net.ParseIP("fe80::1"), Port: 80}, conn))

	if _, err := dial(context.Background(), "tcp", "[fe80::1]:80"); err == nil {
		t.Fatal("expected a link-local IPv6 address to be refused, got nil error")
	}
}

func TestBlockedDial_AllowsOrdinaryAddresses(t *testing.T) {
	for _, ip := range []string{"127.0.0.1", "10.0.0.5", "172.17.0.2", "192.168.1.1", "8.8.8.8"} {
		conn := &fakeConn{}
		dial := blockedDial(dialReturning(&net.TCPAddr{IP: net.ParseIP(ip), Port: 80}, conn))
		if _, err := dial(context.Background(), "tcp", ip+":80"); err != nil {
			t.Fatalf("blockedDial refused ordinary address %s: %v", ip, err)
		}
		if conn.closed {
			t.Fatalf("blockedDial closed the connection to ordinary address %s", ip)
		}
	}
}

func TestBlockedDial_PropagatesDialError(t *testing.T) {
	wantErr := errors.New("connection refused")
	dial := blockedDial(func(context.Context, string, string) (net.Conn, error) {
		return nil, wantErr
	})
	if _, err := dial(context.Background(), "tcp", "example.invalid:80"); !errors.Is(err, wantErr) {
		t.Fatalf("got error %v, want %v", err, wantErr)
	}
}
