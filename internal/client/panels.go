// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package client

import (
	"sort"
	"strconv"
	"strings"

	"github.com/itsnotyoutoday/gtf/internal/layout"
	gnet "github.com/itsnotyoutoday/gtf/internal/net"
)

// panelState holds the named layouts and resolves their data bindings:
//
//	gmcp:/msdp:/mssp:  the foreground session's protocol data
//	var:name           a TF global variable
//	expr:...           a TF expression evaluated by the interpreter
type panelState struct {
	layouts  map[string]*layout.Layout
	cur      string            // default layout name
	world    map[string]string // per-world layout assignment
	seen     map[string]string // last values of var:/expr: dependencies
	c        *Client
	files    map[string]string         // layout name -> file it was loaded from
	canvases map[string]*layout.Canvas // /canvas surfaces, shown by {canvas NAME}
}

func newPanelState() *panelState {
	return &panelState{layouts: map[string]*layout.Layout{}, world: map[string]string{}, seen: map[string]string{}, canvases: map[string]*layout.Canvas{}}
}

// layoutNamed returns (creating if asked) a layout.
func (p *panelState) layoutNamed(name string, create bool) *layout.Layout {
	l := p.layouts[name]
	if l == nil && create {
		l = layout.New(name)
		p.layouts[name] = l
	}
	return l
}

// activeName is the layout in force for the foreground world.
func (p *panelState) activeName(c *Client) string {
	if c.fg != nil {
		if n := p.world[c.fg.World.Name]; n != "" {
			return n
		}
	}
	return p.cur
}

// current returns the layout to draw (nil when none).
func (p *panelState) current(c *Client) *layout.Layout {
	p.c = c
	if l := p.layouts[p.activeName(c)]; l != nil {
		return l
	}
	return nil
}

func (p *panelState) worldChanged(c *Client) { p.seen = map[string]string{} }

// Resolve implements layout.Resolver.
func (p *panelState) Resolve(ref string) (any, bool) {
	c := p.c
	if c == nil {
		return nil, false
	}
	i := strings.IndexByte(ref, ':')
	if i < 0 {
		return nil, false
	}
	switch ref[:i] {
	case "gmcp", "msdp", "mssp":
		if c.fg == nil {
			return nil, false
		}
		return c.fg.store.Resolve(ref)
	case "canvas":
		if cv, ok := p.canvases[ref[i+1:]]; ok {
			return cv, true
		}
		return nil, false
	case "var":
		name := ref[i+1:]
		if v, ok := p.varValue(name); ok {
			return v, true
		}
		return nil, false
	case "expr":
		v, err := c.in.EvalExpr(ref[i+1:])
		if err != nil {
			return nil, false
		}
		return v.String(), true
	}
	return nil, false
}

func (p *panelState) varValue(name string) (string, bool) {
	// $name only reads globals; an unset variable shows as empty
	v := p.c.in.Var(name)
	return v, true
}

// checkLayoutDeps compares the var:/expr: bindings the layout depends on with
// their last values and marks the affected panels dirty.
func (c *Client) checkLayoutDeps() {
	l := c.panels.current(c)
	if l == nil {
		return
	}
	for _, d := range l.Deps() {
		if !(strings.HasPrefix(d, "var:") || strings.HasPrefix(d, "expr:")) {
			continue
		}
		v, _ := c.panels.Resolve(d)
		s, _ := v.(string)
		if old, ok := c.panels.seen[d]; !ok || old != s {
			c.panels.seen[d] = s
			if f, err := strconv.ParseFloat(strings.TrimSpace(s), 64); err == nil {
				l.Observe(d, f)
			}
			l.Changed(d)
		}
	}
}

// dataChanged is called when out-of-band data for the foreground session changes.
func (c *Client) dataChanged(s *Session, key string) {
	if s != c.fg {
		return
	}
	l := c.panels.current(c)
	if l == nil {
		return
	}
	for _, d := range l.Deps() {
		if !(strings.HasPrefix(d, "gmcp:") || strings.HasPrefix(d, "msdp:") || strings.HasPrefix(d, "mssp:")) {
			continue
		}
		kp := key
		if d == kp || strings.HasPrefix(d, kp+".") || strings.HasPrefix(kp, d+".") {
			if v, ok := s.store.Resolve(d); ok {
				if f, ok := layoutFloat(v); ok {
					l.Observe(d, f)
				}
			}
		}
	}
	l.Changed(key)
}

func layoutFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(x), 64)
		return f, err == nil
	}
	return 0, false
}

// onMessage stores GMCP/MSDP/MSSP data and raises the script hooks.
func (c *Client) onMessage(s *Session, m gnet.Message) {
	switch m.Proto {
	case "gmcp":
		s.store.SetGMCP(m.Name, m.Value, m.Raw)
		c.sq.gmcp(s, m.Name, m.Raw)
		c.dataChanged(s, "gmcp:"+m.Name)
		arg := m.Name
		if m.Raw != "" {
			arg += " " + m.Raw
		}
		c.withWorld(s, func() { c.in.FireHook("GMCP", arg) })
	case "msdp":
		s.store.SetMSDP(m.Name, m.Value)
		c.sq.msdp(s, m.Name, m.Value)
		c.dataChanged(s, "msdp:"+m.Name)
		c.withWorld(s, func() { c.in.FireHook("MSDP", m.Name+"="+flatten(m.Value)) })
	case "mssp":
		s.store.SetMSSP(m.Name, m.Value)
		c.dataChanged(s, "mssp:"+m.Name)
	}
}

// flatten renders an MSDP value the way tf's MSDP hook does: scalars as text,
// arrays as { a b }, tables as [ k=v ... ].
func flatten(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case []any:
		p := make([]string, len(x))
		for i, e := range x {
			p[i] = flatten(e)
		}
		return "{ " + strings.Join(p, " ") + " }"
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		p := make([]string, len(keys))
		for i, k := range keys {
			p[i] = k + "=" + flatten(x[k])
		}
		return "[ " + strings.Join(p, " ") + " ]"
	}
	return layout.ToString(v)
}
