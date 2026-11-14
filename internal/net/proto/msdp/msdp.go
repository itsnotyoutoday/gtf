// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

// Package msdp implements the MUD Server Data Protocol (telnet option 69):
// tables, arrays and variables encoded with VAR/VAL/OPEN/CLOSE bytes.
// Incoming variables become telnet.Message{Proto:"msdp"} events.
package msdp

import (
	"fmt"
	"sort"

	"github.com/itsnotyoutoday/gtf/internal/net/telnet"
)

func init() { telnet.Register(func() telnet.Handler { return &H{} }) }

// Protocol bytes.
const (
	VAR        = 1
	VAL        = 2
	TableOpen  = 3
	TableClose = 4
	ArrayOpen  = 5
	ArrayClose = 6
)

// H is the handler.
type H struct{}

func (*H) Option() byte { return telnet.OptMSDP }
func (*H) Name() string { return "msdp" }
func (*H) Policy() telnet.Policy {
	return telnet.Policy{AcceptThem: true}
}
func (*H) OnNegotiate(s *telnet.Session, side telnet.Side, on bool) {}

func (*H) OnSubneg(s *telnet.Session, data []byte) bool {
	for _, kv := range Parse(data) {
		s.Emit(telnet.Message{Proto: "msdp", Name: kv.Name, Value: kv.Value})
	}
	return true
}

// KV is a decoded top-level variable.
type KV struct {
	Name  string
	Value any
}

// Parse decodes an MSDP payload into its top-level variables. Values are
// string, []any or map[string]any.
func Parse(b []byte) []KV {
	var out []KV
	i := 0
	for i < len(b) {
		if b[i] != VAR {
			i++
			continue
		}
		i++
		name, n := readStr(b[i:])
		i += n
		var val any = ""
		if i < len(b) && b[i] == VAL {
			i++
			val, n = readVal(b[i:])
			i += n
			// repeated VALs after one VAR form an array (REPORT a VAL b)
			var more []any
			for i < len(b) && b[i] == VAL {
				i++
				var v any
				v, n = readVal(b[i:])
				i += n
				more = append(more, v)
			}
			if more != nil {
				val = append([]any{val}, more...)
			}
		}
		out = append(out, KV{name, val})
	}
	return out
}

func isCtl(c byte) bool { return c >= VAR && c <= ArrayClose }

func readStr(b []byte) (string, int) {
	i := 0
	for i < len(b) && !isCtl(b[i]) {
		i++
	}
	return string(b[:i]), i
}

func readVal(b []byte) (any, int) {
	if len(b) == 0 {
		return "", 0
	}
	switch b[0] {
	case TableOpen:
		m := map[string]any{}
		i := 1
		for i < len(b) && b[i] != TableClose {
			if b[i] != VAR {
				i++
				continue
			}
			i++
			k, n := readStr(b[i:])
			i += n
			var v any = ""
			if i < len(b) && b[i] == VAL {
				i++
				v, n = readVal(b[i:])
				i += n
			}
			m[k] = v
		}
		if i < len(b) {
			i++
		}
		return m, i
	case ArrayOpen:
		arr := []any{}
		i := 1
		for i < len(b) && b[i] != ArrayClose {
			if b[i] != VAL {
				i++
				continue
			}
			i++
			v, n := readVal(b[i:])
			i += n
			arr = append(arr, v)
		}
		if i < len(b) {
			i++
		}
		return arr, i
	}
	s, n := readStr(b)
	return s, n
}

// Encode builds a payload: VAR name VAL value (arrays at top level become
// repeated VALs, as REPORT/SEND/LIST expect).
func Encode(name string, value any) []byte {
	b := append([]byte{VAR}, name...)
	switch v := value.(type) {
	case nil:
	case []any:
		for _, e := range v {
			b = append(b, VAL)
			b = appendVal(b, e)
		}
	case []string:
		for _, e := range v {
			b = append(b, VAL)
			b = appendVal(b, e)
		}
	default:
		b = append(b, VAL)
		b = appendVal(b, v)
	}
	return b
}

func appendVal(b []byte, v any) []byte {
	switch x := v.(type) {
	case map[string]any:
		b = append(b, TableOpen)
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			b = append(b, VAR)
			b = append(b, k...)
			b = append(b, VAL)
			b = appendVal(b, x[k])
		}
		return append(b, TableClose)
	case []any:
		b = append(b, ArrayOpen)
		for _, e := range x {
			b = append(b, VAL)
			b = appendVal(b, e)
		}
		return append(b, ArrayClose)
	case string:
		return append(b, x...)
	}
	return append(b, fmt.Sprint(v)...)
}

// Send transmits "VAR name VAL value".
func (*H) Send(s *telnet.Session, name string, value any) error {
	if !s.Enabled(telnet.OptMSDP, telnet.Them) {
		return fmt.Errorf("msdp: not negotiated")
	}
	s.Subneg(telnet.OptMSDP, Encode(name, value))
	return nil
}
