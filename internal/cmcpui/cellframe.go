// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package cmcpui

import (
	"github.com/itsnotyoutoday/cmcp/cellglyph"
	"math"
	"strings"

	"github.com/itsnotyoutoday/cmcp/pack"
	"github.com/itsnotyoutoday/cmcp/theme"
	"github.com/itsnotyoutoday/gtf/internal/layout"
)

// This file interprets the `cell` rendition of a core theme pack (spec 19.8): margin, edge-anchored
// frame ops, the nav column, the log label, the textport border, the widget characters and the
// per-screen arrangement as fractions. The theme is data: the same code draws every reference theme.

func cellDoc(t Theme) map[string]any {
	if t.Pack == nil {
		return nil
	}
	m, _ := t.Pack.Doc["cell"].(map[string]any)
	return m
}

func jstr(m map[string]any, k string) string { s, _ := m[k].(string); return s }
func jnum(m map[string]any, k string, def float64) float64 {
	if f, ok := m[k].(float64); ok {
		return f
	}
	return def
}
func jmap(m map[string]any, k string) map[string]any { x, _ := m[k].(map[string]any); return x }
func jlist(m map[string]any, k string) []any         { x, _ := m[k].([]any); return x }

// isCellTheme says whether the interpreter (not the hand-built tng frame) draws this theme.
func (t Theme) isCellTheme() bool { return t.Pack != nil && t.Style != "tng" }

// gradientAt evaluates a [[0..1, role]...] ramp at t.
func (t Theme) gradientAt(stops []any, pos float64) layout.Color {
	type st struct {
		at  float64
		col layout.Color
	}
	var ss []st
	for _, s := range stops {
		a, _ := s.([]any)
		if len(a) == 2 {
			at, _ := a[0].(float64)
			role, _ := a[1].(string)
			ss = append(ss, st{at, t.role(role)})
		}
	}
	if len(ss) == 0 {
		return t.Token("color.accent")
	}
	if pos <= ss[0].at {
		return ss[0].col
	}
	for i := 1; i < len(ss); i++ {
		if pos <= ss[i].at {
			span := ss[i].at - ss[i-1].at
			if span <= 0 {
				return ss[i].col
			}
			return layout.Lerp(ss[i-1].col, ss[i].col, (pos-ss[i-1].at)/span)
		}
	}
	return ss[len(ss)-1].col
}

