// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package client

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/itsnotyoutoday/gtf/internal/layout"
	"github.com/itsnotyoutoday/gtfparser"
)

// ---- /panel ----

var panelValueOpts = map[string]string{
	"dock": "dock", "d": "dock", "w": "w", "width": "w", "h": "h", "height": "h",
	"title": "title", "t": "title", "border": "border", "b": "border",
	"minw": "minw", "m": "minw", "fg": "fg", "bg": "bg",
}

// panelTarget returns the layout /panel edits (creating "default").
func (c *Client) panelTarget() *layout.Layout {
	name := c.panels.activeName(c)
	if name == "" {
		name = "default"
		c.panels.cur = name
	}
	return c.panels.layoutNamed(name, true)
}

// /panel [options] name [= body]
//
//	-dock left|right|top|bottom  -w W  -h H  -title T  -border single|rounded|double|ascii
//	-minw N  -fg COLOR  -bg COLOR  -wrap  -nowrap  -hidden  -shown
//	-del NAME   -show NAME   -hide NAME   -toggle NAME   -list   -clear
func (c *Client) cmdPanel(in *gtfparser.Interp, args string) gtfparser.Value {
	toks := splitPanelArgs(args)
	vals := map[string]string{}
	flags := map[string]bool{}
	var action, target string
	i := 0
	for ; i < len(toks); i++ {
		t := toks[i]
		if !strings.HasPrefix(t, "-") || len(t) < 2 {
			break
		}
		name := strings.TrimPrefix(t, "-")
		val := ""
		hasVal := false
		if k := strings.IndexByte(name, '='); k > 0 {
			name, val, hasVal = name[:k], name[k+1:], true
		}
		switch name {
		case "del", "delete", "show", "hide", "toggle":
			action = name
			if i+1 < len(toks) {
				target = toks[i+1]
				i++
			}
			continue
		case "list", "clear":
			action = name
			continue
		case "wrap", "nowrap", "hidden", "shown":
			flags[name] = true
			continue
		}
		key, ok := panelValueOpts[name]
		if !ok {
			in.Warn("/panel: invalid option -" + name)
			return gtfparser.Int(0)
		}
		if !hasVal {
			if i+1 >= len(toks) {
				in.Warn("/panel: -" + name + " needs a value")
				return gtfparser.Int(0)
			}
			i++
			val = toks[i]
		}
		vals[key] = val
	}
	l := c.panelTarget()
	switch action {
	case "list":
		return c.panelList(in, l)
	case "clear":
		for _, p := range l.Panels() {
			l.Remove(p.Name)
		}
		return gtfparser.Int(1)
	case "del", "delete":
		if !l.Remove(target) {
			in.Warn("/panel: no such panel " + target)
			return gtfparser.Int(0)
		}
		return gtfparser.Int(1)
	case "show", "hide", "toggle":
		p := l.Get(target)
		if p == nil {
			in.Warn("/panel: no such panel " + target)
			return gtfparser.Int(0)
		}
		switch action {
		case "show":
			p.Hidden = false
		case "hide":
			p.Hidden = true
		default:
			p.Hidden = !p.Hidden
		}
		l.Touch(target)
		return gtfparser.Int(1)
	}
	// define or modify: name [= body]
	rest := toks[i:]
	if len(rest) == 0 {
		return c.panelList(in, l)
	}
	name := rest[0]
	body, hasBody := "", false
	if k := strings.IndexByte(name, '='); k >= 0 {
		name, body, hasBody = name[:k], strings.Join(append([]string{name[k+1:]}, rest[1:]...), " "), true
		body = strings.TrimLeft(body, " ")
	} else if len(rest) > 1 && rest[1] == "=" {
		hasBody = true
		body = strings.Join(rest[2:], " ")
	} else if len(rest) > 1 && strings.HasPrefix(rest[1], "=") {
		hasBody = true
		body = strings.TrimPrefix(strings.Join(rest[1:], " "), "=")
		body = strings.TrimLeft(body, " ")
	}
	// the body is everything after the first unquoted "=" in the raw text,
	// which keeps its spacing exactly
	if hasBody {
		if e := firstEq(args); e >= 0 {
			body = strings.TrimLeft(args[e+1:], " ")
		}
	}
	p := l.Get(name)
	isNew := p == nil
	if isNew {
		p = &layout.Panel{Name: name, Dock: layout.DockLeft, Body: ""}
	} else {
		cp := *p
		p = &cp
	}
	if hasBody {
		p.Body = body
	} else if isNew {
		in.Warn("/panel: new panel needs a body: /panel name = template")
		return gtfparser.Int(0)
	}
	var err error
	if v, ok := vals["dock"]; ok {
		if p.Dock, err = layout.ParseDock(v); err != nil {
			in.Warn("/panel: " + err.Error())
			return gtfparser.Int(0)
		}
	}
	if v, ok := vals["w"]; ok {
		if p.W, err = layout.ParseDim(v); err != nil {
			in.Warn("/panel: " + err.Error())
			return gtfparser.Int(0)
		}
	}
	if v, ok := vals["h"]; ok {
		if p.H, err = layout.ParseDim(v); err != nil {
			in.Warn("/panel: " + err.Error())
			return gtfparser.Int(0)
		}
	}
	if v, ok := vals["title"]; ok {
		p.Title = v
	}
	if v, ok := vals["border"]; ok {
		if v == "none" || v == "off" {
			v = ""
		} else if v == "on" || v == "yes" {
			v = "single"
		}
		p.Border = v
	}
	if v, ok := vals["minw"]; ok {
		p.MinWidth, _ = strconv.Atoi(v)
	}
	if v, ok := vals["fg"]; ok {
		if _, err := layout.ParseColor(v); err != nil {
			in.Warn("/panel: " + err.Error())
			return gtfparser.Int(0)
		}
		p.Fg = v
	}
	if v, ok := vals["bg"]; ok {
		if _, err := layout.ParseColor(v); err != nil {
			in.Warn("/panel: " + err.Error())
			return gtfparser.Int(0)
		}
		p.Bg = v
	}
	if flags["wrap"] {
		p.Wrap = true
	}
	if flags["nowrap"] {
		p.Wrap = false
	}
	if flags["hidden"] {
		p.Hidden = true
	}
	if flags["shown"] {
		p.Hidden = false
	}
	if err := l.Set(p); err != nil {
		in.Warn("/panel: " + err.Error())
		return gtfparser.Int(0)
	}
	return gtfparser.Int(1)
}

