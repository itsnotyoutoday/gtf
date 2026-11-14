// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

// Package ttype implements TERMINAL-TYPE (RFC 1091) with MTTS (Mud Terminal
// Type Standard): the client answers successive SEND requests with its name
// (TINYFUGUE as real tf does, then GTF), the terminal type, then "MTTS <bits>".
package ttype

import (
	"strconv"
	"strings"

	"github.com/itsnotyoutoday/gtf/internal/net/telnet"
)

func init() { telnet.Register(func() telnet.Handler { return &H{} }) }

// MTTS capability bits.
const (
	ANSI      = 1
	VT100     = 2
	UTF8      = 4
	Color256  = 8
	Mouse     = 16
	OSCPalete = 32
	Screen    = 64
	Proxy     = 128
	TrueColor = 256
	MNES      = 512
	MSLP      = 1024
	SSL       = 2048
)

// H is the handler.
type H struct{ step int }

func (*H) Option() byte { return telnet.OptTType }
func (*H) Name() string { return "ttype" }
func (*H) Policy() telnet.Policy {
	return telnet.Policy{AcceptUs: true}
}
func (h *H) OnNegotiate(s *telnet.Session, side telnet.Side, on bool) {
	if side == telnet.Us && on {
		h.step = 0
	}
}

// Bits computes the MTTS bitvector for env.
func Bits(e telnet.EnvInfo) int {
	b := ANSI | VT100
	if e.UTF8 {
		b |= UTF8
	}
	if e.Color256 {
		b |= Color256
	}
	if e.TrueColor {
		b |= TrueColor
	}
	if e.TLS {
		b |= SSL
	}
	return b
}

// Replies returns the cycle of answers for env.
func Replies(e telnet.EnvInfo) []string {
	term := strings.ToUpper(e.TermName)
	if term == "" {
		term = "ANSI"
	}
	// TINYFUGUE first: servers key on it; then our own name.
	return []string{"TINYFUGUE", "GTF", term, "MTTS " + strconv.Itoa(Bits(e))}
}

func (h *H) OnSubneg(s *telnet.Session, data []byte) bool {
	if len(data) != 1 || data[0] != 1 { // SEND
		return true
	}
	if !s.Enabled(telnet.OptTType, telnet.Us) {
		return true
	}
	r := Replies(s.Env().Snapshot())
	i := h.step
	if i >= len(r) {
		i = len(r) - 1
	} else {
		h.step++
	}
	s.Subneg(telnet.OptTType, append([]byte{0}, r[i]...)) // IS
	return true
}
