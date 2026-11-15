// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package cmcpui

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/itsnotyoutoday/cmcp/pack"
	"github.com/itsnotyoutoday/gtf/internal/layout"
)

// rowKeysOf lists the keys of the map in prop name, nearest first for contacts.
func (d *drawer) rowKeysOf(b *pack.BoundNode, name string, byDist bool) (map[string]any, []string) {
	m := asMap(prop(b, name))
	keys := sortedKeys(m)
	if byDist {
		sort.SliceStable(keys, func(i, j int) bool {
			return numOr(asMap(m[keys[i]])["distance_m"], math.MaxFloat64) < numOr(asMap(m[keys[j]])["distance_m"], math.MaxFloat64)
		})
	}
	return m, keys
}

// rowKeys is the keyboard handling of a list: Up/Down move the cursor (at the ends the focus moves
// on), Enter or Space selects the row under it.
func (d *drawer) rowKeys(b *pack.BoundNode, name, event string, byDist bool) func(Key) ([]Event, bool) {
	return func(k Key) ([]Event, bool) {
		_, keys := d.rowKeysOf(b, name, byDist)
		ws := d.st.ws(b.Node.Path)
		ws.cur = max(0, min(ws.cur, len(keys)-1))
		switch k {
		case "up":
			if ws.cur > 0 {
				ws.cur--
				return nil, true
			}
		case "down":
			if ws.cur < len(keys)-1 {
				ws.cur++
				return nil, true
			}
		case "home":
			ws.cur = 0
			return nil, true
		case "end":
			ws.cur = max(len(keys)-1, 0)
			return nil, true
		case "enter", "space":
			if len(keys) == 0 {
				return nil, true
			}
			return []Event{{Node: b.Node, Name: event, Value: keyValue(keys[ws.cur])}}, true
		}
		return nil, false
	}
}

// visible window of a cursor list.
func (d *drawer) window(ws *wstate, n, h int) (from, to int) {
	if h <= 0 {
		return 0, 0
	}
	ws.cur = max(0, min(ws.cur, n-1))
	if ws.cur < ws.scroll {
		ws.scroll = ws.cur
	}
	if ws.cur >= ws.scroll+h {
		ws.scroll = ws.cur - h + 1
	}
	ws.scroll = max(0, min(ws.scroll, max(n-h, 0)))
	return ws.scroll, min(n, ws.scroll+h)
}

func rowName(k string, v any) string {
	if m := asMap(v); m != nil {
		for _, f := range []string{"name", "title", "text"} {
			if s := str(m[f]); s != "" {
				return s
			}
		}
	}
	return k
}

func (d *drawer) marker(b *pack.BoundNode, k string, i int, foc bool, ws *wstate) (rune, layout.Attr) {
	th := d.env.Theme
	sel := false
	if s, ok := b.Props["selected"]; ok && s != nil {
		sel = str(s) == k
	}
	switch {
	case foc && ws.cur == i:
		return '>', focused(th.accent())
	case sel:
		return '*', th.accent()
	}
	return ' ', th.text()
}

// rows draws list, bar-list and log.
func (d *drawer) rows(b *pack.BoundNode, r Rect, name string, foc, bars bool) {
	th := d.env.Theme
	m, keys := d.rowKeysOf(b, name, false)
	y := r.Y
	if lab := str(prop(b, "label")); lab != "" {
		a := th.accent()
		a.Bold = true
		d.c.text(r.X, y, lab, a, r.W)
		y++
	}
	h := r.Y + r.H - y
	ws := d.st.ws(b.Node.Path)
	if b.Widget == "log" {
		// newest at the bottom: keep the tail in view unless the cursor moved
		if mx := int(numOr(prop(b, "max"), 0)); mx > 0 && len(keys) > mx {
			keys = keys[len(keys)-mx:]
		}
		ws.cur = len(keys) - 1
	}
	from, to := d.window(ws, len(keys), h)
	nw := 0
	for _, k := range keys {
		nw = max(nw, runeLen(rowName(k, m[k])))
	}
	nw = min(nw, max(r.W/2, 8))
	for i := from; i < to; i++ {
		k := keys[i]
		v := m[k]
		yy := y + i - from
		mk, ma := d.marker(b, k, i, foc, ws)
		d.c.put(r.X, yy, mk, ma)
		name := rowName(k, v)
		if bars {
			d.c.text(r.X+2, yy, name, th.text(), nw)
			d.rowBar(r, yy, r.X+3+nw, v)
			continue
		}
		d.c.text(r.X+2, yy, d.rowText(b, k, v), th.text(), r.W-2)
	}
	if len(keys) > to-from && r.W > 2 {
		d.c.put(r.X+r.W-1, y, '▲', th.dim())
		d.c.put(r.X+r.W-1, y+h-1, '▼', th.dim())
	}
}

