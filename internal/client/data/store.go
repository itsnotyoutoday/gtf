// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

// Package data is the per-session store of out-of-band data from a world:
// GMCP packages, MSDP variables and MSSP status, addressable as
// gmcp:Char.Vitals.hp, msdp:HEALTH, mssp:NAME.
package data

import (
	"encoding/json"
	"sort"
	"strings"
	"sync"
)

// Store holds decoded protocol data for one connection.
type Store struct {
	mu   sync.RWMutex
	gmcp map[string]any
	msdp map[string]any
	mssp map[string]any
	raw  map[string]string // last raw GMCP JSON per package
}

// New makes an empty store.
func New() *Store {
	return &Store{gmcp: map[string]any{}, msdp: map[string]any{}, mssp: map[string]any{}, raw: map[string]string{}}
}

// SetGMCP stores a GMCP package ("Char.Vitals") under its dotted path.
func (s *Store) SetGMCP(pkg string, v any, raw string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	setPath(s.gmcp, pkg, v)
	s.raw[pkg] = raw
}

// SetMSDP stores an MSDP variable.
func (s *Store) SetMSDP(name string, v any) {
	s.mu.Lock()
	s.msdp[name] = v
	s.mu.Unlock()
}

// SetMSSP stores an MSSP value (repeated values become a list).
func (s *Store) SetMSSP(name string, v any) {
	s.mu.Lock()
	if old, ok := s.mssp[name]; ok {
		switch o := old.(type) {
		case []any:
			s.mssp[name] = append(o, v)
		default:
			s.mssp[name] = []any{o, v}
		}
	} else {
		s.mssp[name] = v
	}
	s.mu.Unlock()
}

func setPath(root map[string]any, path string, v any) {
	segs := strings.Split(path, ".")
	m := root
	for _, seg := range segs[:len(segs)-1] {
		next, ok := m[seg].(map[string]any)
		if !ok {
			next = map[string]any{}
			m[seg] = next
		}
		m = next
	}
	m[segs[len(segs)-1]] = v
}

func walk(root map[string]any, path string) (any, bool) {
	var v any = root
	if path == "" {
		return v, true
	}
	for _, seg := range strings.Split(path, ".") {
		switch x := v.(type) {
		case map[string]any:
			nv, ok := x[seg]
			if !ok {
				return nil, false
			}
			v = nv
		case []any:
			i := 0
			for _, c := range seg {
				if c < '0' || c > '9' {
					return nil, false
				}
				i = i*10 + int(c-'0')
			}
			if i >= len(x) {
				return nil, false
			}
			v = x[i]
		default:
			return nil, false
		}
	}
	return v, true
}

// Resolve looks up "gmcp:Path", "msdp:Path" or "mssp:Path" (the scheme is
// required). Arrays are indexed from 0.
func (s *Store) Resolve(ref string) (any, bool) {
	i := strings.IndexByte(ref, ':')
	if i < 0 {
		return nil, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	switch ref[:i] {
	case "gmcp":
		return walk(s.gmcp, ref[i+1:])
	case "msdp":
		return walk(s.msdp, ref[i+1:])
	case "mssp":
		return walk(s.mssp, ref[i+1:])
	}
	return nil, false
}

// JSON renders the value at ref as JSON ("" if absent).
func (s *Store) JSON(ref string) string {
	v, ok := s.Resolve(ref)
	if !ok {
		return ""
	}
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

// Packages lists the GMCP packages received so far.
func (s *Store) Packages() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, 0, len(s.raw))
	for k := range s.raw {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// MSDPVars lists the MSDP variables received so far.
func (s *Store) MSDPVars() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, 0, len(s.msdp))
	for k := range s.msdp {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// State is the store's content as plain data (for /restart): the decoded
// values round-trip through JSON unchanged because they came from JSON or are
// strings and lists of strings.
type State struct {
	GMCP map[string]any    `json:"gmcp,omitempty"`
	MSDP map[string]any    `json:"msdp,omitempty"`
	MSSP map[string]any    `json:"mssp,omitempty"`
	Raw  map[string]string `json:"raw,omitempty"`
}

// State captures the store. The maps are shared with the store: encode the
// result before the store changes again.
func (s *Store) State() State {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return State{GMCP: s.gmcp, MSDP: s.msdp, MSSP: s.mssp, Raw: s.raw}
}

// FromState rebuilds a store.
func FromState(st State) *Store {
	s := New()
	if st.GMCP != nil {
		s.gmcp = st.GMCP
	}
	if st.MSDP != nil {
		s.msdp = st.MSDP
	}
	if st.MSSP != nil {
		s.mssp = st.MSSP
	}
	if st.Raw != nil {
		s.raw = st.Raw
	}
	return s
}
