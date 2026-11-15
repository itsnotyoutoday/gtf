// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package cmcpui

import (
	"fmt"
	"math"
	"strings"

	"github.com/itsnotyoutoday/cmcp/pack"
	"github.com/itsnotyoutoday/gtf/internal/layout"
)

func rgbC(r, g, b uint8) layout.Color { return layout.Color{Kind: layout.RGB, R: r, G: g, B: b} }

// kindOf maps a contact's allegiance to the radar's letters.
func kindOf(iff string) string {
	switch iff {
	case "friend", "friendly":
		return "F"
	case "hostile", "enemy":
		return "H"
	case "neutral":
		return "N"
	}
	return "U"
}

// radarPalette is the theme's dish colouring.
func (d *drawer) radarPalette() layout.RadarPalette {
	th := d.env.Theme
	if th.Style == "" {
		return layout.RadarPalette{}
	}
	a, b := th.RGB("color.accent"), th.RGB("color.accent2")
	dim := func(c layout.Color, f float64) layout.Color {
		return rgbC(uint8(float64(c.R)*f), uint8(float64(c.G)*f), uint8(float64(c.B)*f))
	}
	return layout.RadarPalette{Sweep: b, Head: layout.Lerp(b, rgbC(255, 255, 255), 0.7), Ring: dim(a, 0.45), Outer: a,
		Grid: dim(a, 0.3), Centre: layout.Lerp(a, rgbC(255, 255, 255), 0.6), Set: true}
}

// radar draws the sensor dish with the /lcars renderer (layout.RenderRadar): rings, a sweep with
// a fading trail and contacts that glow as the sweep passes; the selected one and the keyboard
// cursor are emphasised. A line under it names the cursor's contact.
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
	dw := min(r.W, dh*2) // the dish is round when it is twice as many cells wide as tall
	ox := r.X + (r.W-dw)/2
	dmax := numOr(prop(b, "range_m"), 0)
	for _, x := range bl {
		dmax = math.Max(dmax, x.d)
	}
	sel := str(prop(b, "selected"))
	var cs []layout.RadarContact
	for i, x := range bl {
		rf := 0.0
		if dmax > 0 {
			rf = math.Log10(1+x.d) / math.Log10(1+dmax)
		}
		mark := '●'
		switch x.typ {
		case "Ship":
			mark = '▲'
		case "":
			mark = '◆'
		}
		cs = append(cs, layout.RadarContact{ID: "", Bearing: x.az, Range: rf * 100, Kind: kindOf(x.iff), Mark: mark,
			Bold: x.cid == sel || (foc && ws.cur == i)})
	}
	blk := layout.RenderRadar(layout.RadarOpts{W: dw, H: dh, Sweep: math.Mod(float64(d.env.Now.UnixMilli())/6000, 1) * 360,
		Contacts: cs, BG: th.blank().BG, Palette: d.radarPalette()})
	d.blit(blk, ox, r.Y)
	if info {
		line := "no contacts"
		if len(bl) > 0 {
			x := bl[ws.cur]
			line = fmt.Sprintf("%s %s %s", x.cid, x.name, str(x.rel))
		}
		a := th.text()
		if foc {
			a = focused(th.accent())
		}
		d.c.text(r.X, r.Y+dh, line, a, r.W)
	}
}

// blit draws a cell block at x, y inside the current clip.
func (d *drawer) blit(b *layout.CellBlock, x, y int) {
	for j := 0; j < b.H; j++ {
		for i := 0; i < b.W; i++ {
			c := b.Cells[j*b.W+i]
			if c.R == ' ' && c.A.BG.Kind == layout.Default {
				continue
			}
			if c.A.BG.Kind == layout.Default {
				c.A.BG = d.env.Theme.blank().BG
			}
			d.c.put(x+i, y+j, c.R, c.A)
		}
	}
}