// cellFrame draws the theme's frame ops in the margins, the nav column, the log pill.
func (d *drawer) cellFrame() {
	th := d.env.Theme
	cell := cellDoc(th)
	g := d.f.Grid
	w, h := g.W, g.H
	top, bottom := th.margins(h)
	bg := th.blank().BG
	d.c.clip = Rect{W: w, H: h}
	put := func(x, y int, r rune, fg, b layout.Color) {
		g.Set(x, y, layout.Cell{R: r, A: layout.Attr{FG: fg, BG: b}})
	}
	used := map[string]int{} // rows already taken on each edge: ops stack inward from the edge
	for _, op := range jlist(cell, "frame") {
		o, _ := op.(map[string]any)
		switch jstr(o, "op") {
		case "strip":
			chars := []rune(jstr(o, "chars"))
			pat := jstr(o, "pattern")
			size := int(jnum(o, "size", 1))
			edge := jstr(o, "edge")
			lo, hi := int(jnum(o, "from", 0)*float64(w)), w
			if _, ok := o["to"]; ok {
				hi = int(jnum(o, "to", 1) * float64(w))
			}
			if len(chars) == 0 && pat == "" {
				continue
			}
			col := func(i, n int) layout.Color {
				if gr := jlist(o, "gradient"); len(gr) > 0 {
					return th.edgeTheme().gradientAt(gr, float64(i)/float64(max(n-1, 1)))
				}
				return th.edgeTheme().role(jstr(o, "role"))
			}
			switch edge {
			case "top", "bottom":
				for r := 0; r < size; r++ {
					y := r + used[edge]
					if edge == "bottom" {
						y = h - 1 - r - used[edge]
					}
					var row []rune
					if pat != "" {
						if s, err := cellglyph.Lib().EdgeRow(pat, edge, w); err == nil {
							row = []rune(s)
						}
					}
					for x := lo; x < hi && x < w; x++ {
						ch := ' '
						switch {
						case len(row) > x:
							ch = row[x]
						case len(chars) > 0:
							ch = chars[(x+r)%len(chars)]
						}
						put(x, y, ch, col(x, w), bg)
					}
				}
			default:
				for r := 0; r < size; r++ {
					x := r + used[edge]
					if edge == "right" {
						x = w - 1 - r
					}
					for y := 0; y < h && len(chars) > 0; y++ {
						put(x, y, chars[y%len(chars)], col(y, h), bg)
					}
				}
			}
			used[edge] += size
		case "corner":
			rows, err := cellglyph.Lib().CornerPattern(jstr(o, "style"), jstr(o, "corner"))
			if err != nil {
				continue
			}
			c := jstr(o, "corner")
			for j, row := range rows {
				rs := []rune(row)
				for i, r := range rs {
					if r == ' ' {
						continue
					}
					x, y := i, j
					if c == "tr" || c == "br" {
						x = w - len(rs) + i
					}
					if c == "bl" || c == "br" {
						y = h - len(rows) + j
					}
					if x >= 0 && x < w && y >= 0 && y < h {
						put(x, y, r, th.edgeTheme().role(jstr(o, "role")), bg)
					}
				}
			}
		case "band":
			d.cellBand(o, used[jstr(o, "edge")])
			used[jstr(o, "edge")] += int(jnum(o, "rows", 1))
		case "elbow":
			rows := cellglyph.Lib().ElbowGrid(jstr(o, "corner"), int(jnum(o, "w", 8)), int(jnum(o, "h", 3)), int(jnum(o, "bar", 1)), int(jnum(o, "side", 3)), int(jnum(o, "ro", 1)), int(jnum(o, "ri", 1)))
			x0, y0 := 0, 0
			if c := jstr(o, "corner"); c == "tr" || c == "br" {
				x0 = w - int(jnum(o, "w", 8))
			}
			if c := jstr(o, "corner"); c == "bl" || c == "br" {
				y0 = h - int(jnum(o, "h", 3))
			}
			for j, row := range rows {
				for i, r := range []rune(row) {
					if r != ' ' && x0+i >= 0 && x0+i < w && y0+j >= 0 && y0+j < h {
						put(x0+i, y0+j, r, th.edgeTheme().role(jstr(o, "role")), bg)
					}
				}
			}
		case "rule":
			chars := []rune(jstr(o, "chars"))
			if len(chars) == 0 {
				continue
			}
			y := top
			if jstr(o, "edge") == "bottom" {
				y = h - bottom - 1
			}
			if top == 0 && jstr(o, "edge") != "bottom" || bottom == 0 && jstr(o, "edge") == "bottom" {
				continue
			}
			for x := th.leftCols(w); x < w; x++ {
				put(x, y, chars[x%len(chars)], th.edgeTheme().role(jstr(o, "role")), bg)
			}
		case "rivets":
			chars := []rune(jstr(o, "chars"))
			every := max(int(jnum(o, "every", 8)), 2)
			y := 0
			if jstr(o, "edge") == "bottom" {
				y = h - 1
			}
			if len(chars) > 0 {
				for x := every / 2; x < w; x += every {
					put(x, y, chars[0], th.edgeTheme().role(jstr(o, "role")), bg)
				}
			}
		case "text":
			txt, err := theme.Render(o["text"], d.themeVars())
			if err != nil || txt == "" {
				continue
			}
			rs := []rune(clean(txt))
			y := 0
			if jstr(o, "edge") == "bottom" {
				y = h - 1
			}
			x := 2
			switch jstr(o, "align") {
			case "right":
				x = w - len(rs) - 2
			case "center":
				x = (w - len(rs)) / 2
			}
			role := jstr(o, "role")
			for i, r := range rs {
				xx := x + i
				if xx < 0 || xx >= w {
					continue
				}
				fg, b := th.edgeTheme().role(role), bg
				if role == "bg" { // dark text on a coloured strip: sit on the strip's colour
					b = g.At(xx, y).A.FG
				}
				put(xx, y, r, fg, b)
			}
		}
	}
	d.cellNav(top, bottom)
	d.cellLogPill(top, bottom)
	d.cellFocusLine(top, bottom)
}

