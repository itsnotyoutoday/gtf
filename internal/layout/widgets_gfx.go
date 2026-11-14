// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package layout

import (
	"math"
	"strconv"
	"strings"
)

// Truecolor widgets: {radar}, {scope}, {ansi} and the bar gradient. They paint
// individual cells with their own colours, which plain {color} spans cannot.

func scaleRGB(r, g, b uint8, f float64) Color {
	f = math.Max(0, math.Min(1, f))
	return Color{Kind: RGB, R: uint8(float64(r) * f), G: uint8(float64(g) * f), B: uint8(float64(b) * f)}
}

func lerpColor(a, b Color, t float64) Color {
	mix := func(x, y uint8) uint8 { return uint8(float64(x) + (float64(y)-float64(x))*t) }
	return Color{Kind: RGB, R: mix(a.R, b.R), G: mix(a.G, b.G), B: mix(a.B, b.B)}
}

// gradientColors parses "#from,#to" (both must be RGB colours).
func gradientColors(s string) (a, b Color, ok bool) {
	p := strings.Split(s, ",")
	if len(p) != 2 {
		return
	}
	a, err1 := ParseColor(p[0])
	b, err2 := ParseColor(p[1])
	if err1 != nil || err2 != nil || a.Kind != RGB || b.Kind != RGB {
		return Color{}, Color{}, false
	}
	return a, b, true
}

// braille dot bits for a dot at (x 0..1, y 0..3) in a cell.
var brailleBit = [2][4]uint8{{0x01, 0x02, 0x04, 0x40}, {0x08, 0x10, 0x20, 0x80}}

type dotCanvas struct {
	w, h int // cells
	bits []uint8
	col  []Color
	prio []int8
}

func newDotCanvas(w, h int) *dotCanvas {
	n := w * h
	return &dotCanvas{w: w, h: h, bits: make([]uint8, n), col: make([]Color, n), prio: make([]int8, n)}
}

// set lights the dot (px,py) and colours its cell if prio is at least the cell's.
func (d *dotCanvas) set(px, py int, c Color, prio int8) {
	cx, cy := px>>1, py>>2
	if px < 0 || py < 0 || cx >= d.w || cy >= d.h {
		return
	}
	i := cy*d.w + cx
	d.bits[i] |= brailleBit[px&1][py&3]
	if prio >= d.prio[i] {
		d.prio[i] = prio
		d.col[i] = c
	}
}

func (c *Ctx) emitCanvas(d *dotCanvas, over map[int]Cell, bg Color) {
	for y := 0; y < d.h; y++ {
		for x := 0; x < d.w; x++ {
			i := y*d.w + x
			if oc, ok := over[i]; ok {
				oc.A.BG = bg
				c.emitWith(string(oc.R), oc.A)
				continue
			}
			r := ' '
			if d.bits[i] != 0 {
				r = rune(0x2800) + rune(d.bits[i])
			}
			c.emitWith(string(r), Attr{FG: d.col[i], BG: bg})
		}
		if y < d.h-1 {
			c.emit("\n")
		}
	}
}

type radarContact struct {
	id   string
	brg  float64
	rng  float64
	kind string
}

// parseContacts reads "id:bearing:range:type;id:bearing:range:type;...".
func parseContacts(s string) []radarContact {
	var out []radarContact
	for _, rec := range strings.Split(s, ";") {
		f := strings.Split(strings.TrimSpace(rec), ":")
		if len(f) < 3 {
			continue
		}
		b, err1 := strconv.ParseFloat(f[1], 64)
		r, err2 := strconv.ParseFloat(f[2], 64)
		if err1 != nil || err2 != nil {
			continue
		}
		k := "U"
		if len(f) > 3 {
			k = strings.ToUpper(f[3])
		}
		out = append(out, radarContact{f[0], b, r, k})
	}
	return out
}

func contactRGB(kind string) (uint8, uint8, uint8) {
	switch kind {
	case "H":
		return 255, 64, 64
	case "F":
		return 77, 166, 255
	case "N":
		return 255, 210, 77
	}
	return 235, 235, 235
}

func normDeg(a float64) float64 {
	a = math.Mod(a, 360)
	if a < 0 {
		a += 360
	}
	return a
}