// splitPanelArgs splits on blanks, honouring quotes, but leaves everything
// from the first bare "=" onward as individual words (the body is re-read
// from the raw text).
func splitPanelArgs(s string) []string { return splitWords(s) }

func (c *Client) panelList(in *gtfparser.Interp, l *layout.Layout) gtfparser.Value {
	ps := l.Panels()
	if len(ps) == 0 {
		in.Host.Echo("% Layout " + l.Name + ": no panels.")
		return gtfparser.Int(0)
	}
	in.Host.Echo("% Layout " + l.Name + ":")
	f := c.ui.Frame()
	shown := map[string]bool{}
	for _, p := range f.Panels {
		shown[p.P.Name] = true
	}
	for _, p := range ps {
		state := "shown"
		switch {
		case p.Hidden:
			state = "hidden"
		case !shown[p.Name]:
			state = "no room"
		}
		in.Host.Echo(fmt.Sprintf("%%   %-12s dock=%-6s w=%-5s h=%-5s border=%-7s %s", p.Name, p.Dock, p.W, p.H, orDash(p.Border), state))
	}
	return gtfparser.Int(int64(len(ps)))
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// script renders a layout as the commands that recreate it.
func layoutScript(l *layout.Layout) string {
	var b strings.Builder
	fmt.Fprintf(&b, "/layout new %s\n", l.Name)
	for _, p := range l.Panels() {
		fmt.Fprintf(&b, "/panel -dock %s", p.Dock)
		if p.W.Kind != layout.Auto {
			fmt.Fprintf(&b, " -w %s", p.W)
		}
		if p.H.Kind != layout.Auto {
			fmt.Fprintf(&b, " -h %s", p.H)
		}
		if p.Title != "" {
			fmt.Fprintf(&b, " -title %s", qw(p.Title))
		}
		if p.Border != "" {
			fmt.Fprintf(&b, " -border %s", p.Border)
		}
		if p.MinWidth > 0 {
			fmt.Fprintf(&b, " -minw %d", p.MinWidth)
		}
		if p.Fg != "" {
			fmt.Fprintf(&b, " -fg %s", p.Fg)
		}
		if p.Bg != "" {
			fmt.Fprintf(&b, " -bg %s", p.Bg)
		}
		if p.Wrap {
			b.WriteString(" -wrap")
		}
		if p.Hidden {
			b.WriteString(" -hidden")
		}
		body := strings.ReplaceAll(strings.ReplaceAll(p.Body, "\\", "\\\\"), "\n", "\\n")
		fmt.Fprintf(&b, " %s = %s\n", p.Name, body)
	}
	return b.String()
}

// ---- /layout ----

// /layout [list] | new NAME | use [-w WORLD] NAME | load NAME|FILE | save [FILE]
// | show | delete NAME | clear | off | reload | dirs
func (c *Client) cmdLayout(in *gtfparser.Interp, args string) gtfparser.Value {
	f := splitWords(args)
	sub := "list"
	if len(f) > 0 {
		sub, f = f[0], f[1:]
	}
	ps := c.panels
	switch sub {
	case "list", "ls":
		names := make([]string, 0, len(ps.layouts))
		for n := range ps.layouts {
			names = append(names, n)
		}
		sort.Strings(names)
		act := ps.activeName(c)
		if len(names) == 0 {
			in.Host.Echo("% No layouts defined.")
		}
		for _, n := range names {
			mark := " "
			if n == act {
				mark = "*"
			}
			in.Host.Echo(fmt.Sprintf("%% %s %-16s %d panel(s)", mark, n, len(ps.layouts[n].Panels())))
		}
		for w, n := range ps.world {
			in.Host.Echo(fmt.Sprintf("%%   world %-12s uses %s", w, n))
		}
		return gtfparser.Int(int64(len(names)))
	case "new":
		if len(f) == 0 {
			in.Warn("/layout new: name required")
			return gtfparser.Int(0)
		}
		ps.layouts[f[0]] = layout.New(f[0])
		ps.cur = f[0]
		c.ui.Redraw()
		return gtfparser.Int(1)
	case "use", "switch":
		world := ""
		if len(f) >= 2 && f[0] == "-w" {
			world, f = f[1], f[2:]
		}
		if len(f) == 0 {
			in.Warn("/layout use: name required")
			return gtfparser.Int(0)
		}
		if ps.layouts[f[0]] == nil {
			if !c.loadLayoutFile(in, f[0]) {
				return gtfparser.Int(0)
			}
			if ps.layouts[f[0]] == nil {
				ps.layouts[f[0]] = layout.New(f[0])
			}
		}
		if world != "" {
			ps.world[world] = f[0]
		} else {
			ps.cur = f[0]
		}
		ps.seen = map[string]string{}
		c.ui.Redraw()
		return gtfparser.Int(1)
	case "load":
		if len(f) == 0 {
			in.Warn("/layout load: file or name required")
			return gtfparser.Int(0)
		}
		return b2i(c.loadLayoutFile(in, f[0]))
	case "reload":
		n := ps.activeName(c)
		path := ps.files[n]
		if path == "" {
			in.Warn("/layout reload: layout " + n + " was not loaded from a file")
			return gtfparser.Int(0)
		}
		delete(ps.layouts, n)
		ok := c.loadLayoutFile(in, path)
		ps.cur = n
		ps.seen = map[string]string{}
		c.ui.Redraw()
		return b2i(ok)
	case "save":
		l := ps.layouts[ps.activeName(c)]
		if l == nil {
			in.Warn("/layout save: no active layout")
			return gtfparser.Int(0)
		}
		path := ""
		if len(f) > 0 {
			path = f[0]
		} else {
			path = filepath.Join(c.userLayoutDir(), l.Name+".layout")
		}
		if in.Restriction() >= gtfparser.RestrictFile {
			in.Warn("/layout save: restricted")
			return gtfparser.Int(0)
		}
		os.MkdirAll(filepath.Dir(path), 0o755)
		if err := os.WriteFile(path, []byte(layoutScript(l)), 0o644); err != nil {
			in.Warn("/layout save: " + err.Error())
			return gtfparser.Int(0)
		}
		in.Host.Echo("% Layout " + l.Name + " saved to " + path)
		return gtfparser.Int(1)
	case "show":
		if l := ps.layouts[ps.activeName(c)]; l != nil {
			for _, ln := range strings.Split(strings.TrimRight(layoutScript(l), "\n"), "\n") {
				in.Host.Echo(ln)
			}
		}
		return gtfparser.Int(1)
	case "delete", "rm":
		if len(f) == 0 || ps.layouts[f[0]] == nil {
			in.Warn("/layout delete: no such layout")
			return gtfparser.Int(0)
		}
		delete(ps.layouts, f[0])
		if ps.cur == f[0] {
			ps.cur = ""
		}
		c.ui.Redraw()
		return gtfparser.Int(1)
	case "clear":
		if l := ps.layouts[ps.activeName(c)]; l != nil {
			for _, p := range l.Panels() {
				l.Remove(p.Name)
			}
		}
		c.ui.Redraw()
		return gtfparser.Int(1)
	case "off":
		ps.cur = ""
		ps.world = map[string]string{}
		c.ui.Redraw()
		return gtfparser.Int(1)
	case "dirs":
		for _, d := range c.layoutDirs() {
			in.Host.Echo("% " + d)
		}
		return gtfparser.Int(1)
	}
	in.Warn("/layout: unknown subcommand " + sub + " (list new use load reload save show delete clear off dirs)")
	return gtfparser.Int(0)
}

func (c *Client) userLayoutDir() string { return filepath.Join(c.cfg.HomeDir, ".gtf", "layouts") }

func (c *Client) layoutDirs() []string {
	dirs := []string{".", c.userLayoutDir()}
	if d := gtfLibDir(); d != "" {
		dirs = append(dirs, filepath.Join(d, "gtf", "layouts"))
	}
	return dirs
}

// loadLayoutFile loads name (a path, or a layout name searched as
// NAME.layout in the layout directories).
func (c *Client) loadLayoutFile(in *gtfparser.Interp, name string) bool {
	path := ""
	if strings.ContainsRune(name, '/') || strings.HasSuffix(name, ".layout") {
		if _, err := os.Stat(name); err == nil {
			path = name
		}
	}
	if path == "" {
		for _, d := range c.layoutDirs() {
			p := filepath.Join(d, strings.TrimSuffix(name, ".layout")+".layout")
			if _, err := os.Stat(p); err == nil {
				path = p
				break
			}
		}
	}
	if path == "" {
		in.Warn("/layout: no such layout file: " + name)
		return false
	}
	before := c.panels.cur
	c.panels.cur = ""
	err := in.LoadFile(path)
	loaded := c.panels.cur
	if loaded == "" {
		loaded = before
	}
	c.panels.cur = loaded
	if loaded != "" {
		if c.panels.files == nil {
			c.panels.files = map[string]string{}
		}
		c.panels.files[loaded] = path
	}
	if err != nil {
		in.Warn("/layout: " + err.Error())
		return false
	}
	c.panels.seen = map[string]string{}
	c.ui.Redraw()
	return true
}

// firstEq finds the first "=" outside quotes that is not part of a -opt=value.
func firstEq(s string) int {
	var q byte
	wordStart := true
	opt := false
	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch {
		case q != 0:
			if ch == q {
				q = 0
			}
		case ch == '"' || ch == '\'':
			q = ch
		case ch == ' ' || ch == '\t':
			wordStart, opt = true, false
		case ch == '=' && !opt:
			return i
		default:
			if wordStart && ch == '-' {
				opt = true
			}
			wordStart = false
		}
	}
	return -1
}
