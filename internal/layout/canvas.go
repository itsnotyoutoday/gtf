// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package layout

import (
	"math"
	"strings"
)

// Canvas is a truecolor pixel surface for scripts to draw on. Each terminal cell
// holds two vertically stacked pixels (the upper and lower half blocks), so a
// canvas of W x H cells is W x 2H pixels, and pixels are square on a normal font.
// An unset pixel shows the terminal background. Text can be placed on whole cells.
type Canvas struct {
	W, H int // size in pixels (H is twice the cell height)
	pix  []pixel
	text map[int]Cell // cell index -> text cell drawn over the pixels
}

type pixel struct {
	R, G, B uint8
	Set     bool
}

// NewCanvas makes a canvas cw cells wide and ch cells tall.
func NewCanvas(cw, ch int) *Canvas {
	cw, ch = max(cw, 1), max(ch, 1)
	return &Canvas{W: cw, H: ch * 2, pix: make([]pixel, cw*ch*2), text: map[int]Cell{}}
}

// Cells returns the canvas size in cells.
func (c *Canvas) Cells() (w, h int) { return c.W, c.H / 2 }

// Clear erases everything; with a colour it floods the canvas with it.
func (c *Canvas) Clear(col *Color) {
	for i := range c.pix {
		c.pix[i] = pixel{}
		if col != nil {
			c.pix[i] = pixel{col.R, col.G, col.B, true}
		}
	}
	c.text = map[int]Cell{}
}

// Blend paints one pixel with the given coverage/alpha (0..1) over what is there.
func (c *Canvas) Blend(x, y int, col Color, a float64) {
	if x < 0 || y < 0 || x >= c.W || y >= c.H || a <= 0 {
		return
	}
	p := &c.pix[y*c.W+x]
	if a >= 1 || !p.Set {
		if a >= 1 {
			*p = pixel{col.R, col.G, col.B, true}
			return
		}
		*p = pixel{uint8(float64(col.R) * a), uint8(float64(col.G) * a), uint8(float64(col.B) * a), true}
		return
	}
	mix := func(old, nw uint8) uint8 { return uint8(float64(old)*(1-a) + float64(nw)*a) }
	p.R, p.G, p.B = mix(p.R, col.R), mix(p.G, col.G), mix(p.B, col.B)
}

// Add paints additively (glows): the colour, scaled by a, is added to what is there.
func (c *Canvas) Add(x, y int, col Color, a float64) {
	if x < 0 || y < 0 || x >= c.W || y >= c.H || a <= 0 {
		return
	}
	p := &c.pix[y*c.W+x]
	add := func(old, nw uint8) uint8 { return uint8(math.Min(255, float64(old)+float64(nw)*a)) }
	p.R, p.G, p.B, p.Set = add(p.R, col.R), add(p.G, col.G), add(p.B, col.B), true
}

// Line draws a line from (x1,y1) to (x2,y2) with the given thickness in pixels.
func (c *Canvas) Line(x1, y1, x2, y2 float64, thick float64, col Color, a float64) {
	dx, dy := x2-x1, y2-y1
	n := int(math.Max(math.Abs(dx), math.Abs(dy)))
	if n == 0 {
		c.Disc(x1, y1, math.Max(thick/2, 0.5), col, a)
		return
	}
	for i := 0; i <= n; i++ {
		t := float64(i) / float64(n)
		x, y := x1+dx*t, y1+dy*t
		if thick <= 1 {
			c.Blend(int(math.Round(x)), int(math.Round(y)), col, a)
		} else {
			c.Disc(x, y, thick/2, col, a)
		}
	}
}

// Rect fills (or outlines) the rectangle x,y,w,h.
func (c *Canvas) Rect(x, y, w, h int, col Color, a float64, fill bool) {
	for yy := y; yy < y+h; yy++ {
		for xx := x; xx < x+w; xx++ {
			if fill || xx == x || yy == y || xx == x+w-1 || yy == y+h-1 {
				c.Blend(xx, yy, col, a)
			}
		}
	}
}

