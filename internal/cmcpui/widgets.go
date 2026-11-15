// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package cmcpui

import (
	"fmt"
	"math"
	"strings"
	"unicode"

	"github.com/itsnotyoutoday/cmcp/pack"
	"github.com/itsnotyoutoday/gtf/internal/layout"
)

type drawer struct {
	f     *Frame
	env   Env
	st    *State
	c     *canvas
	pillN int // buttons and tabs drawn so far: the next colour of the theme's rainbow
}

func prop(b *pack.BoundNode, k string) any { return b.Props[k] }

func boolProp(b *pack.BoundNode, k string) bool { v, _ := b.Props[k].(bool); return v }

func (d *drawer) hasEvent(b *pack.BoundNode, ev string) bool {
	_, ok := b.Node.On[ev]
	return ok
}

// leafSize is the natural size of a widget.
func leafSize(b *pack.BoundNode, w int) (int, int) {
	switch b.Widget {
	case "label":
		t := str(prop(b, "text"))
		lines := strings.Split(t, "\n")
		m := 0
		for _, l := range lines {
			m = max(m, runeLen(l))
		}
		return m, len(lines)
	case "bar", "gauge":
		return 24, 1
	case "graph":
		return 24, 3
	case "bar-list":
		return 30, min(len(asMap(prop(b, "rows"))), maxListRows) + labelRow(b)
	case "list", "log":
		return 30, min(len(asMap(prop(b, "rows"))), maxListRows) + labelRow(b)
	case "table":
		return 30, min(len(asMap(prop(b, "rows"))), maxListRows) + 1
	case "button":
		return runeLen(str(prop(b, "text"))) + 4, 1
	case "toggle":
		return runeLen(str(prop(b, "text"))) + 5, 1
	case "slider":
		return 30, 1
	case "helm-controls":
		return 40, 8
	case "radar":
		if w > 0 && w < 31 {
			return w, max(5, w/2)
		}
		return 31, 15
	case "viewscreen":
		return 40, 12
	case "schematic":
		return 36, 14
	case "contact-picker":
		return 34, min(len(asMap(prop(b, "contacts"))), maxListRows) + 1
	case "group-picker":
		return 24, min(len(asMap(prop(b, "groups"))), maxListRows) + 1
	case "team-assigner":
		n := min(max(len(asMap(prop(b, "teams"))), len(asList(prop(b, "systems")))), maxListRows)
		return 44, n + 3
	case "sector-map":
		return 40, 14
	case "portrait":
		return 16, 8
	case "pad-diagram":
		return 30, 8
	}
	return 10, 1
}

// maxListRows caps how tall a list asks to be: longer ones scroll inside their region (spec 6.13).
const maxListRows = 12

func labelRow(b *pack.BoundNode) int {
	if str(prop(b, "label")) != "" {
		return 1
	}
	return 0
}

// collect registers the focusable items of a placed leaf (before drawing, so focus is known).
func (d *drawer) collect(p placed) {
	b := p.b
	it := Item{Node: b, Rect: p.r, Kind: b.Widget}
	switch b.Widget {
	case "button":
		it.key = d.buttonKeys(b)
		it.Mnemonic = d.mnemonicOf(b)
		it.Disabled = !b.Enabled || boolProp(b, "enabled") == false && b.Props["enabled"] != nil
	case "toggle":
		it.key = d.toggleKeys(b)
		it.Mnemonic = d.mnemonicOf(b)
		it.Disabled = !b.Enabled
	case "slider":
		it.key = d.sliderKeys(b)
		it.Disabled = !b.Enabled
	case "helm-controls":
		it.key = d.helmKeys(b)
	case "radar", "scope":
		it.key = d.radarKeys(b)
	case "contact-picker":
		it.key = d.rowKeys(b, "contacts", "select", true)
	case "group-picker":
		it.key = d.rowKeys(b, "groups", "select", false)
	case "list", "table", "bar-list", "log":
		if boolProp(b, "selectable") || d.hasEvent(b, "select") || d.hasEvent(b, "click") {
			it.key = d.rowKeys(b, "rows", pickEvent(b), false)
		}
	case "team-assigner":
		it.key = d.teamKeys(b)
		it.Disabled = !b.Enabled
	case "schematic":
		if d.hasEvent(b, "click") {
			it.key = d.zoneKeys(b)
			it.Disabled = !b.Enabled
		}
	case "pad-diagram":
		if d.hasEvent(b, "select") {
			it.key = d.rowKeys(b, "pads", "select", false)
		}
	}
	d.f.Items = append(d.f.Items, it)
}

