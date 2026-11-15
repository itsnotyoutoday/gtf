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
	f   *Frame
	env Env
	st  *State
	c   *canvas
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
		return 30, len(asMap(prop(b, "rows"))) + labelRow(b)
	case "list", "log":
		return 30, len(asMap(prop(b, "rows"))) + labelRow(b)
	case "table":
		return 30, len(asMap(prop(b, "rows"))) + 1
	case "button":
		return runeLen(str(prop(b, "text"))) + 4, 1
	case "toggle":
		return runeLen(str(prop(b, "text"))) + 5, 1
	case "slider":
		return 30, 1
	case "helm-controls":
		return 40, 8
	case "radar":
		return 31, 15
	case "viewscreen":
		return 40, 12
	case "schematic":
		return 36, 14
	case "contact-picker":
		return 34, len(asMap(prop(b, "contacts"))) + 1
	case "group-picker":
		return 24, len(asMap(prop(b, "groups"))) + 1
	case "team-assigner":
		n := max(len(asMap(prop(b, "teams"))), len(asList(prop(b, "systems"))))
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
		d.radar(b, r, foc)
	case "viewscreen":
		d.viewscreen(b, r)
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
	for i, ln := range strings.Split(str(prop(b, "text")), "\n") {
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
	fill := th.attr(d.autoTone(b, frac))
	d.c.bar(r.X+lw, r.Y, bw, frac, fill, th.dim())
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
	a := th.attr(th.tone(str(prop(b, "tone"))))
	dis := !b.Enabled || (b.Props["enabled"] != nil && !boolProp(b, "enabled"))
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
	n := d.c.text(r.X, r.Y, s, a, r.W)
	_ = n
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

func (d *drawer) buttonKeys(b *pack.BoundNode) func(Key) ([]Event, bool) {
	return func(k Key) ([]Event, bool) {
		if k != "enter" && k != "space" {
			return nil, false
		}
		return []Event{{Node: b.Node, Name: "click"}}, true
	}
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