// RoundRect fills a rectangle with rounded corners of radius r. corners lists
// which corners are rounded ("tl", "tr", "bl", "br"; empty means all four).
func (c *Canvas) RoundRect(x, y, w, h int, r float64, corners string, col Color, a float64) {
	all := corners == ""
	has := func(k string) bool { return all || strings.Contains(corners, k) }
	for yy := y; yy < y+h; yy++ {
		for xx := x; xx < x+w; xx++ {
			cov := 1.0
			px, py := float64(xx)+0.5, float64(yy)+0.5
			var cx, cy float64
			inCorner := false
			switch {
			case px < float64(x)+r && py < float64(y)+r && has("tl"):
				cx, cy, inCorner = float64(x)+r, float64(y)+r, true
			case px > float64(x+w)-r && py < float64(y)+r && has("tr"):
				cx, cy, inCorner = float64(x+w)-r, float64(y)+r, true
			case px < float64(x)+r && py > float64(y+h)-r && has("bl"):
				cx, cy, inCorner = float64(x)+r, float64(y+h)-r, true
			case px > float64(x+w)-r && py > float64(y+h)-r && has("br"):
				cx, cy, inCorner = float64(x+w)-r, float64(y+h)-r, true
			}
			if inCorner {
				cov = clamp01(r - math.Hypot(px-cx, py-cy) + 0.5)
			}
			c.Blend(xx, yy, col, a*cov)
		}
	}
}

// Disc fills a circle with anti-aliased edges.
func (c *Canvas) Disc(cx, cy, r float64, col Color, a float64) { c.Arc(cx, cy, 0, r, 0, 360, col, a) }

// Ring draws a circle outline of the given thickness.
func (c *Canvas) Ring(cx, cy, r, thick float64, col Color, a float64) {
	c.Arc(cx, cy, r-thick/2, r+thick/2, 0, 360, col, a)
}

// Arc fills the annulus sector between radii r1 and r2 from angle a1 to a2 degrees
// (0 = up, clockwise), with anti-aliased radial edges. Quarter arcs make the
// rounded elbows of an LCARS frame; thin arcs make radar sweeps and dials.
func (c *Canvas) Arc(cx, cy, r1, r2, a1, a2 float64, col Color, alpha float64) {
	x0, x1 := int(math.Floor(cx-r2-1)), int(math.Ceil(cx+r2+1))
	y0, y1 := int(math.Floor(cy-r2-1)), int(math.Ceil(cy+r2+1))
	full := a2-a1 >= 360
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			dx, dy := float64(x)+0.5-cx, float64(y)+0.5-cy
			d := math.Hypot(dx, dy)
			cov := clamp01(r2-d+0.5) * clamp01(d-r1+0.5)
			if cov <= 0 {
				continue
			}
			if !full {
				th := normDeg(math.Atan2(dx, -dy) * 180 / math.Pi)
				span := normDeg(a2 - a1)
				if normDeg(th-a1) > span {
					continue
				}
			}
			c.Blend(x, y, col, alpha*cov)
		}
	}
}

// Glow adds a soft radial light of radius r centred on (cx, cy).
func (c *Canvas) Glow(cx, cy, r float64, col Color, a float64) {
	for y := int(cy - r - 1); y <= int(cy+r+1); y++ {
		for x := int(cx - r - 1); x <= int(cx+r+1); x++ {
			d := math.Hypot(float64(x)+0.5-cx, float64(y)+0.5-cy) / r
			if d < 1 {
				c.Add(x, y, col, a*(1-d)*(1-d))
			}
		}
	}
}

// Gradient fills a rectangle with a colour ramp, left to right ("h") or top to bottom ("v").
func (c *Canvas) Gradient(x, y, w, h int, from, to Color, vertical bool) {
	for yy := y; yy < y+h; yy++ {
		for xx := x; xx < x+w; xx++ {
			t := 0.0
			if vertical && h > 1 {
				t = float64(yy-y) / float64(h-1)
			} else if !vertical && w > 1 {
				t = float64(xx-x) / float64(w-1)
			}
			c.Blend(xx, yy, lerpColor(from, to, t), 1)
		}
	}
}