func (d *drawer) rowBar(r Rect, y, x int, v any) {
	th := d.env.Theme
	mv := asMap(v)
	var frac float64
	txt := ""
	if _, ok := mv["pct"]; ok {
		p := numOr(mv["pct"], 0)
		frac, txt = p/100, fmtNum(p)+"%"
	} else {
		cur, mx := numOr(mv["cur"], 0), numOr(mv["max"], 0)
		if mx > 0 {
			frac = cur / mx
		}
		txt = fmt.Sprintf("%s/%s", fmtNum(round1(cur)), fmtNum(round1(mx)))
	}
	col := th.Token("color.ok")
	if frac < 0.6 {
		col = th.Token("color.warn")
	}
	if frac < 0.3 {
		col = th.Token("color.alert")
	}
	tw := runeLen(txt) + 1
	bw := r.X + r.W - x - tw
	if bw < 3 {
		bw, tw = r.X+r.W-x, 0
	}
	d.c.bar(x, y, bw, frac, th.attr(col), th.dim())
	if tw > 0 {
		d.c.text(x+bw+1, y, txt, th.text(), tw-1)
	}
}

func (d *drawer) rowText(b *pack.BoundNode, k string, v any) string {
	mv := asMap(v)
	if mv == nil {
		return k + "  " + str(v)
	}
	if b.Widget == "log" {
		return strings.TrimSpace(str(mv["from"]) + " " + str(mv["text"]))
	}
	if cols := asList(prop(b, "columns")); len(cols) > 0 {
		var p []string
		for _, c := range cols {
			p = append(p, str(path(mv, str(asMap(c)["key"]))))
		}
		return strings.Join(p, "  ")
	}
	var p []string
	for _, f := range sortedKeys(mv) {
		if s := str(mv[f]); s != "" && f != "name" {
			p = append(p, f+"="+s)
		}
	}
	return rowName(k, v) + "  " + strings.Join(p, " ")
}

func path(m map[string]any, p string) any {
	var cur any = m
	for _, part := range strings.Split(p, ".") {
		mm := asMap(cur)
		if mm == nil {
			return nil
		}
		cur = mm[part]
	}
	return cur
}

func (d *drawer) table(b *pack.BoundNode, r Rect, foc bool) {
	th := d.env.Theme
	m, keys := d.rowKeysOf(b, "rows", false)
	cols := asList(prop(b, "columns"))
	type col struct {
		key, title string
		w          int
	}
	var cs []col
	for _, c := range cols {
		cm := asMap(c)
		k := str(cm["key"])
		t := str(cm["title"])
		if t == "" {
			t = strings.ToUpper(k)
		}
		w := runeLen(t)
		for _, rk := range keys {
			w = max(w, runeLen(str(path(asMap(m[rk]), k))))
		}
		cs = append(cs, col{k, t, min(w, 22)})
	}
	total := 2
	for _, c := range cs {
		total += c.w + 1
	}
	for total > r.W && len(cs) > 0 { // too wide: shrink the widest columns
		wi := 0
		for i, c := range cs {
			if c.w > cs[wi].w {
				wi = i
			}
		}
		if cs[wi].w <= 3 {
			break
		}
		cs[wi].w--
		total--
	}
	hdr := th.accent()
	hdr.Bold = true
	x := r.X + 2
	for _, c := range cs {
		if x < r.X+r.W {
			d.c.text(x, r.Y, c.title, hdr, c.w)
		}
		x += c.w + 1
	}
	ws := d.st.ws(b.Node.Path)
	from, to := d.window(ws, len(keys), r.H-1)
	for i := from; i < to; i++ {
		k := keys[i]
		yy := r.Y + 1 + i - from
		mk, ma := d.marker(b, k, i, foc, ws)
		d.c.put(r.X, yy, mk, ma)
		x := r.X + 2
		for _, c := range cs {
			v := path(asMap(m[k]), c.key)
			if c.key == "" || (asMap(m[k]) == nil) {
				v = m[k]
			}
			d.c.text(x, yy, str(v), th.text(), c.w)
			x += c.w + 1
		}
	}
}