func (d *drawer) mnemonicOf(b *pack.BoundNode) rune {
	if l := []rune(strings.ToUpper(d.env.Mnemonics[b.Node.Path])); len(l) == 1 {
		return l[0]
	}
	return 0
}

func pickEvent(b *pack.BoundNode) string {
	if _, ok := b.Node.On["select"]; ok {
		return "select"
	}
	return "click"
}

func (d *drawer) focusOf(b *pack.BoundNode) bool {
	return d.st.Active && d.st.Focus == b.Node.Path
}

func (d *drawer) draw(p placed) {
	b, r := p.b, p.r
	d.c.clip = r
	th := d.env.Theme
	foc := d.focusOf(b)
	switch b.Widget {
	case "label":
		d.label(b, r)
	case "bar":
		d.barw(b, r)
	case "gauge":
		d.gauge(b, r)
	case "graph":
		d.graph(b, r)
	case "bar-list":
		d.rows(b, r, "rows", foc, true)
	case "list", "log":
		d.rows(b, r, "rows", foc, false)
	case "table":
		d.table(b, r, foc)
	case "button":
		d.button(b, r, foc)
	case "toggle":
		d.toggle(b, r, foc)
	case "slider":
		d.slider(b, r, foc)
	case "helm-controls":
		d.helmControls(b, r, foc)
	case "radar", "scope":
		if d.env.ASCII {
			d.radarText(b, r, foc)
		} else {
			d.radar(b, r, foc)
		}
	case "viewscreen":
		if d.env.ASCII {
			d.viewscreenText(b, r)
		} else {
			d.viewscreen(b, r)
		}
	case "schematic":
		d.schematic(b, r, foc)
	case "contact-picker":
		d.contactPicker(b, r, foc)
	case "group-picker":
		d.groupPicker(b, r, foc)
	case "team-assigner":
		d.teamAssigner(b, r, foc)
	case "sector-map":
		d.sectorMap(b, r)
	case "portrait":
		d.portrait(b, r)
	case "pad-diagram":
		d.padDiagram(b, r, foc)
	case "lights":
		// decorative and optional: nothing drawn
	default:
		d.c.text(r.X, r.Y, "["+b.Widget+"]", th.dim(), r.W)
	}
	d.c.clip = Rect{W: d.f.Grid.W, H: d.f.Grid.H}
}

func (d *drawer) hint(it Item) string {
	switch it.Kind {
	case "button", "toggle":
		return "Enter: activate"
	case "slider":
		return "Left/Right: change"
	case "helm-controls":
		return "Up/Down: control, Left/Right: change, Enter: send"
	case "radar", "scope", "contact-picker", "group-picker", "list", "table", "bar-list", "log":
		return "Up/Down: choose, Enter: select"
	case "team-assigner":
		return "Up/Down: choose, Enter: pick team then system"
	case "schematic":
		return "Left/Right: zone, Enter: assign"
	}
	return ""
}

// header draws row 0: the screen title, the key mode and the hint of the focused widget.
func (d *drawer) header() {
	th := d.env.Theme
	g := d.f.Grid
	d.c.clip = Rect{W: g.W, H: 1}
	a := th.accent()
	a.Bold = true
	if d.env.Alert == "red" {
		a.FG = th.Token("color.alert")
	}
	if th.PaintBG {
		a.BG = th.Token("color.panel")
	}
	d.c.fill(Rect{W: g.W, H: 1}, ' ', a)
	x := d.c.text(1, 0, d.env.Header, a, g.W-2)
	right := "Tab: screen keys"
	at := th.dim()
	if th.PaintBG {
		at.BG = th.Token("color.panel")
	}
	if d.st.Active {
		right = "KEYS ON  Tab move  Enter act  underlined letter = button  Esc: input line"
		at = th.attr(th.Token("color.ok"))
		if th.PaintBG {
			at.BG = th.Token("color.panel")
		}
		at.Bold = true
	}
	if d.env.Notice != "" {
		right = d.env.Notice
		at = th.attr(th.Token("color.warn"))
		if th.PaintBG {
			at.BG = th.Token("color.panel")
		}
	}
	rx := g.W - runeLen(right) - 1
	if rx > x+2 {
		d.c.text(rx, 0, right, at, g.W-rx)
	}
}

