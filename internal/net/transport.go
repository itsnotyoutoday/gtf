// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

// Package net is the transport and protocol stack: byte transports (tcp, tls)
// and Conn, which runs the telnet machine over one and yields typed events.
// It has no knowledge of the interpreter or the UI.
package net

import (
	"context"
	"crypto/tls"
	"net"
	"time"
)

// Transport opens a byte stream to host:port.
type Transport interface {
	Dial(ctx context.Context, host, port string) (net.Conn, error)
	Name() string
}

// TCP is a plain TCP transport.
type TCP struct{ Timeout time.Duration }

func (TCP) Name() string { return "tcp" }

// Dial connects.
func (t TCP) Dial(ctx context.Context, host, port string) (net.Conn, error) {
	d := net.Dialer{Timeout: t.Timeout, KeepAlive: 30 * time.Second}
	return d.DialContext(ctx, "tcp", net.JoinHostPort(host, port))
}

// TLS is TCP wrapped in TLS. Verify=false skips certificate verification
// (MUD servers often use self-signed certificates).
type TLS struct {
	Timeout    time.Duration
	Verify     bool
	ServerName string
	Config     *tls.Config // optional base config
}

func (TLS) Name() string { return "tls" }

// Dial connects and handshakes.
func (t TLS) Dial(ctx context.Context, host, port string) (net.Conn, error) {
	cfg := &tls.Config{}
	if t.Config != nil {
		cfg = t.Config.Clone()
	}
	cfg.ServerName = host
	if t.ServerName != "" {
		cfg.ServerName = t.ServerName
	}
	cfg.InsecureSkipVerify = !t.Verify //nolint:gosec // user-selected
	d := &tls.Dialer{NetDialer: &net.Dialer{Timeout: t.Timeout, KeepAlive: 30 * time.Second}, Config: cfg}
	return d.DialContext(ctx, "tcp", net.JoinHostPort(host, port))
}
