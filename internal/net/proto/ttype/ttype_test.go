// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package ttype

import (
	"strings"
	"testing"

	"github.com/itsnotyoutoday/gtf/internal/net/telnet"
)

// The TERMINAL-TYPE cycle: TinyFugue's name first (games key on it), then the terminal type, then the
// MTTS bits in the position strict readers such as Evennia expect, then our own name.
func TestRepliesOrder(t *testing.T) {
	r := Replies(telnet.EnvInfo{TermName: "xterm-256color", UTF8: true})
	if len(r) != 4 {
		t.Fatalf("want 4 answers, got %v", r)
	}
	if r[0] != "TINYFUGUE" {
		t.Errorf("first answer %q: games recognise TinyFugue by it", r[0])
	}
	if r[1] != "XTERM-256COLOR" {
		t.Errorf("second answer %q: MTTS puts the terminal type second", r[1])
	}
	if !strings.HasPrefix(r[2], "MTTS ") {
		t.Errorf("third answer %q: MTTS puts the capability bits third", r[2])
	}
	if r[3] != "GTF" {
		t.Errorf("fourth answer %q", r[3])
	}
	if r := Replies(telnet.EnvInfo{}); r[1] != "ANSI" {
		t.Errorf("no terminal name should fall back to ANSI, got %q", r[1])
	}
}