// themeVars are the closed template names of 19.5.
func (d *drawer) themeVars() map[string]any {
	v := map[string]any{}
	for k, x := range d.env.Vars {
		v[k] = x
	}
	if _, ok := v["title"]; !ok {
		v["title"] = d.env.Title
	}
	if _, ok := v["ship"]; !ok {
		v["ship"] = d.env.Ship
	}
	if _, ok := v["alert"]; !ok {
		v["alert"] = d.env.Alert
	}
	if _, ok := v["mode"]; !ok {
		v["mode"] = strings.TrimPrefix(d.env.ScreenID, "std.")
	}
	return v
}

// cellNav draws the console-switch column (decoration, spec 19.7) in the left margin.
func (d *drawer) cellNav(top, bottom int) {
	th := d.env.Theme
	g := d.f.Grid
	nav := jmap(cellDoc(th), "nav")
	nw := th.leftCols(g.W)
	if nav == nil || nw == 0 {
		return
	}
	style := jstr(nav, "style")
	marker := []rune(jstr(nav, "marker"))
	cur := consoleTab(d.env.Title)
	bg := th.blank().BG
	y := top + 1
	items := jlist(nav, "items")
	type entry struct {
		label, role    string
		cur, ok        bool
		active, cursor bool
	}
	var ents []entry
	if len(d.env.Nav) > 0 {
		for i, e := range navWindow(d.navEntries(), g.H-top-bottom-2) {
			role := "p"
			if len(items) > 0 {
				m, _ := items[i%len(items)].(map[string]any)
				role = jstr(m, "role")
			}
			ents = append(ents, entry{strings.ToUpper(e.Title), role, e.Active || e.Cursor, e.Available, e.Active, e.Cursor})
		}
	} else {
		for i, it := range items {
			m, _ := it.(map[string]any)
			ents = append(ents, entry{strings.ToUpper(jstr(m, "label")), jstr(m, "role"), i == cur, true, i == cur, false})
		}
	}
	for _, e := range ents {
		label, isCur := e.label, e.cur
		col := th.role(e.role)
		if !e.ok {
			col = layout.Lerp(col, rgbC(0, 0, 0), 0.65)
		}
		if y+1 >= g.H-bottom {
			break
		}
		fg, back := col, bg
		txt := label
		if len(txt) > nw-2 {
			txt = txt[:nw-2]
		}
		switch style {
		case "box", "slant":
			fg, back = dark, col
			if isCur {
				back = layout.Lerp(col, rgbC(255, 255, 255), 0.35)
			}
			for x := 0; x < nw-1; x++ {
				g.Set(x, y, layout.Cell{R: ' ', A: layout.Attr{FG: fg, BG: back}})
			}
			for k, r := range []rune(txt) {
				g.Set(1+k, y, layout.Cell{R: r, A: layout.Attr{FG: fg, BG: back, Bold: true}})
			}
			if style == "slant" && len(marker) > 0 {
				g.Set(nw-1, y, layout.Cell{R: marker[0], A: layout.Attr{FG: col, BG: bg}})
			}
		case "bracket":
			t := "[ " + txt + " ]"
			for k, r := range []rune(t) {
				g.Set(k, y, layout.Cell{R: r, A: layout.Attr{FG: col, BG: bg, Bold: isCur}})
			}
		default: // plain
			pre := ' '
			if isCur && len(marker) > 0 {
				pre = marker[0]
			}
			g.Set(0, y, layout.Cell{R: pre, A: layout.Attr{FG: col, BG: bg}})
			for k, r := range []rune(txt) {
				g.Set(1+k, y, layout.Cell{R: r, A: layout.Attr{FG: col, BG: bg, Bold: isCur}})
			}
		}
		// the menu's markers, whatever the style: an arrow and reverse video on the keyboard
		// cursor, a dot on the screen being shown
		if e.cursor || e.active {
			mk := '●'
			if e.cursor {
				mk = '▶'
				for x := 0; x < nw-1; x++ {
					c := g.At(x, y)
					c.A.Reverse, c.A.Bold = true, true
					g.Set(x, y, c)
				}
			}
			c := g.At(0, y)
			c.R = mk
			g.Set(0, y, c)
		}
		y += 2
	}
}

