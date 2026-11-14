// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

// Package sga implements SUPPRESS-GO-AHEAD (RFC 858) and ECHO (RFC 857).
// ECHO from the server (WILL ECHO) means the server will echo what we type,
// so the client hides input (password mode); the telnet layer reports it as an
// OptionChange{Opt: OptEcho, Side: Them}.
package sga

import "github.com/itsnotyoutoday/gtf/internal/net/telnet"

func init() {
	telnet.Register(func() telnet.Handler { return &sgaH{} })
	telnet.Register(func() telnet.Handler { return &echoH{} })
}

type sgaH struct{}

func (*sgaH) Option() byte { return telnet.OptSGA }
func (*sgaH) Name() string { return "sga" }
func (*sgaH) Policy() telnet.Policy {
	return telnet.Policy{AcceptUs: true, AcceptThem: true}
}
func (*sgaH) OnNegotiate(s *telnet.Session, side telnet.Side, on bool) {}
func (*sgaH) OnSubneg(s *telnet.Session, data []byte) bool             { return true }

type echoH struct{}

func (*echoH) Option() byte { return telnet.OptEcho }
func (*echoH) Name() string { return "echo" }

// We never echo for the server (DONT), but accept the server echoing.
func (*echoH) Policy() telnet.Policy                                    { return telnet.Policy{AcceptThem: true} }
func (*echoH) OnNegotiate(s *telnet.Session, side telnet.Side, on bool) {}
func (*echoH) OnSubneg(s *telnet.Session, data []byte) bool             { return true }