// radar draws a hi-res Braille radar dish:
//
//	{radar contacts=REF sweep=REF width=25 height=13 trail=100 rings=3 bg=#001208}
//
// contacts is a variable holding "id:bearing:range:type;..." (bearing in degrees,
// 0 = up; range 0..100; type H hostile, F friend, N neutral, U unknown).
func (c *Ctx) radar(kw map[string]string) {
	w, h := intOpt(kw, "width", 25), intOpt(kw, "height", 13)
	if w < 5 || h < 3 {
		return
	}
	trail := float64(intOpt(kw, "trail", 100))
	rings := intOpt(kw, "rings", 3)
	sweep := 0.0
	if r := kw["sweep"]; r != "" {
		sweep, _ = c.num(r)
	}
	var contacts []radarContact
	if r := kw["contacts"]; r != "" {
		v, _ := c.lookup(r)
		contacts = parseContacts(toString(v))
	}
	bg, _ := ParseColor(kw["bg"])
	d := newDotCanvas(w, h)
	pw, ph := float64(w*2), float64(h*4)
	cx, cy := (pw-1)/2, (ph-1)/2
	R := math.Min(cx, cy)
	for py := 0; py < h*4; py++ {
		for px := 0; px < w*2; px++ {
			dx, dy := float64(px)-cx, float64(py)-cy
			dist := math.Hypot(dx, dy)
			if dist > R+0.5 {
				continue
			}
			theta := normDeg(math.Atan2(dx, -dy) * 180 / math.Pi)
			delta := normDeg(sweep - theta)
			if delta < trail {
				in := math.Pow(1-delta/trail, 1.6)
				if in > 0.5 || (px+py)%2 == 0 {
					if delta < 2 {
						d.set(px, py, Color{Kind: RGB, R: 190, G: 255, B: 210}, 3)
					} else if in > 0.05 {
						d.set(px, py, scaleRGB(20, 255, 110, 0.15+0.85*in), 1)
					}
				}
			}
			for k := 1; k <= rings; k++ {
				rr := R * float64(k) / float64(rings)
				if math.Abs(dist-rr) < 0.55 {
					if k == rings {
						d.set(px, py, Color{Kind: RGB, G: 190, B: 90}, 2)
					} else if (px+py)%2 == 0 {
						d.set(px, py, Color{Kind: RGB, G: 90, B: 55}, 2)
					}
				}
			}
			if (math.Abs(dx) < 0.6 || math.Abs(dy) < 0.6) && (px+py)%3 == 0 {
				d.set(px, py, Color{Kind: RGB, G: 70, B: 45}, 2)
			}
			if dist < 1.2 {
				d.set(px, py, Color{Kind: RGB, R: 120, G: 255, B: 170}, 4)
			}
		}
	}
	over := map[int]Cell{}
	for _, ct := range contacts {
		a := ct.brg * math.Pi / 180
		rr := math.Max(0, math.Min(100, ct.rng)) / 100 * R * 0.94
		x, y := cx+rr*math.Sin(a), cy-rr*math.Cos(a)
		cxc, cyc := int(x)>>1, int(y)>>2
		if cxc < 0 || cyc < 0 || cxc >= w || cyc >= h {
			continue
		}
		ping := 1 - normDeg(sweep-normDeg(ct.brg))/360 // brightest just after the sweep passes
		rC, gC, bC := contactRGB(ct.kind)
		col := scaleRGB(rC, gC, bC, 0.35+0.65*ping)
		over[cyc*w+cxc] = Cell{R: '●', A: Attr{FG: col, Bold: ping > 0.85}}
		if cxc+1 < w {
			if _, taken := over[cyc*w+cxc+1]; !taken && ct.id != "" {
				lc := []rune(ct.id)[0]
				over[cyc*w+cxc+1] = Cell{R: lc, A: Attr{FG: scaleRGB(rC, gC, bC, 0.55+0.45*ping)}}
			}
		}
	}
	c.emitCanvas(d, over, bg)
}