// planetPalette picks a planet's colours from its class and name (stable for a given body).
func planetPalette(class, name string) (lo, hi [3]float64) {
	h := hash01(len(name)*131 + int(name[0]))
	mix := func(a, b [3]float64, t float64) [3]float64 {
		return [3]float64{a[0] + (b[0]-a[0])*t, a[1] + (b[1]-a[1])*t, a[2] + (b[2]-a[2])*t}
	}
	switch class {
	case "M", "Earth":
		return mix([3]float64{20, 70, 140}, [3]float64{40, 110, 70}, h), [3]float64{140, 200, 255}
	case "J", "Gas", "gas":
		return [3]float64{150, 90, 40}, mix([3]float64{245, 200, 140}, [3]float64{230, 170, 110}, h)
	case "K", "Barren":
		return [3]float64{90, 60, 50}, [3]float64{200, 160, 130}
	}
	return mix([3]float64{60, 80, 110}, [3]float64{110, 70, 90}, h), [3]float64{190, 200, 230}
}

// viewscreen draws the forward view as a canvas picture in the /lcars manner: a graded sky with
// nebula light, stars that drift with the heading, lit and shaded planets, a frame with the
// heading read-out; then the scene's ship art, names and effects on top. Contacts are placed with
// pack.Project, like every client's viewscreen.
func (d *drawer) viewscreen(b *pack.BoundNode, r Rect) {
	th := d.env.Theme
	seed := 0
	for _, c := range str(prop(b, "scene")) {
		seed = seed*31 + int(c)
	}
	cv := layout.NewCanvas(r.W, r.H)
	pw, ph := cv.W, cv.H
	acc, warm := th.RGB("color.accent2"), th.RGB("color.accent")
	top, bot := rgbC(2, 3, 12), rgbC(8, 6, 26)
	cv.Gradient(0, 0, pw, ph, top, bot, true)
	hd := asMap(prop(b, "heading"))
	shift := numOr(hd["az"], 0) / 360 * float64(pw) * 3
	for y := 0; y < ph; y++ {
		for x := 0; x < pw; x++ {
			nx := (float64(x) + shift) / 28
			n1 := noise(nx, float64(y)/16, seed)
			n2 := noise(nx*2.1+5, float64(y)/9, seed+1)
			cv.Add(x, y, acc, math.Max(0, n1*n2*2-0.5)*0.12)
			cv.Add(x, y, warm, math.Max(0, n2-0.7)*0.08)
		}
	}
	for i := 0; i < pw*ph/40; i++ {
		sx, sy := int(math.Mod(hash01(seed+i*3)*float64(pw*3)-shift+float64(pw*9), float64(pw*3))), int(hash01(seed+i*3+1)*float64(ph))
		if sx >= pw {
			continue
		}
		br := math.Pow(hash01(seed+i*3+2), 3) // most stars are faint, a few bright
		cv.Add(sx, sy, rgbC(190, 205, 255), 0.12+0.7*br)
	}
	fov := 60.0
	scene := str(prop(b, "scene"))
	type shipAt struct {
		x, y int
		b    blip
	}
	var ships []shipAt
	for _, x := range d.blips(b) {
		rel := asMap(x.raw["rel"])
		if rel == nil {
			rel = asMap(x.raw["bearing"])
		}
		relAz := numOr(rel["az"], 0)
		if relAz > 180 {
			relAz -= 360
		}
		fx, fy, vis := pack.Project(relAz, numOr(rel["mark"], 0), fov, float64(ph)/float64(pw)/2)
		if !vis {
			continue
		}
		px, py := fx*float64(pw), fy*float64(ph)
		switch x.typ {
		case "Planet", "Star":
			rad := math.Max(3, math.Min(float64(ph)*0.45, 10/math.Max(0.7, math.Log10(1+x.d)-5.5)))
			lo, hi := planetPalette(str(x.raw["class"]), x.name)
			if x.typ == "Star" {
				lo, hi = [3]float64{255, 150, 40}, [3]float64{255, 245, 200}
				cv.Glow(px, py, rad*3, rgbC(255, 190, 90), 0.8)
			}
			for yy := int(py - rad - 1); yy <= int(py+rad+1); yy++ {
				for xx := int(px - rad - 1); xx <= int(px+rad+1); xx++ {
					dx, dy := (float64(xx)+0.5-px)/rad, (float64(yy)+0.5-py)/rad
					d2 := dx*dx + dy*dy
					cov := math.Max(0, math.Min(1, (1-math.Sqrt(d2))*rad+0.5))
					if cov <= 0 {
						continue
					}
					z := math.Sqrt(math.Max(0, 1-d2))
					lit := math.Max(0, -0.55*dx-0.6*dy+z*0.55)
					t := math.Min(1, lit*1.1)
					sh := 0.25 + 0.85*lit
					col := rgbC(clamp8((lo[0]+(hi[0]-lo[0])*t)*sh), clamp8((lo[1]+(hi[1]-lo[1])*t)*sh), clamp8((lo[2]+(hi[2]-lo[2])*t)*sh))
					cv.Blend(xx, yy, col, cov)
				}
			}
		case "Ship":
			cv.Glow(px, py, 5, rgbC(150, 190, 255), 0.35)
			ships = append(ships, shipAt{int(px), int(py), x})
		default:
			cv.Add(int(px), int(py), rgbC(210, 210, 230), 0.9)
		}
	}
	for _, e := range d.env.Effects {
		age := d.env.Now.Sub(e.At).Seconds()
		if age < 0 || age > 2 {
			continue
		}
		fade := 1 - age/2
		switch e.Type {
		case "fire", "fire_phaser", "phaser":
			cv.Line(float64(pw)/2, float64(ph-1), float64(pw)/2+float64(pw)/6, float64(ph)/2, 3, rgbC(255, 90, 60), fade)
			cv.Line(float64(pw)/2, float64(ph-1), float64(pw)/2+float64(pw)/6, float64(ph)/2, 1, rgbC(255, 240, 200), fade)
		case "hit", "hit_shield":
			cv.Rect(0, 0, pw, 2, rgbC(255, 190, 60), fade, true)
			cv.Rect(0, ph-2, pw, 2, rgbC(255, 190, 60), fade, true)
		case "destroyed":
			cv.Glow(float64(pw)/2, float64(ph)/2, float64(ph)/2, rgbC(255, 170, 60), fade)
		}
	}
	// the frame: a thin line in the theme's accent, with the title and the heading in its corners
	frameCol := th.RGB("color.accent")
	if th.Style == "" {
		frameCol = rgbC(90, 110, 140)
	}
	cv.Rect(0, 0, pw, ph, frameCol, 1, false)
	head := fmt.Sprintf("HEADING %s", str(prop(b, "heading")))
	ta := layout.Attr{FG: th.RGB("color.text"), Bold: true}
	cv.Text(1, 0, "FORWARD VIEWSCREEN", ta)
	cv.Text(max(r.W-runeLen(head)-1, 20), 0, head, ta)
	d.blit(cv.Block(layout.Color{}), r.X, r.Y)
	for _, s := range ships {
		x := s.b
		wc := max(int(pack.ApparentFraction(numOr(x.raw["size_m"], 0), x.d, fov)*float64(r.W)), 6)
		at := th.attr(iffColor(th, x.iff))
		if x.iff == "" {
			at = th.text()
		}
		sy := r.Y + s.y/2
		if blk := d.artFor(scene, x.raw, min(wc, r.W/2), max(3, min(wc/2, r.H/2))); blk != nil {
			for y := 0; y < blk.H; y++ {
				for xx := 0; xx < blk.W; xx++ {
					c := blk.Cells[y*blk.W+xx]
					if c.R == ' ' && c.A.BG.Kind == layout.Default {
						continue
					}
					cx, cy := r.X+s.x-blk.W/2+xx, sy-blk.H/2+y
					if c.A.BG.Kind == layout.Default && cx >= 0 && cy >= 0 && cx < d.f.Grid.W && cy < d.f.Grid.H {
						c.A.BG = d.f.Grid.At(cx, cy).A.BG
					}
					d.c.put(cx, cy, c.R, c.A)
				}
			}
		} else {
			d.c.put(r.X+s.x, sy, '▲', at)
		}
		d.c.text(r.X+s.x-runeLen(x.name)/2, sy+2, strings.ToUpper(x.name), at, r.X+r.W-(r.X+s.x-runeLen(x.name)/2))
	}
}

func clamp8(v float64) uint8 { return uint8(math.Max(0, math.Min(255, math.Round(v)))) }