// ---- simple widgets ----

func (d *drawer) label(b *pack.BoundNode, r Rect) {
	th := d.env.Theme
	a := th.text()
	switch strings.ToLower(str(prop(b, "style"))) {
	case "title", "heading":
		a = th.accent()
		a.Bold = true
	case "dim":
		a = th.dim()
	case "alert":
		a = th.attr(th.Token("color.alert"))
	}
	txt := str(prop(b, "text"))
	if th.Style != "" && isTab(txt) && r.H >= 1 {
		d.tab(txt, r)
		return
	}
	for i, ln := range strings.Split(txt, "\n") {
		if i >= r.H {
			break
		}
		d.c.text(r.X, r.Y+i, ln, a, r.W)
	}
}

func fracOf(b *pack.BoundNode) (v, m, frac float64) {
	v = numOr(prop(b, "value"), 0)
	m = numOr(prop(b, "max"), 100)
	if m > 0 {
		frac = v / m
	}
	return
}

func (d *drawer) autoTone(b *pack.BoundNode, frac float64) layout.Color {
	th := d.env.Theme
	if t := str(prop(b, "tone")); t != "" {
		return th.tone(t)
	}
	return th.Token("color.accent")
}

func (d *drawer) barw(b *pack.BoundNode, r Rect) {
	th := d.env.Theme
	v, m, frac := fracOf(b)
	lab := str(prop(b, "label"))
	txt := str(prop(b, "text"))
	if txt == "" {
		txt = fmt.Sprintf("%s/%s", fmtNum(round1(v)), fmtNum(round1(m)))
	}
	lw := 0
	if lab != "" {
		lw = min(runeLen(lab)+1, max(r.W/3, 6))
		d.c.text(r.X, r.Y, lab, th.text(), lw-1)
	}
	tw := runeLen(txt) + 1
	bw := r.W - lw - tw
	if bw < 4 {
		tw, bw = 0, r.W-lw
	}
	if th.Style != "" {
		from, to := d.bandColor(d.pillN), d.bandColor(d.pillN+2)
		d.pillN++
		if t := str(prop(b, "tone")); t != "" {
			to = th.tone(t)
			from = layout.Lerp(to, rgbC(0, 0, 0), 0.45)
		}
		d.c.gradBar(r.X+lw, r.Y, bw, frac, from, to, rgbC(26, 26, 38), th.blank().BG)
	} else {
		fill := th.attr(d.autoTone(b, frac))
		d.c.bar(r.X+lw, r.Y, bw, frac, fill, th.dim())
	}
	if tw > 0 {
		d.c.text(r.X+r.W-tw+1, r.Y, txt, th.text(), tw-1)
	}
}

func round1(f float64) float64 { return math.Round(f*10) / 10 }

func (d *drawer) gauge(b *pack.BoundNode, r Rect) {
	th := d.env.Theme
	_, _, frac := fracOf(b)
	lab := str(prop(b, "label"))
	pct := fmt.Sprintf("%3.0f%%", math.Max(0, math.Min(1, frac))*100)
	x := r.X
	if lab != "" {
		x += d.c.text(x, r.Y, lab, th.text(), r.W/2) + 1
	}
	d.c.put(x, r.Y, '◖', th.dim())
	bw := r.X + r.W - x - 2 - len(pct) - 1
	col := th.Token("color.ok")
	if frac < 0.5 {
		col = th.Token("color.warn")
	}
	if frac < 0.25 {
		col = th.Token("color.alert")
	}
	d.c.bar(x+1, r.Y, bw, frac, th.attr(col), th.dim())
	d.c.put(x+1+bw, r.Y, '◗', th.dim())
	d.c.text(x+bw+3, r.Y, pct, th.text(), len(pct))
}

