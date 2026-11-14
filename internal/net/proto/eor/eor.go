// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

// Package eor implements the TELNET END-OF-RECORD option (RFC 885): once
// negotiated, IAC EOR (and IAC GA) mark prompts. The telnet machine itself
// raises Prompt events for those commands; this handler only negotiates.
package eor

import "github.com/itsnotyoutoday/gtf/internal/net/telnet"

func init() { telnet.Register(func() telnet.Handler { return &H{} }) }

// H is the handler.
type H struct{}

func (*H) Option() byte { return telnet.OptEOR }
func (*H) Name() string { return "eor" }
func (*H) Policy() telnet.Policy {
	return telnet.Policy{AcceptUs: true, AcceptThem: true}
}
func (*H) OnNegotiate(s *telnet.Session, side telnet.Side, on bool) {}
func (*H) OnSubneg(s *telnet.Session, data []byte) bool             { return true }
