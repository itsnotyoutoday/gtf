// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package client

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/itsnotyoutoday/gtf/internal/layout"
	"github.com/itsnotyoutoday/gtfparser"
)

// /canvas: truecolor drawing surfaces for panels ({canvas NAME}).
//
//	/canvas new NAME W H [bg]          W x H cells (the surface is W x 2H pixels)
//	/canvas clear NAME [color]         /canvas del NAME      /canvas list
//	/canvas px NAME x y color
//	/canvas line NAME x1 y1 x2 y2 color [t=N]
//	/canvas rect NAME x y w h color    outline        /canvas fill NAME x y w h color
//	/canvas rrect NAME x y w h r color [corners=tl,tr,bl,br]
//	/canvas circle NAME cx cy r color [t=N]    /canvas disc NAME cx cy r color
//	/canvas arc NAME cx cy r1 r2 a1 a2 color   (degrees, 0 = up, clockwise)
//	/canvas ellipse NAME cx cy rx ry color
//	/canvas glow NAME cx cy r color            soft additive light
//	/canvas poly NAME color x1 y1 x2 y2 x3 y3 ...     filled polygon
//	/canvas stars NAME x y w h count seed [t=PHASE]   a starfield
//	/canvas grad NAME x y w h color1 color2 [v]
//	/canvas text NAME cellx celly color text...
//	/canvas sprite NAME FILE x y [w h] [zones=id=v,..] [glows=id=v,..]   (experimental) rasterise a vector sprite file
//
// Any drawing command accepts a=ALPHA (0..1). Colors are #rrggbb, names, or 256:N.
func (c *Client) cmdCanvas(in *gtfparser.Interp, args string) gtfparser.Value {
	w := splitWords(args)
	fail := func(f string, a ...any) gtfparser.Value {
		in.Warn("canvas: " + fmt.Sprintf(f, a...))
		return gtfparser.Int(0)
	}
	if len(w) == 0 {
		return fail("usage: /canvas new|clear|del|px|line|rect|fill|rrect|circle|disc|arc|glow|grad|text|list NAME ...")
	}
	op := strings.ToLower(w[0])
	if op == "list" {
		names := make([]string, 0, len(c.panels.canvases))
		for n := range c.panels.canvases {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			cw, ch := c.panels.canvases[n].Cells()
			in.Host.Echo(fmt.Sprintf("%% canvas %s: %dx%d cells (%dx%d pixels)", n, cw, ch, cw, ch*2))
		}
		return gtfparser.Int(1)
	}
	if len(w) < 2 {
		return fail("/canvas %s needs a canvas name", op)
	}
	name := w[1]
	rest := w[2:]
	opts := map[string]string{}
	var pos []string
	for _, a := range rest {
		if k := strings.IndexByte(a, '='); k > 0 && k < 8 && !strings.ContainsAny(a[:k], "#:") && (op != "text" || a == "bold=1") {
			opts[a[:k]] = a[k+1:]
		} else {
			pos = append(pos, a)
		}
	}
	num := func(i int) (float64, bool) {
		if i >= len(pos) {
			return 0, false
		}
		v, err := strconv.ParseFloat(pos[i], 64)
		return v, err == nil
	}
	nums := func(n int) ([]float64, bool) {
		out := make([]float64, n)
		for i := range out {
			v, ok := num(i)
			if !ok {
				return nil, false
			}
			out[i] = v
		}
		return out, true
	}
	color := func(i int) (layout.Color, bool) {
		if i >= len(pos) {
			return layout.Color{}, false
		}
		col, err := layout.ParseColor(pos[i])
		return layout.RGBOf(col), err == nil
	}
	alpha := 1.0
	if s := opts["a"]; s != "" {
		if v, err := strconv.ParseFloat(s, 64); err == nil {
			alpha = v
		}
	}
	thick := 1.0
	if s := opts["t"]; s != "" {
		if v, err := strconv.ParseFloat(s, 64); err == nil {
			thick = v
		}
	}
	switch op {
	case "new":
		n, ok := nums(2)
		if !ok {
			return fail("usage: /canvas new NAME W H [bg]")
		}
		cv := layout.NewCanvas(int(n[0]), int(n[1]))
		if col, ok := color(2); ok {
			cv.Clear(&col)
		}
		c.panels.canvases[name] = cv
		c.canvasChanged(name)
		return gtfparser.Int(1)
	case "del":
		delete(c.panels.canvases, name)
		c.canvasChanged(name)
		return gtfparser.Int(1)
	}
	cv, ok := c.panels.canvases[name]
	if !ok {
		return fail("no such canvas: %s (use /canvas new %s W H)", name, name)
	}
	defer c.canvasChanged(name)
	switch op {
	case "clear":
		if col, ok := color(0); ok {
			cv.Clear(&col)
		} else {
			cv.Clear(nil)
		}
	case "px":
		n, ok1 := nums(2)
		col, ok2 := color(2)
		if !ok1 || !ok2 {
			return fail("usage: /canvas px NAME x y color")
		}
		cv.Blend(int(n[0]), int(n[1]), col, alpha)
	case "line":
		n, ok1 := nums(4)
		col, ok2 := color(4)
		if !ok1 || !ok2 {
			return fail("usage: /canvas line NAME x1 y1 x2 y2 color [t=N]")
		}
		cv.Line(n[0], n[1], n[2], n[3], thick, col, alpha)
	case "rect", "fill":
		n, ok1 := nums(4)
		col, ok2 := color(4)
		if !ok1 || !ok2 {
			return fail("usage: /canvas %s NAME x y w h color", op)
		}
		cv.Rect(int(n[0]), int(n[1]), int(n[2]), int(n[3]), col, alpha, op == "fill")
	case "rrect":
		n, ok1 := nums(5)
		col, ok2 := color(5)
		if !ok1 || !ok2 {
			return fail("usage: /canvas rrect NAME x y w h r color [corners=tl,tr,bl,br]")
		}
		cv.RoundRect(int(n[0]), int(n[1]), int(n[2]), int(n[3]), n[4], opts["corners"], col, alpha)
	case "circle", "disc":
		n, ok1 := nums(3)
		col, ok2 := color(3)
		if !ok1 || !ok2 {
			return fail("usage: /canvas %s NAME cx cy r color", op)
		}
		if op == "disc" {
			cv.Disc(n[0], n[1], n[2], col, alpha)
		} else {
			cv.Ring(n[0], n[1], n[2], thick, col, alpha)
		}
	case "arc":
		n, ok1 := nums(6)
		col, ok2 := color(6)
		if !ok1 || !ok2 {
			return fail("usage: /canvas arc NAME cx cy r1 r2 a1 a2 color")
		}
		cv.Arc(n[0], n[1], n[2], n[3], n[4], n[5], col, alpha)
	case "ellipse":
		n, ok1 := nums(4)
		col, ok2 := color(4)
		if !ok1 || !ok2 {
			return fail("usage: /canvas ellipse NAME cx cy rx ry color")
		}
		cv.Ellipse(n[0], n[1], n[2], n[3], col, alpha)
	case "glow":
		n, ok1 := nums(3)
		col, ok2 := color(3)
		if !ok1 || !ok2 {
			return fail("usage: /canvas glow NAME cx cy r color")
		}
		cv.Glow(n[0], n[1], n[2], col, alpha)
	case "poly":
		col, ok := color(0)
		if !ok || len(pos) < 7 {
			return fail("usage: /canvas poly NAME color x1 y1 x2 y2 x3 y3 ...")
		}
		var pts []float64
		for _, p := range pos[1:] {
			v, err := strconv.ParseFloat(p, 64)
			if err != nil {
				return fail("poly: bad coordinate %q", p)
			}
			pts = append(pts, v)
		}
		if len(pts)%2 != 0 {
			return fail("poly: odd number of coordinates")
		}
		cv.Poly(pts, col, alpha)
	case "stars":
		n, ok := nums(6)
		if !ok {
			return fail("usage: /canvas stars NAME x y w h count seed [t=PHASE]")
		}
		ph, _ := strconv.ParseFloat(opts["t"], 64)
		cv.Stars(int(n[0]), int(n[1]), int(n[2]), int(n[3]), int(n[4]), uint32(n[5]), ph)
	case "grad":
		n, ok1 := nums(4)
		c1, ok2 := color(4)
		c2, ok3 := color(5)
		if !ok1 || !ok2 || !ok3 {
			return fail("usage: /canvas grad NAME x y w h color1 color2 [v]")
		}
		cv.Gradient(int(n[0]), int(n[1]), int(n[2]), int(n[3]), c1, c2, len(pos) > 6 && pos[6] == "v")
	case "sprite": // experimental
		if err := c.canvasSprite(cv, pos, opts); err != nil {
			return fail("sprite: %v", err)
		}
	case "text":
		n, ok1 := nums(2)
		col, ok2 := color(2)
		if !ok1 || !ok2 || len(pos) < 4 {
			return fail("usage: /canvas text NAME cellx celly color text...")
		}
		cv.Text(int(n[0]), int(n[1]), strings.Join(pos[3:], " "), layout.Attr{FG: col, Bold: opts["bold"] == "1"})
	default:
		return fail("unknown operation %q", op)
	}
	return gtfparser.Int(1)
}

// canvasChanged tells the active layout that a canvas it may show has changed.
func (c *Client) canvasChanged(name string) {
	if l := c.panels.current(c); l != nil {
		l.Changed("canvas:" + name)
	}
	c.scheduleRender()
}