func (d *drawer) contactPicker(b *pack.BoundNode, r Rect, foc bool) {
	th := d.env.Theme
	m, keys := d.rowKeysOf(b, "contacts", true)
	ws := d.st.ws(b.Node.Path)
	from, to := d.window(ws, len(keys), r.H)
	for i := from; i < to; i++ {
		k := keys[i]
		c := asMap(m[k])
		yy := r.Y + i - from
		mk, ma := d.marker(b, k, i, foc, ws)
		d.c.put(r.X, yy, mk, ma)
		line := fmt.Sprintf("%-3s %-16s %-7s %s", k, str(c["name"]), str(c["bearing"]), str(c["distance_text"]))
		a := th.text()
		if iff := str(c["iff"]); iff != "" {
			a = th.attr(iffColor(th, iff))
		}
		d.c.text(r.X+2, yy, line, a, r.W-2)
	}
	if len(keys) == 0 {
		d.c.text(r.X, r.Y, "no contacts", th.dim(), r.W)
	}
}

func iffColor(th Theme, iff string) layout.Color {
	switch iff {
	case "friend", "friendly":
		return th.Token("color.ok")
	case "hostile", "enemy":
		return th.Token("color.alert")
	case "neutral":
		return th.Token("color.accent2")
	}
	return th.Token("color.warn")
}

func (d *drawer) groupPicker(b *pack.BoundNode, r Rect, foc bool) {
	th := d.env.Theme
	m, keys := d.rowKeysOf(b, "groups", false)
	ws := d.st.ws(b.Node.Path)
	from, to := d.window(ws, len(keys), r.H)
	for i := from; i < to; i++ {
		k := keys[i]
		g := asMap(m[k])
		yy := r.Y + i - from
		mk, ma := d.marker(b, k, i, foc, ws)
		d.c.put(r.X, yy, mk, ma)
		d.c.text(r.X+2, yy, fmt.Sprintf("%-2s %-14s", k, str(g["name"])), th.text(), r.W-2)
		d.rowBar(Rect{X: r.X, Y: r.Y, W: r.W, H: r.H}, yy, r.X+2+17, map[string]any{"pct": g["pct"]})
	}
}

// ---- team assigner ----

func (d *drawer) teamKeys(b *pack.BoundNode) func(Key) ([]Event, bool) {
	return func(k Key) ([]Event, bool) {
		teams := asMap(prop(b, "teams"))
		tk := sortedKeys(teams)
		sys := asList(prop(b, "systems"))
		ws := d.st.ws(b.Node.Path)
		ws.cur = max(0, min(ws.cur, len(tk)-1))
		ws.zone = max(0, min(ws.zone, len(sys)-1))
		if ws.stage == 0 {
			switch k {
			case "up":
				if ws.cur > 0 {
					ws.cur--
					return nil, true
				}
			case "down":
				if ws.cur < len(tk)-1 {
					ws.cur++
					return nil, true
				}
			case "enter", "space":
				if len(tk) == 0 || len(sys) == 0 {
					return nil, true
				}
				ws.stage, ws.team = 1, tk[ws.cur]
				d.st.UI["team"] = keyValue(ws.team)
				return nil, true
			}
			return nil, false
		}
		switch k {
		case "up":
			ws.zone = max(ws.zone-1, 0)
		case "down":
			ws.zone = min(ws.zone+1, len(sys)-1)
		case "esc", "left":
			ws.stage = 0
		case "enter", "space":
			ws.stage = 0
			return []Event{{Node: b.Node, Name: "assign", Value: map[string]any{"team": keyValue(ws.team), "system": str(sys[ws.zone])}}}, true
		default:
			return nil, false
		}
		return nil, true
	}
}