var blocks = []rune{' ', '▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}

func (d *drawer) graph(b *pack.BoundNode, r Rect) {
	th := d.env.Theme
	ws := d.st.ws(b.Node.Path)
	v := numOr(prop(b, "value"), math.NaN())
	n := int(numOr(prop(b, "samples"), 120))
	if !math.IsNaN(v) {
		// one sample per change of the bound value or per redraw tick (the caller redraws on data)
		if len(ws.hist) == 0 || ws.lastVal != v || d.env.Now.Second() != 0 {
			ws.hist = append(ws.hist, v)
		}
		ws.lastVal = v
		if len(ws.hist) > max(n, 8) {
			ws.hist = ws.hist[len(ws.hist)-max(n, 8):]
		}
	}
	lab := str(prop(b, "label"))
	top := r.Y
	if lab != "" {
		d.c.text(r.X, r.Y, lab, th.text(), r.W)
		top++
	}
	h := r.Y + r.H - top
	if h <= 0 {
		return
	}
	lo, hi := numOr(prop(b, "min"), 0), numOr(prop(b, "max"), 100)
	if hi <= lo {
		hi = lo + 1
	}
	hist := ws.hist
	if len(hist) > r.W {
		hist = hist[len(hist)-r.W:]
	}
	a := th.accent()
	for i, val := range hist {
		x := r.X + r.W - len(hist) + i
		lv := (val - lo) / (hi - lo) * float64(h*8)
		lv = math.Max(0, math.Min(float64(h*8), lv))
		for row := 0; row < h; row++ {
			cellLv := int(lv) - (h-1-row)*8
			ch := blocks[max(0, min(8, cellLv))]
			if ch != ' ' {
				d.c.put(x, top+row, ch, a)
			}
		}
	}
}

func (d *drawer) button(b *pack.BoundNode, r Rect, foc bool) {
	th := d.env.Theme
	txt := str(prop(b, "text"))
	dis := !b.Enabled || (b.Props["enabled"] != nil && !boolProp(b, "enabled"))
	if th.Style != "" {
		d.pill(b, r, txt, foc, dis)
		return
	}
	a := th.attr(th.tone(str(prop(b, "tone"))))
	if dis {
		a = th.dim()
	}
	if foc {
		a = focused(a)
	}
	s := "[ " + txt + " ]"
	if runeLen(s) > r.W {
		s = "[" + txt + "]"
	}
	d.c.text(r.X, r.Y, s, a, r.W)
	if i := d.f.index(b.Node.Path); i >= 0 && d.f.Items[i].Mnemonic != 0 {
		// the mnemonic letter is underlined
		m := d.f.Items[i].Mnemonic
		for k, ch := range []rune(clean(s)) {
			if unicode.ToUpper(ch) == m && k >= 1 {
				ua := a
				ua.Underline = true
				d.c.put(r.X+k, r.Y, ch, ua)
				break
			}
		}
	}
}

// pill draws a themed button: a filled, rounded-ended bar in the next colour of the theme's
// rainbow (tone buttons keep their tone), dark bold text, the mnemonic letter underlined; the
// focused one is brighter with a white edge.
func (d *drawer) pill(b *pack.BoundNode, r Rect, txt string, foc, dis bool) {
	th := d.env.Theme
	col := d.bandColor(d.pillN)
	d.pillN++
	switch strings.ToLower(str(prop(b, "tone"))) {
	case "alert", "danger":
		col = th.RGB("color.alert")
	case "warn":
		col = th.RGB("color.warn")
	}
	fg := dark
	if dis {
		col, fg = rgbC(60, 62, 74), rgbC(120, 124, 140)
	}
	if foc {
		col = layout.Lerp(col, rgbC(255, 255, 255), 0.45)
	}
	bg := th.blank().BG
	body := " " + txt + " "
	if runeLen(body)+2 > r.W {
		body = " " + string([]rune(clean(txt))[:max(r.W-4, 0)]) + " "
	}
	x := r.X
	d.c.put(x, r.Y, d.capL(), layout.Attr{FG: col, BG: bg})
	m := rune(0)
	if i := d.f.index(b.Node.Path); i >= 0 {
		m = d.f.Items[i].Mnemonic
	}
	used := false
	for k, ch := range []rune(clean(body)) {
		a := layout.Attr{FG: fg, BG: col, Bold: true}
		if !used && m != 0 && unicode.ToUpper(ch) == m && k >= 1 {
			a.Underline, used = true, true
		}
		d.c.put(x+1+k, r.Y, ch, a)
	}
	d.c.put(x+1+runeLen(body), r.Y, d.capR(), layout.Attr{FG: col, BG: bg})
}

func (d *drawer) toggle(b *pack.BoundNode, r Rect, foc bool) {
	th := d.env.Theme
	on := boolProp(b, "value")
	box := "[ ] "
	if on {
		box = "[x] "
	}
	a := th.text()
	if foc {
		a = focused(th.accent())
	}
	d.c.text(r.X, r.Y, box+str(prop(b, "text")), a, r.W)
}

func (d *drawer) toggleKeys(b *pack.BoundNode) func(Key) ([]Event, bool) {
	return func(k Key) ([]Event, bool) {
		if k != "enter" && k != "space" {
			return nil, false
		}
		return []Event{{Node: b.Node, Name: "change", Value: !boolProp(b, "value")}}, true
	}
}

func (d *drawer) sliderVal(b *pack.BoundNode) float64 {
	ws := d.st.ws(b.Node.Path)
	if d.env.Now.Before(ws.valUntil) {
		return ws.val
	}
	return numOr(prop(b, "value"), numOr(prop(b, "min"), 0))
}

func (d *drawer) slider(b *pack.BoundNode, r Rect, foc bool) {
	th := d.env.Theme
	lo, hi := numOr(prop(b, "min"), 0), numOr(prop(b, "max"), 100)
	v := d.sliderVal(b)
	lab := str(prop(b, "label"))
	x := r.X
	if lab != "" {
		x += d.c.text(x, r.Y, lab, th.text(), r.W/3) + 1
	}
	val := fmtNum(v)
	tw := len(val) + 1
	tw2 := r.X + r.W - x - tw - 2
	if tw2 < 3 {
		return
	}
	frac := 0.0
	if hi > lo {
		frac = (v - lo) / (hi - lo)
	}
	pos := int(math.Round(math.Max(0, math.Min(1, frac)) * float64(tw2-1)))
	a := th.dim()
	k := th.accent()
	if foc {
		k = focused(k)
	}
	d.c.put(x, r.Y, '◀', a)
	for i := 0; i < tw2; i++ {
		if i == pos {
			d.c.put(x+1+i, r.Y, '●', k)
		} else {
			d.c.put(x+1+i, r.Y, '─', a)
		}
	}
	d.c.put(x+1+tw2, r.Y, '▶', a)
	d.c.text(x+tw2+3, r.Y, val, th.text(), tw)
}

func (d *drawer) sliderKeys(b *pack.BoundNode) func(Key) ([]Event, bool) {
	return func(k Key) ([]Event, bool) {
		lo, hi := numOr(prop(b, "min"), 0), numOr(prop(b, "max"), 100)
		step := numOr(prop(b, "step"), math.Max((hi-lo)/20, 1))
		v := d.sliderVal(b)
		switch k {
		case "left", "down", "-":
			v -= step
		case "right", "up", "+", "=":
			v += step
		case "home":
			v = lo
		case "end":
			v = hi
		default:
			return nil, false
		}
		v = math.Max(lo, math.Min(hi, v))
		ws := d.st.ws(b.Node.Path)
		ws.val, ws.valUntil = v, d.env.Now.Add(2e9)
		return []Event{{Node: b.Node, Name: "change", Value: v}}, true
	}
}

func focusedIf(a layout.Attr, f bool) layout.Attr {
	if f {
		return focused(a)
	}
	return a
}

// ---- row widgets ----

// isTab says whether a label is a section heading: short, upper case, no value in it.
func isTab(s string) bool {
	if s == "" || runeLen(s) > 28 || strings.ContainsAny(s, ":\n") || strings.Contains(s, " - ") || strings.Contains(s, "  ") {
		return false
	}
	return s == strings.ToUpper(s) && strings.IndexFunc(s, unicode.IsLetter) >= 0
}

// tab draws a section heading as a coloured tab with a thin rule running on to the edge.
func (d *drawer) tab(txt string, r Rect) {
	th := d.env.Theme
	col := d.bandColor(d.pillN)
	d.pillN++
	bg := th.blank().BG
	n := runeLen(txt) + 2
	d.c.put(r.X, r.Y, d.capL(), layout.Attr{FG: col, BG: bg})
	for k, ch := range []rune(clean(" " + txt + " ")) {
		d.c.put(r.X+1+k, r.Y, ch, layout.Attr{FG: dark, BG: col, Bold: true})
	}
	d.c.put(r.X+1+n, r.Y, d.capR(), layout.Attr{FG: col, BG: bg})
	for x := r.X + n + 3; x < r.X+r.W; x++ {
		d.c.put(x, r.Y, '▔', layout.Attr{FG: layout.Lerp(col, rgbC(0, 0, 0), 0.55), BG: bg})
	}
}

// gradBar draws a bar whose fill runs through a colour ramp in truecolor, on a dark track.
func (c *canvas) gradBar(x, y, w int, frac float64, from, to, track, bg layout.Color) {
	if w <= 0 {
		return
	}
	frac = math.Max(0, math.Min(1, frac))
	full := frac * float64(w)
	n := int(full)
	part := int((full - float64(n)) * 8)
	for i := 0; i < w; i++ {
		col := layout.Lerp(from, to, float64(i)/float64(max(w-1, 1)))
		switch {
		case i < n:
			c.put(x+i, y, '█', layout.Attr{FG: col, BG: bg})
		case i == n && part > 0:
			c.put(x+i, y, eighths[part], layout.Attr{FG: col, BG: track})
		default:
			c.put(x+i, y, ' ', layout.Attr{BG: track})
		}
	}
}

func (d *drawer) buttonKeys(b *pack.BoundNode) func(Key) ([]Event, bool) {
	return func(k Key) ([]Event, bool) {
		if k != "enter" && k != "space" {
			return nil, false
		}
		return []Event{{Node: b.Node, Name: "click"}}, true
	}
}

// navNode is the synthetic widget node of the screen menu, so the menu is a panel like any other.
var navNode = &pack.BoundNode{Node: &pack.Node{Path: "~nav", Widget: "nav"}, Widget: "nav", Visible: true, Enabled: true}

// navItem registers the screen menu (the left column) as a focusable panel: Up and Down move its
// cursor, Enter shows the entry (an unavailable one reports why it is not shown).
func (d *drawer) navItem(top, bottom, left int) {
	if left == 0 || len(d.env.Nav) == 0 {
		return
	}
	g := d.f.Grid
	it := Item{Node: navNode, Rect: Rect{X: 0, Y: top, W: left, H: g.H - top - bottom}, Kind: "nav"}
	ws := d.st.ws("~nav")
	it.key = func(k Key) ([]Event, bool) {
		n := len(d.env.Nav)
		ws.cur = max(0, min(ws.cur, n-1))
		switch k {
		case "up":
			ws.cur = (ws.cur - 1 + n) % n
			return nil, true
		case "down":
			ws.cur = (ws.cur + 1) % n
			return nil, true
		case "home":
			ws.cur = 0
			return nil, true
		case "end":
			ws.cur = n - 1
			return nil, true
		case "pgup":
			ws.cur = max(ws.cur-5, 0)
			return nil, true
		case "pgdn":
			ws.cur = min(ws.cur+5, n-1)
			return nil, true
		case "enter", "space":
			return []Event{{Nav: d.env.Nav[ws.cur].ID}}, true
		}
		return nil, false
	}
	d.f.Items = append(d.f.Items, it)
}

// navEntries is the menu for drawing: the cursor entry marked while the menu has the keyboard.
func (d *drawer) navEntries() []NavEntry {
	out := append([]NavEntry(nil), d.env.Nav...)
	if d.st.Active && d.st.Focus == "~nav" && len(out) > 0 {
		ws := d.st.ws("~nav")
		out[max(0, min(ws.cur, len(out)-1))].Cursor = true
	}
	return out
}

// FocusHeader is the focus header of spec 6.12.2 (exact templates, three spaces between key
// groups): it names the area and lists the keys valid there. label is the highlighted screen of
// the menu, name the focused panel's title. The [ ] and digit aliases are not advertised here;
// /cmcp keys lists them.
func FocusHeader(name, kind, label string) string {
	switch kind {
	case "":
		return "INPUT - Tab or F2: screen menu   Shift+Tab: last panel"
	case "nav":
		return "MENU " + label + " - Up/Down choose   Enter open   Home/End ends   Tab next area   Esc input line"
	case "tabs":
		return "PANEL " + name + " - Left/Right tab   Enter open   Tab next area   Shift+Tab back   Esc input line"
	case "button", "toggle":
		return "PANEL " + name + " - Left/Right button   Enter press   letter presses   Tab next area   Esc input line"
	case "slider":
		return "PANEL " + name + " - Left/Right adjust   Tab next area   Shift+Tab back   Esc input line"
	}
	return "PANEL " + name + " - Up/Down row   Enter select   Tab next area   Shift+Tab back   Esc input line"
}

// Header is the frame's focus header (INPUT when the screen has no focus).
func (f *Frame) Header() string { return FocusHeader(f.FocusName, f.FocusKind, f.FocusLabel) }

// focusName names an item for the "FOCUS:" line.
func (d *drawer) focusName(it Item) string {
	b := it.Node
	switch it.Kind {
	case "nav":
		return "SCREEN MENU"
	case "button", "toggle":
		return strings.ToUpper(str(prop(b, "text")))
	case "slider":
		return strings.ToUpper(orStrC(str(prop(b, "label")), "SLIDER"))
	case "radar", "scope":
		return "RADAR (arrows: contact)"
	case "viewscreen":
		return "VIEWSCREEN"
	case "contact-picker":
		return "CONTACTS"
	case "table":
		return strings.ToUpper(orStrC(str(prop(b, "label")), "TABLE"))
	case "team-assigner":
		return "DAMAGE TEAMS"
	case "helm-controls":
		return "HELM CONTROLS"
	}
	return strings.ToUpper(orStrC(str(prop(b, "label")), strings.ReplaceAll(it.Kind, "-", " ")))
}

func orStrC(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

// markFocus makes the focused panel unmistakable in any terminal, 16 colours or mono included: its
// first row (a picture's label row) is drawn in inverse bold with a marker and the panel's name, on
// top of the widget's own highlight (cursor row, brighter pill).
func (d *drawer) markFocus(it Item) {
	g := d.f.Grid
	r := it.Rect
	if it.Kind == "nav" || r.W <= 0 || r.H <= 0 {
		return // the menu marks its own cursor row
	}
	label := "▶ " + d.focusName(it)
	row := r.Y
	if it.Kind == "radar" || it.Kind == "scope" {
		row = r.Y + r.H - 1
	}
	for x := r.X; x < r.X+r.W && x < g.W; x++ {
		c := g.At(x, row)
		c.A.Reverse, c.A.Bold = true, true
		g.Set(x, row, c)
	}
	if it.Kind != "button" && it.Kind != "toggle" && it.Kind != "slider" {
		for i, ch := range []rune(clean(label)) {
			if i >= r.W-1 {
				break
			}
			c := g.At(r.X+i, row)
			c.R = ch
			g.Set(r.X+i, row, c)
		}
	}
}

// capL and capR are the end caps of pill-shaped controls: the pill's own half circles, or the theme's
// button glyphs (cell.widgets.button left/right, e.g. the klingon blades) when it asks for brackets.
func (d *drawer) capL() rune { return d.cap("left", '◖') }
func (d *drawer) capR() rune { return d.cap("right", '◗') }

func (d *drawer) cap(side string, def rune) rune {
	b := jmap(jmap(cellDoc(d.env.Theme), "widgets"), "button")
	if jstr(b, "style") == "bracket" {
		if rs := []rune(jstr(b, side)); len(rs) > 0 {
			return rs[0]
		}
	}
	return def
}