// cellLogPill draws the LOG control, prominent, in the bottom margin (top when it has no rows):
// a filled pill with the theme's label for the next state and the key.
func (d *drawer) cellLogPill(top, bottom int) {
	th := d.env.Theme
	g := d.f.Grid
	lg := jmap(cellDoc(th), "log")
	label := "LOG"
	if lg != nil {
		label = jstr(lg, "expand")
		if d.env.Expanded {
			label = jstr(lg, "shrink")
		}
	}
	d.logPill(label, th.role(orRole(jstr(lg, "role"))), g.H-1, g.W)
}

func orRole(r string) string {
	if r == "" {
		return "p"
	}
	return r
}

// logPill draws "◖ LABEL  F3 ◗" right-aligned on row y.
func (d *drawer) logPill(label string, col layout.Color, y, w int) {
	g := d.f.Grid
	if y < 0 || y >= g.H {
		return
	}
	bg := d.env.Theme.blank().BG
	txt := " " + label + "  F3 "
	rs := []rune(txt)
	x0 := w - len(rs) - 3
	if x0 < 0 {
		return
	}
	g.Set(x0, y, layout.Cell{R: '◖', A: layout.Attr{FG: col, BG: bg}})
	for i, r := range rs {
		g.Set(x0+1+i, y, layout.Cell{R: r, A: layout.Attr{FG: dark, BG: col, Bold: true}})
	}
	g.Set(x0+1+len(rs), y, layout.Cell{R: '◗', A: layout.Attr{FG: col, BG: bg}})
}

// cellArrange places the leaves of a built-in screen by the theme's per-screen fractions (19.8), in
// the panels area above the text; leaves the theme does not name stack in a strip at the bottom of
// that area. It reports false when the theme has no arrangement for the screen.
func (a *arranger) cellArrange(rest *pack.BoundNode, panels Rect, th Theme, screen string) bool {
	arr := jmap(jmap(cellDoc(th), "arrange"), screen)
	if arr == nil {
		return false
	}
	var leaves []*pack.BoundNode
	var walk func(b *pack.BoundNode)
	walk = func(b *pack.BoundNode) {
		if !b.Visible {
			return
		}
		if isContainer(b) {
			for _, k := range b.Children {
				walk(k)
			}
			return
		}
		if b.Widget != "" && b.Widget != "textport" {
			leaves = append(leaves, b)
		}
	}
	walk(rest)
	counts := map[string]int{}
	type item struct {
		b *pack.BoundNode
		r []float64
	}
	var named []item
	var loose []*pack.BoundNode
	var ctrlRect []float64
	for _, k := range []string{"button", "toggle", "slider"} {
		if r := fracRect(arr, k+"#0"); r != nil && ctrlRect == nil {
			ctrlRect = r
		}
	}
	for _, b := range leaves {
		key := b.Widget + "#" + itoa(counts[b.Widget])
		counts[b.Widget]++
		if r := fracRect(arr, key); r != nil && (b.Widget != "button" && b.Widget != "toggle" && b.Widget != "slider") {
			named = append(named, item{b, r})
			continue
		}
		loose = append(loose, b)
	}
	// the controls share the theme's rectangle for the first control, wrapped like a row of pills
	var rows int
	if len(loose) > 0 {
		rows = min(len(loose), 3)
	}
	area := Rect{X: panels.X, Y: panels.Y, W: panels.W, H: max(panels.H-rows, 1)}
	for _, it := range named {
		q := *it.b
		a.leaves = append(a.leaves, placed{&q, fractionRect(area, it.r)})
	}
	var ctrls, other []*pack.BoundNode
	for _, b := range loose {
		if b.Widget == "button" || b.Widget == "toggle" || b.Widget == "slider" {
			ctrls = append(ctrls, b)
		} else {
			other = append(other, b)
		}
	}
	if len(ctrls) > 0 {
		cr := area
		if ctrlRect != nil {
			cr = fractionRect(area, ctrlRect)
		}
		x, y := cr.X, cr.Y
		for _, b := range ctrls {
			kw, _ := intrinsic(b, cr.W)
			if x > cr.X && x+kw > cr.X+cr.W {
				x, y = cr.X, y+1
			}
			if y >= cr.Y+cr.H {
				break
			}
			q := *b
			a.leaves = append(a.leaves, placed{&q, Rect{X: x, Y: y, W: min(kw, cr.W), H: 1}})
			x += kw + 1
		}
	}
	y := area.Y + area.H
	for _, b := range other {
		if y >= panels.Y+panels.H {
			break
		}
		q := *b
		a.leaves = append(a.leaves, placed{&q, Rect{X: panels.X, Y: y, W: panels.W, H: 1}})
		y++
	}
	return true
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	s := ""
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	return s
}