func (d *drawer) teamAssigner(b *pack.BoundNode, r Rect, foc bool) {
	th := d.env.Theme
	teams := asMap(prop(b, "teams"))
	tk := sortedKeys(teams)
	sys := asList(prop(b, "systems"))
	ws := d.st.ws(b.Node.Path)
	hdr := th.accent()
	hdr.Bold = true
	lw := max(r.W/2, 16)
	// a narrow widget stacks the two lists instead of putting them side by side
	stack := r.W < 44
	sx, sy := r.X+lw, r.Y // where the systems list goes
	rows := r.H - 2
	if stack {
		lw = r.W
		nt := min(len(tk), max((r.H-3)/2, 1))
		rows = nt
		sx, sy = r.X, r.Y+nt+1
	}
	d.c.text(r.X, r.Y, "TEAMS", hdr, lw)
	if stack {
		d.c.text(sx, sy, "ASSIGN TO", hdr, r.W)
	} else {
		d.c.text(sx, sy, "ASSIGN TO", hdr, r.W-lw)
	}
	f0, t0 := d.window(&wstate{cur: ws.cur}, len(tk), rows)
	for i := f0; i < t0; i++ {
		t := asMap(teams[tk[i]])
		a := th.text()
		mk := ' '
		if foc && ws.stage == 0 && ws.cur == i {
			mk, a = '>', focused(th.accent())
		} else if ws.stage == 1 && tk[i] == ws.team {
			mk = '*'
			a = th.accent()
		}
		d.c.put(r.X, r.Y+1+i-f0, mk, a)
		d.c.text(r.X+2, r.Y+1+i-f0, fmt.Sprintf("T%-2s crew %-3s %s", tk[i], str(t["crew"]), str(t["assign"])), a, lw-2)
	}
	for i, s := range sys {
		if sy+1+i >= r.Y+r.H-1 {
			break
		}
		a := th.text()
		mk := ' '
		if ws.stage == 1 && foc && ws.zone == i {
			mk, a = '>', focused(th.accent())
		}
		d.c.put(sx, sy+1+i, mk, a)
		d.c.text(sx+2, sy+1+i, str(s), a, r.X+r.W-sx-2)
	}
	msg := "Enter: choose a team"
	if ws.stage == 1 {
		msg = "Team " + ws.team + ": choose a system, Enter assigns, Esc back"
	}
	d.c.text(r.X, r.Y+r.H-1, msg, th.dim(), r.W)
}

// ---- radar ----

type blip struct {
	cid   string
	name  string
	az, d float64
	typ   string
	iff   string
	rel   map[string]any
}

func (d *drawer) blips(b *pack.BoundNode) []blip {
	m, keys := d.rowKeysOf(b, "contacts", true)
	var out []blip
	for _, k := range keys {
		c := asMap(m[k])
		bear := asMap(c["rel"])
		if bear == nil {
			bear = asMap(c["bearing"])
		}
		out = append(out, blip{cid: k, name: str(c["name"]), az: numOr(bear["az"], 0), d: numOr(c["distance_m"], 0), typ: str(c["type"]), iff: str(c["iff"]), rel: bear})
	}
	return out
}

func (d *drawer) radarKeys(b *pack.BoundNode) func(Key) ([]Event, bool) {
	return func(k Key) ([]Event, bool) {
		bl := d.blips(b)
		ws := d.st.ws(b.Node.Path)
		ws.cur = max(0, min(ws.cur, len(bl)-1))
		switch k {
		case "up", "left":
			if ws.cur > 0 {
				ws.cur--
				return nil, true
			}
		case "down", "right":
			if ws.cur < len(bl)-1 {
				ws.cur++
				return nil, true
			}
		case "enter", "space":
			if len(bl) == 0 {
				return nil, true
			}
			return []Event{{Node: b.Node, Name: "select", Value: keyValue(bl[ws.cur].cid)}}, true
		}
		return nil, false
	}
}