// scope draws a multi-row line graph of a variable's recent history in Braille:
//
//	{scope REF width=30 height=3 color=#33ff66 fill=on}
func (c *Ctx) scopeWidget(pos []string, kw map[string]string) {
	if len(pos) == 0 || c.Hist == nil {
		return
	}
	c.lookup(pos[0])
	hist := c.Hist(pos[0])
	w, h := intOpt(kw, "width", 30), intOpt(kw, "height", 3)
	if w < 1 || h < 1 {
		return
	}
	n := w * 2
	if len(hist) > n {
		hist = hist[len(hist)-n:]
	}
	fg, err := ParseColor(kw["color"])
	if err != nil || fg.Kind != RGB {
		fg = Color{Kind: RGB, R: 51, G: 255, B: 102}
	}
	d := newDotCanvas(w, h)
	if len(hist) == 0 {
		c.emitCanvas(d, nil, Color{})
		return
	}
	lo, hi := hist[0], hist[0]
	for _, v := range hist {
		lo, hi = math.Min(lo, v), math.Max(hi, v)
	}
	if s, ok := kw["min"]; ok {
		lo, _ = strconv.ParseFloat(s, 64)
	}
	if s, ok := kw["max"]; ok {
		hi, _ = strconv.ParseFloat(s, 64)
	}
	if hi <= lo {
		hi = lo + 1
	}
	dots := h * 4
	yOf := func(v float64) int {
		t := (v - lo) / (hi - lo)
		return dots - 1 - int(math.Round(math.Max(0, math.Min(1, t))*float64(dots-1)))
	}
	off := n - len(hist) // right-align: the newest sample is the rightmost column
	prev := -1
	for i, v := range hist {
		x, y := off+i, yOf(v)
		y0, y1 := y, y
		if prev >= 0 {
			y0, y1 = min(prev, y), max(prev, y)
		}
		for yy := y0; yy <= y1; yy++ {
			d.set(x, yy, fg, 2)
		}
		if kw["fill"] == "on" {
			for yy := y + 1; yy < dots; yy++ {
				if (x+yy)%2 == 0 {
					d.set(x, yy, scaleRGB(fg.R, fg.G, fg.B, 0.35), 1)
				}
			}
		}
		prev = y
	}
	c.emitCanvas(d, nil, Color{})
}

// ansi renders a variable that holds text with SGR escape sequences (ESC [ ... m, or
// the two characters \e instead of ESC), so a script can paint arbitrary cells:
//
//	{ansi var:row3}
func (c *Ctx) ansi(ref string) {
	v, _ := c.lookup(ref)
	s := strings.ReplaceAll(toString(v), `\e`, "\x1b")
	var a Attr
	rs := []rune(s)
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		if r == 0x1b && i+1 < len(rs) && rs[i+1] == '[' {
			j := i + 2
			for j < len(rs) && (rs[j] == ';' || rs[j] == ':' || (rs[j] >= '0' && rs[j] <= '9')) {
				j++
			}
			if j < len(rs) && rs[j] == 'm' {
				a = applySGR(a, string(rs[i+2:j]))
			}
			if j < len(rs) {
				i = j
			} else {
				i = j - 1
			}
			continue
		}
		if r == '\n' {
			c.emit("\n")
			continue
		}
		c.emitWith(string(r), a)
	}
}

func applySGR(a Attr, params string) Attr {
	if params == "" {
		return Attr{}
	}
	p := strings.FieldsFunc(params, func(r rune) bool { return r == ';' || r == ':' })
	n := make([]int, len(p))
	for i, s := range p {
		n[i], _ = strconv.Atoi(s)
	}
	for i := 0; i < len(n); i++ {
		switch k := n[i]; {
		case k == 0:
			a = Attr{}
		case k == 1:
			a.Bold = true
		case k == 2:
			a.Dim = true
		case k == 3:
			a.Italic = true
		case k == 4:
			a.Underline = true
		case k == 7:
			a.Reverse = true
		case k == 22:
			a.Bold, a.Dim = false, false
		case k == 23:
			a.Italic = false
		case k == 24:
			a.Underline = false
		case k == 27:
			a.Reverse = false
		case k >= 30 && k <= 37:
			a.FG = Color{Kind: ANSI, N: uint8(k - 30)}
		case k >= 90 && k <= 97:
			a.FG = Color{Kind: ANSI, N: uint8(k - 90 + 8)}
		case k >= 40 && k <= 47:
			a.BG = Color{Kind: ANSI, N: uint8(k - 40)}
		case k >= 100 && k <= 107:
			a.BG = Color{Kind: ANSI, N: uint8(k - 100 + 8)}
		case k == 39:
			a.FG = Color{}
		case k == 49:
			a.BG = Color{}
		case k == 38 || k == 48:
			col, used := Color{}, 0
			if i+2 < len(n) && n[i+1] == 5 {
				col, used = Color{Kind: Indexed, N: uint8(n[i+2])}, 2
			} else if i+4 < len(n) && n[i+1] == 2 {
				col, used = Color{Kind: RGB, R: uint8(n[i+2]), G: uint8(n[i+3]), B: uint8(n[i+4])}, 4
			}
			if used > 0 {
				if k == 38 {
					a.FG = col
				} else {
					a.BG = col
				}
				i += used
			}
		}
	}
	return a
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