func fracRect(m map[string]any, k string) []float64 {
	l, _ := m[k].([]any)
	if len(l) != 4 {
		return nil
	}
	out := make([]float64, 4)
	for i, v := range l {
		out[i], _ = v.(float64)
	}
	return out
}

// fractionRect scales [x y w h] fractions into an area, leaving no gaps and no overlap on shared edges.
func fractionRect(a Rect, f []float64) Rect {
	x0 := a.X + int(math.Round(f[0]*float64(a.W)))
	y0 := a.Y + int(math.Round(f[1]*float64(a.H)))
	x1 := a.X + int(math.Round((f[0]+f[2])*float64(a.W)))
	y1 := a.Y + int(math.Round((f[1]+f[3])*float64(a.H)))
	return Rect{X: x0, Y: y0, W: max(x1-x0-1, 1), H: max(y1-y0, 1)} // one column of air between neighbours
}

// cellFocusLine puts the keyboard state, or a pending consent question, on a row of the frame in
// inverse: the focused panel and the keys that work.
func (d *drawer) cellFocusLine(top, bottom int) {
	g := d.f.Grid
	txt := ""
	switch {
	case d.env.Consent != "":
		txt = " " + d.env.Consent + " "
	case d.st.Active:
		txt = " " + d.f.Header() + " "
	}
	if txt == "" {
		return
	}
	y := 0
	if top == 0 {
		y = g.H - 1
	}
	a := layout.Attr{FG: dark, BG: d.env.Theme.role("x"), Bold: true}
	for i, r := range []rune(clean(txt)) {
		if i >= g.W-14 {
			break
		}
		g.Set(1+i, y, layout.Cell{R: r, A: a})
	}
}

// cellBand draws a band op: coloured segments with templated text across a run of rows on one edge.
func (d *drawer) cellBand(o map[string]any, skip int) {
	th := d.env.Theme
	g := d.f.Grid
	w, h := g.W, g.H
	rows := int(jnum(o, "rows", 1))
	lo, hi := int(jnum(o, "from", 0)*float64(w)), w
	if _, ok := o["to"]; ok {
		hi = int(jnum(o, "to", 1) * float64(w))
	}
	segs := jlist(o, "segments")
	fixed, fills := 0, 0
	for _, sg := range segs {
		m, _ := sg.(map[string]any)
		if v, ok := m["w"].(float64); ok {
			fixed += int(v)
		} else {
			fills++
		}
	}
	free := max(hi-lo-fixed, 0)
	x := lo
	for _, sg := range segs {
		m, _ := sg.(map[string]any)
		sw := free
		if fills > 1 {
			sw = free / fills
		}
		if v, ok := m["w"].(float64); ok {
			sw = int(v)
		}
		col := th.edgeTheme().role(jstr(m, "role"))
		txt, _ := theme.Render(m["text"], d.themeVars())
		rs := []rune(clean(txt))
		for r := 0; r < rows; r++ {
			y := r + skip
			if jstr(o, "edge") == "bottom" {
				y = h - 1 - r - skip
			}
			for k := 0; k < sw && x+k < w; k++ {
				ch := ' '
				if r == 0 && len(rs) > 0 {
					off := 1
					switch jstr(m, "align") {
					case "center":
						off = (sw - len(rs)) / 2
					case "right":
						off = sw - len(rs) - 1
					}
					if i := k - off; i >= 0 && i < len(rs) {
						ch = rs[i]
					}
				}
				g.Set(x+k, y, layout.Cell{R: ch, A: layout.Attr{FG: dark, BG: col, Bold: true}})
			}
		}
		x += sw
	}
}