func (d *drawer) radar(b *pack.BoundNode, r Rect, foc bool) {
	th := d.env.Theme
	ws := d.st.ws(b.Node.Path)
	bl := d.blips(b)
	ws.cur = max(0, min(ws.cur, len(bl)-1))
	info := r.H >= 6
	dh := r.H
	if info {
		dh--
	}
	pw, ph := r.W*2, dh*4
	rad := float64(min(pw, ph)/2 - 1)
	cx, cy := float64(pw)/2, float64(ph)/2
	bd := newBraille(r.W, dh)
	ring := th.Token("color.dim")
	for i := 1; i <= 3; i++ {
		bd.circle(cx, cy, rad*float64(i)/3, rad*float64(i)/3, ring)
	}
	bd.line(int(cx), 0, int(cx), ph-1, ring)
	bd.line(0, int(cy), pw-1, int(cy), ring)
	// the sweep: one turn in 6 s
	ang := math.Mod(float64(d.env.Now.UnixMilli())/6000, 1) * 2 * math.Pi
	bd.line(int(cx), int(cy), int(cx+rad*math.Sin(ang)), int(cy-rad*math.Cos(ang)), th.Token("color.accent2"))
	bd.blit(d.c, r.X, r.Y, th.blank())
	dmax := numOr(prop(b, "range_m"), 0)
	for _, x := range bl {
		dmax = math.Max(dmax, x.d)
	}
	sel := str(prop(b, "selected"))
	for i, x := range bl {
		rf := 0.0
		if dmax > 0 {
			rf = math.Log10(1+x.d) / math.Log10(1+dmax)
		}
		a := x.az * math.Pi / 180
		px, py := cx+rad*rf*math.Sin(a), cy-rad*rf*math.Cos(a)
		gx, gy := int(px)/2, int(py)/4
		g := '·'
		switch {
		case x.typ == "Ship":
			g = '▲'
		case x.typ == "Planet" || x.typ == "Star":
			g = '●'
		case x.typ != "":
			g = '◆'
		}
		at := th.attr(iffColor(th, x.iff))
		if x.iff == "" {
			at = th.text()
		}
		if x.cid == sel {
			at = th.attr(th.Token("color.accent"))
			at.Bold = true
		}
		if foc && ws.cur == i {
			at = focused(at)
		}
		d.c.put(r.X+gx, r.Y+gy, g, at)
	}
	d.c.put(r.X+int(cx)/2, r.Y+int(cy)/4, '✛', th.text())
	if info {
		line := "no contacts"
		if len(bl) > 0 {
			x := bl[ws.cur]
			line = fmt.Sprintf("%s %s %s", x.cid, x.name, str(x.rel))
		}
		d.c.text(r.X, r.Y+dh, line, th.text(), r.W)
	}
}

// ---- viewscreen ----

func hash(i int) uint32 {
	x := uint32(i)*2654435761 + 0x9e3779b9
	x ^= x >> 15
	x *= 2246822519
	x ^= x >> 13
	return x
}

func (d *drawer) viewscreen(b *pack.BoundNode, r Rect) {
	th := d.env.Theme
	seed := 0
	for _, c := range str(prop(b, "scene")) {
		seed = seed*31 + int(c)
	}
	bg := th.blank()
	bg.BG = layout.Color{Kind: layout.RGB, R: 8, G: 10, B: 24}
	d.c.fill(r, ' ', bg)
	// stars: fixed by the scene id, shifted by the heading so a turn moves them
	hd := asMap(prop(b, "heading"))
	shift := int(numOr(hd["az"], 0) / 3)
	for i := 0; i < r.W*r.H/6; i++ {
		h := hash(seed + i)
		x := int(h%uint32(r.W*4)) - shift
		x = ((x % (r.W * 4)) + r.W*4) % (r.W * 4)
		if x >= r.W {
			continue
		}
		y := int((h >> 12) % uint32(r.H))
		ch, col := '·', uint8(120)
		if h>>24&7 == 0 {
			ch, col = '*', 200
		}
		a := bg
		a.FG = layout.Color{Kind: layout.RGB, R: col, G: col, B: col}
		d.c.put(r.X+x, r.Y+y, ch, a)
	}
	fov := 60.0
	for _, x := range d.blips(b) {
		az := math.Mod(x.az+540, 360) - 180 // -180..180, 0 = dead ahead
		mk := numOr(x.rel["mark"], 0)
		a := bg
		a.FG = iffColor(th, x.iff)
		if x.iff == "" {
			a.FG = th.Token("color.text")
		}
		if math.Abs(az) > fov/2 {
			// off screen: an arrow on the edge it is on
			if x.typ == "Ship" {
				if az < 0 {
					d.c.text(r.X, r.Y+r.H/2, "◀ "+x.name, a, r.W)
				} else {
					s := x.name + " ▶"
					d.c.text(r.X+r.W-runeLen(s), r.Y+r.H/2, s, a, r.W)
				}
			}
			continue
		}
		px := r.X + int(float64(r.W)*(0.5+az/fov))
		py := r.Y + int(float64(r.H)*(0.5-mk/(fov*0.6)))
		if py < r.Y || py >= r.Y+r.H {
			continue
		}
		switch x.typ {
		case "Planet", "Star":
			size := int(math.Max(1, math.Min(float64(r.H)/2, 8/math.Max(1, math.Log10(1+x.d)-5))))
			for dy := -size; dy <= size; dy++ {
				for dx := -size * 2; dx <= size*2; dx++ {
					if float64(dx*dx)/4+float64(dy*dy) <= float64(size*size) {
						d.c.put(px+dx, py+dy, '█', a)
					}
				}
			}
		case "Ship":
			d.c.put(px, py, '▲', a)
			d.c.text(px+2, py, x.name, a, r.X+r.W-px-2)
		default:
			d.c.put(px, py, '·', a)
		}
	}
	for _, e := range d.env.Effects {
		age := d.env.Now.Sub(e.At)
		if age < 0 || age > 2e9 {
			continue
		}
		ea := bg
		switch e.Type {
		case "fire", "fire_phaser", "phaser":
			ea.FG = th.Token("color.alert")
			for i := 0; i < r.H/2; i++ {
				d.c.put(r.X+r.W/2+i*2-r.H/2, r.Y+r.H-1-i, '╱', ea)
			}
		case "hit", "hit_shield":
			ea.FG = th.Token("color.warn")
			for x := 0; x < r.W; x++ {
				d.c.put(r.X+x, r.Y, '▀', ea)
				d.c.put(r.X+x, r.Y+r.H-1, '▄', ea)
			}
		case "destroyed":
			ea.FG = th.Token("color.warn")
			d.c.text(r.X+r.W/2-3, r.Y+r.H/2, "✸ ✸ ✸", ea, 6)
		}
	}
}

