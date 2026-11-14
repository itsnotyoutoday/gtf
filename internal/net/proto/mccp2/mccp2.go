// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

// Package mccp2 implements MUD Client Compression Protocol v2 (option 86):
// the server sends IAC SB MCCP2 IAC SE and everything after is a zlib
// stream. The decompression itself lives in the telnet machine; this handler
// agrees to the option and triggers the switch.
package mccp2

import "github.com/itsnotyoutoday/gtf/internal/net/telnet"

func init() { telnet.Register(func() telnet.Handler { return &H{} }) }

// H is the handler.
type H struct{}

func (*H) Option() byte { return telnet.OptMCCP2 }
func (*H) Name() string { return "mccp2" }
func (*H) Policy() telnet.Policy {
	return telnet.Policy{AcceptThem: true}
}
func (*H) OnNegotiate(s *telnet.Session, side telnet.Side, on bool) {}
func (*H) OnSubneg(s *telnet.Session, data []byte) bool {
	if s.Enabled(telnet.OptMCCP2, telnet.Them) {
		s.StartCompression()
	}
	return true
}
