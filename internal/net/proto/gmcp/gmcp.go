// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

// Package gmcp implements the Generic MUD Communication Protocol (telnet
// option 201): "Package.Name <json>" packets in IAC SB 201 ... IAC SE.
// Incoming packets become telnet.Message{Proto:"gmcp"} events; on enabling,
// the handler says Core.Hello and Core.Supports.Set.
package gmcp

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/itsnotyoutoday/gtf/internal/net/telnet"
)

func init() { telnet.Register(func() telnet.Handler { return New() }) }

// DefaultSupports are the packages announced in Core.Supports.Set.
var DefaultSupports = []string{"Char 1", "Room 1", "Comm 1", "Core 1", "Group 1", "Client.Media 1"}

// H is the handler.
type H struct {
	Supports []string
}

// New returns a handler with the default package list.
func New() *H { return &H{Supports: append([]string(nil), DefaultSupports...)} }

func (*H) Option() byte { return telnet.OptGMCP }
func (*H) Name() string { return "gmcp" }
func (*H) Policy() telnet.Policy {
	return telnet.Policy{AcceptThem: true}
}

func (h *H) OnNegotiate(s *telnet.Session, side telnet.Side, on bool) {
	if side != telnet.Them || !on {
		return
	}
	e := s.Env().Snapshot()
	h.Send(s, "Core.Hello", map[string]any{"client": e.ClientName, "version": e.ClientVer})
	h.Send(s, "Core.Supports.Set", h.Supports)
}

// Split separates "Package.Name json" into its parts.
func Split(p string) (name, js string) {
	p = strings.TrimSpace(p)
	if i := strings.IndexAny(p, " \t"); i >= 0 {
		return p[:i], strings.TrimSpace(p[i+1:])
	}
	return p, ""
}

func (h *H) OnSubneg(s *telnet.Session, data []byte) bool {
	name, js := Split(string(data))
	if name == "" {
		return true
	}
	var v any
	if js != "" {
		if err := json.Unmarshal([]byte(js), &v); err != nil {
			v = js // not JSON: pass the text through
		}
	}
	s.Emit(telnet.Message{Proto: "gmcp", Name: name, Value: v, Raw: js})
	return true
}

// Send transmits a GMCP packet. value may be a string holding JSON text
// (sent verbatim if valid JSON), nil (no payload) or any marshalable value.
func (h *H) Send(s *telnet.Session, name string, value any) error {
	if !s.Enabled(telnet.OptGMCP, telnet.Them) && name != "Core.Hello" && name != "Core.Supports.Set" {
		return fmt.Errorf("gmcp: not negotiated")
	}
	var js string
	switch v := value.(type) {
	case nil:
	case string:
		if json.Valid([]byte(v)) {
			js = v
		} else {
			b, _ := json.Marshal(v)
			js = string(b)
		}
	case []byte:
		js = string(v)
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return err
		}
		js = string(b)
	}
	p := name
	if js != "" {
		p += " " + js
	}
	s.Subneg(telnet.OptGMCP, []byte(p))
	return nil
}