// ---- schematic ----

func (d *drawer) schematic(b *pack.BoundNode, r Rect, foc bool) {
	th := d.env.Theme
	id := str(prop(b, "sprite"))
	var zones []string
	h := r.H
	if d.env.Assets != nil {
		zones = d.env.Assets.Zones(id)
	}
	if len(zones) > 0 {
		h--
	}
	var blk *Block
	if d.env.Assets != nil && h > 0 {
		blk = d.env.Assets.Sprite(id, r.W, h)
	}
	if blk == nil {
		d.c.text(r.X, r.Y, "[no schematic "+id+"]", th.dim(), r.W)
		return
	}
	ox, oy := r.X+(r.W-blk.W)/2, r.Y+(h-blk.H)/2
	for y := 0; y < blk.H; y++ {
		for x := 0; x < blk.W; x++ {
			c := blk.Cells[y*blk.W+x]
			if c.R == ' ' && c.A.BG.Kind == layout.Default && c.A.FG.Kind == layout.Default {
				continue
			}
			if c.A.BG.Kind == layout.Default && th.PaintBG {
				c.A.BG = th.Token("color.bg")
			}
			d.c.put(ox+x, oy+y, c.R, c.A)
		}
	}
	if len(zones) > 0 {
		ws := d.st.ws(b.Node.Path)
		ws.zone = max(0, min(ws.zone, len(zones)-1))
		a := th.dim()
		if foc {
			a = focused(th.accent())
		}
		d.c.text(r.X, r.Y+h, "zone: "+zones[ws.zone], a, r.W)
	}
}

func (d *drawer) zoneKeys(b *pack.BoundNode) func(Key) ([]Event, bool) {
	return func(k Key) ([]Event, bool) {
		var zones []string
		if d.env.Assets != nil {
			zones = d.env.Assets.Zones(str(prop(b, "sprite")))
		}
		ws := d.st.ws(b.Node.Path)
		ws.zone = max(0, min(ws.zone, len(zones)-1))
		switch k {
		case "left":
			if ws.zone > 0 {
				ws.zone--
				return nil, true
			}
		case "right":
			if ws.zone < len(zones)-1 {
				ws.zone++
				return nil, true
			}
		case "enter", "space":
			if len(zones) > 0 {
				return []Event{{Node: b.Node, Name: "click", Value: zones[ws.zone]}}, true
			}
			return nil, true
		}
		return nil, false
	}
}