// Text writes a string on whole cells starting at cell (cx, cy).
func (c *Canvas) Text(cx, cy int, s string, a Attr) {
	cw, ch := c.Cells()
	for _, r := range s {
		if cx >= 0 && cy >= 0 && cx < cw && cy < ch {
			c.text[cy*cw+cx] = Cell{R: r, A: a}
		}
		cx++
	}
}

func clamp01(v float64) float64 { return math.Max(0, math.Min(1, v)) }

// render emits the canvas through the half-block trick: the upper pixel is the
// foreground of "▀" and the lower pixel its background.
func (cx *Ctx) renderCanvas(c *Canvas) {
	cw, ch := c.Cells()
	for y := 0; y < ch; y++ {
		for x := 0; x < cw; x++ {
			top, bot := c.pix[(2*y)*c.W+x], c.pix[(2*y+1)*c.W+x]
			if tc, ok := c.text[y*cw+x]; ok {
				a := tc.A
				switch {
				case bot.Set && !top.Set:
					a.BG = Color{Kind: RGB, R: bot.R, G: bot.G, B: bot.B}
				case top.Set && !bot.Set:
					a.BG = Color{Kind: RGB, R: top.R, G: top.G, B: top.B}
				case top.Set:
					a.BG = Color{Kind: RGB, R: top.R, G: top.G, B: top.B}
				}
				cx.emitWith(string(tc.R), a)
				continue
			}
			tcol := Color{Kind: RGB, R: top.R, G: top.G, B: top.B}
			bcol := Color{Kind: RGB, R: bot.R, G: bot.G, B: bot.B}
			switch {
			case !top.Set && !bot.Set:
				cx.emitWith(" ", Attr{})
			case top.Set && !bot.Set:
				cx.emitWith("▀", Attr{FG: tcol})
			case !top.Set && bot.Set:
				cx.emitWith("▄", Attr{FG: bcol})
			case top == bot:
				cx.emitWith("█", Attr{FG: tcol})
			default:
				cx.emitWith("▀", Attr{FG: tcol, BG: bcol})
			}
		}
		if y < ch-1 {
			cx.emit("\n")
		}
	}
}

// canvasWidget renders {canvas NAME}; the client resolves "canvas:NAME" to a *Canvas.
func (c *Ctx) canvasWidget(name string) {
	v, ok := c.lookup("canvas:" + name)
	if !ok {
		return
	}
	if cv, ok := v.(*Canvas); ok {
		c.renderCanvas(cv)
	}
}

// RGBOf converts any Color to RGB (using the standard xterm palette for indexed colours).
func RGBOf(col Color) Color {
	switch col.Kind {
	case RGB:
		return col
	case ANSI, Indexed:
		return xtermRGB(int(col.N))
	}
	return Color{Kind: RGB}
}

func xtermRGB(n int) Color {
	base := [16][3]uint8{{0, 0, 0}, {205, 0, 0}, {0, 205, 0}, {205, 205, 0}, {0, 0, 238}, {205, 0, 205}, {0, 205, 205}, {229, 229, 229},
		{127, 127, 127}, {255, 0, 0}, {0, 255, 0}, {255, 255, 0}, {92, 92, 255}, {255, 0, 255}, {0, 255, 255}, {255, 255, 255}}
	switch {
	case n < 16:
		b := base[n]
		return Color{Kind: RGB, R: b[0], G: b[1], B: b[2]}
	case n < 232:
		n -= 16
		lv := func(i int) uint8 {
			if i == 0 {
				return 0
			}
			return uint8(55 + 40*i)
		}
		return Color{Kind: RGB, R: lv(n / 36), G: lv(n / 6 % 6), B: lv(n % 6)}
	}
	g := uint8(8 + 10*(n-232))
	return Color{Kind: RGB, R: g, G: g, B: g}
}