// ---- widgets of the later edition (11.1): drawn simply ----

func (d *drawer) portrait(b *pack.BoundNode, r Rect) {
	th := d.env.Theme
	face := []string{"  ▄▄▄▄▄  ", " █ ◉ ◉ █ ", " █  ▾  █ ", " █ ╰─╯ █ ", "  ▀▀▀▀▀  "}
	for i, l := range face {
		if i < r.H-1 {
			d.c.text(r.X+max((r.W-9)/2, 0), r.Y+i, l, th.accent(), r.W)
		}
	}
	d.c.text(r.X, r.Y+min(len(face), r.H-1), str(prop(b, "name")), th.text(), r.W)
}

func (d *drawer) sectorMap(b *pack.BoundNode, r Rect) {
	th := d.env.Theme
	m := asMap(prop(b, "map"))
	bodies := asMap(m["bodies"])
	bd := newBraille(r.W, r.H)
	var ext float64 = 1
	pos := func(v any) (float64, float64) {
		l := asList(v)
		if len(l) < 2 {
			return 0, 0
		}
		return numOr(l[0], 0), numOr(l[1], 0)
	}
	for _, k := range sortedKeys(bodies) {
		x, y := pos(asMap(bodies[k])["pos"])
		ext = math.Max(ext, math.Max(math.Abs(x), math.Abs(y)))
	}
	for _, k := range sortedKeys(bodies) {
		x, y := pos(asMap(bodies[k])["pos"])
		px := int(float64(r.W)*(1+x/ext)) / 1
		py := int(float64(r.H*2) * (1 - y/ext))
		bd.dot(px, py, th.Token("color.accent2"))
		d.c.text(r.X+px/2+1, r.Y+py/4, str(asMap(bodies[k])["name"]), th.text(), r.W)
	}
	bd.blit(d.c, r.X, r.Y, th.blank())
}

func (d *drawer) padDiagram(b *pack.BoundNode, r Rect, foc bool) {
	th := d.env.Theme
	pads := asMap(prop(b, "pads"))
	keys := sortedKeys(pads)
	ws := d.st.ws(b.Node.Path)
	ws.cur = max(0, min(ws.cur, len(keys)-1))
	sel := str(prop(b, "selected"))
	bh := 4
	prog := numOr(prop(b, "progress"), -1)
	hasBar := prog > 0 && r.H > bh+1
	rows := r.H
	if hasBar {
		rows--
	}
	cols := max(1, min(len(keys), r.W/13))
	cw := r.W / cols
	for i, k := range keys {
		p := asMap(pads[k])
		x := r.X + (i%cols)*cw
		y := r.Y + (i/cols)*bh
		if y+bh > r.Y+rows {
			break
		}
		a := th.text()
		switch str(p["state"]) {
		case "occupied":
			a = th.attr(th.Token("color.ok"))
		case "blocked", "fault":
			a = th.attr(th.Token("color.alert"))
		case "empty":
			a = th.dim()
		}
		if k == sel {
			a = th.accent()
			a.Bold = true
		}
		if foc && ws.cur == i {
			a = focused(a)
		}
		w := min(cw-1, 12)
		d.c.text(x, y, "┌"+strings.Repeat("─", w-2)+"┐", a, w)
		label := str(p["label"])
		if label == "" {
			label = "Pad " + k
		}
		d.c.text(x, y+1, "│"+fmt.Sprintf("%-*s", w-2, clipN(label, w-2))+"│", a, w)
		occ := asMap(p["occupants"])
		first := ""
		if ok := sortedKeys(occ); len(ok) > 0 {
			first = str(asMap(occ[ok[0]])["name"])
			if len(ok) > 1 {
				first = fmt.Sprintf("%s +%d", clipN(first, w-6), len(ok)-1)
			}
		}
		d.c.text(x, y+2, "│"+fmt.Sprintf("%-*s", w-2, clipN(first, w-2))+"│", a, w)
		d.c.text(x, y+3, "└"+strings.Repeat("─", w-2)+"┘", a, w)
	}
	if hasBar {
		d.c.bar(r.X, r.Y+r.H-1, r.W, prog/100, th.accent(), th.dim())
	}
}

func clipN(s string, n int) string {
	r := []rune(clean(s))
	if len(r) > n {
		r = r[:n]
	}
	return string(r)
}
