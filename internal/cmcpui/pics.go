// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package cmcpui

import (
	"fmt"
	"github.com/itsnotyoutoday/cmcp/go/cellglyph"
	"math"
	"strings"

	"github.com/itsnotyoutoday/cmcp/go/pack"
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
	if th.Style == "" || d.env.Alert != "red" {
		// the /lcars dish: green-teal rings and sweep (a red alert turns it red below)
		if th.Style != "" {
			return layout.RadarPalette{Sweep: rgbC(30, 240, 190), Head: rgbC(200, 255, 235), Ring: rgbC(0, 100, 85),
				Outer: rgbC(0, 200, 160), Grid: rgbC(0, 70, 60), Centre: rgbC(150, 255, 220), Set: true}
		}
		return layout.RadarPalette{}
	}
	a, b := rgbC(220, 50, 50), rgbC(255, 110, 80)
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
	var blk *layout.CellBlock
	if kind := d.radarKind(); kind != "dish" && cellglyph.Lib().HasRadar(kind) {
		blk = d.charRadar(kind, dw, dh, cs)
	} else {
		blk = layout.RenderRadar(layout.RadarOpts{W: dw, H: dh, Sweep: math.Mod(float64(d.env.Now.UnixMilli())/6000, 1) * 360,
			Contacts: cs, BG: th.blank().BG, Palette: d.radarPalette()})
	}
	d.blit(blk, ox, r.Y)
	if dw >= 20 && dmax > 0 {
		// range labels on the rings, along the right-hand radius: what each ring is worth
		cx, cy := ox+dw/2, r.Y+dh/2
		for k := 1; k <= 3; k++ {
			rng := math.Pow(10, float64(k)/3*math.Log10(1+dmax)) - 1
			lbl := fmtRange(rng)
			x := cx + int(float64(dw)/2*float64(k)/3) - runeLen(lbl)/2
			if k == 3 {
				x = cx + dw/2 - runeLen(lbl) - 1
			}
			d.c.text(x, cy, lbl, layout.Attr{FG: rgbC(120, 220, 200), BG: th.blank().BG}, runeLen(lbl))
		}
	}
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
			cv.Add(x, y, acc, math.Max(0, n1*n2*2-0.55)*0.07)
			cv.Add(x, y, warm, math.Max(0, n2-0.72)*0.05)
		}
	}
	// a distant sun low in the corner gives the sky depth; three star layers drift at different rates
	sunx, suny := float64(pw)*(0.15+0.7*hash01(seed+99)), float64(ph)*(0.15+0.5*hash01(seed+98))
	cv.Glow(sunx, suny, float64(pw)*0.45, rgbC(255, 200, 150), 0.10)
	cv.Glow(sunx, suny, float64(pw)*0.10, rgbC(255, 235, 210), 0.35)
	tw := float64(d.env.Now.UnixMilli()) / 1700
	for layer := 0; layer < 3; layer++ {
		rate := []float64{0.35, 1, 2.2}[layer]
		count := []int{pw * ph / 26, pw * ph / 55, pw * ph / 160}[layer]
		for i := 0; i < count; i++ {
			k := seed + layer*7919 + i*3
			sx := int(math.Mod(hash01(k)*float64(pw*3)-shift*rate+float64(pw*30), float64(pw*3)))
			if sx >= pw {
				continue
			}
			sy := int(hash01(k+1) * float64(ph))
			br := math.Pow(hash01(k+2), 2.2)*0.9 + 0.08
			br *= 0.85 + 0.15*math.Sin(tw+hash01(k+4)*6.28)
			tint := rgbC(200, 215, 255)
			switch int(hash01(k+5) * 4) {
			case 0:
				tint = rgbC(255, 220, 190)
			case 1:
				tint = rgbC(170, 200, 255)
			}
			cv.Add(sx, sy, tint, br*[]float64{0.5, 0.8, 1}[layer])
			if layer == 2 && br > 0.6 {
				cv.Add(sx+1, sy, tint, br*0.3)
				cv.Add(sx-1, sy, tint, br*0.3)
				cv.Add(sx, sy+1, tint, br*0.2)
				cv.Add(sx, sy-1, tint, br*0.2)
			}
		}
	}
	fov := 60.0
	scene := str(prop(b, "scene"))
	type shipAt struct {
		x, y int
		b    blip
	}
	var ships, labels []shipAt
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
			rad := math.Max(3, math.Min(float64(ph)*0.3, 10/math.Max(0.7, math.Log10(1+x.d)-5.5)))
			lo, hi := planetPalette(str(x.raw["class"]), x.name)
			if x.typ == "Star" {
				lo, hi = [3]float64{255, 150, 40}, [3]float64{255, 245, 200}
				cv.Glow(px, py, rad*3, rgbC(255, 190, 90), 0.8)
			}
			atm := rgbC(110, 170, 255)
			_ = atm
			if x.typ != "Star" {
				cv.Glow(px, py, rad*1.35, atm, 0.28) // atmosphere haze around the limb
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
					cr, cg, cb := (lo[0]+(hi[0]-lo[0])*t)*sh, (lo[1]+(hi[1]-lo[1])*t)*sh, (lo[2]+(hi[2]-lo[2])*t)*sh
					if x.typ != "Star" {
						// cloud bands, lit like the ground, and a blue rim of atmosphere at the limb
						cl := math.Max(0, noise(dx*3+float64(seed%7), dy*5, seed)*1.6-0.7) * (0.35 + 0.65*lit)
						cr, cg, cb = cr+(255-cr)*cl*0.8, cg+(255-cg)*cl*0.8, cb+(255-cb)*cl*0.8
						rim := math.Pow(1-z, 2.5) * 0.7 * (0.3 + 0.7*lit)
						cr, cg, cb = cr+float64(atm.R)*rim, cg+float64(atm.G)*rim, cb+float64(atm.B)*rim
					}
					cv.Blend(xx, yy, rgbC(clamp8(cr), clamp8(cg), clamp8(cb)), cov)
				}
			}
		case "Ship":
			if d.artFor(scene, x.raw, 6, 3) == nil {
				// no scene art for this class: a generic ship drawn with the canvas, sized by its apparent width
				wpx := math.Max(10, pack.ApparentFraction(numOr(x.raw["size_m"], 100), x.d, fov)*float64(pw))
				drawShip(cv, px, py, math.Min(wpx, float64(pw)/3), iffRGB(x.iff))
			} else {
				cv.Glow(px, py, 5, rgbC(150, 190, 255), 0.35)
				ships = append(ships, shipAt{int(px), int(py), x})
			}
			labels = append(labels, shipAt{int(px), int(py), x})
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
	for _, s := range labels {
		at := th.attr(iffColor(th, s.b.iff))
		if s.b.iff == "" {
			at = th.text()
		}
		sy := r.Y + s.y/2
		nm := strings.ToUpper(s.b.name)
		d.c.text(r.X+s.x-runeLen(nm)/2, min(sy+3, r.Y+r.H-1), nm, at, r.X+r.W-(r.X+s.x-runeLen(nm)/2))
	}
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
	}
}

func clamp8(v float64) uint8 { return uint8(math.Max(0, math.Min(255, math.Round(v)))) }

// R2, G2, B2 give a colour's channels as floats (for blending).
func colorF(c layout.Color) (float64, float64, float64) {
	return float64(c.R), float64(c.G), float64(c.B)
}

// drawShip draws a generic starship seen three-quarters on: a lit saucer, a secondary hull, two
// nacelles on pylons with glowing engines, tinted by allegiance; wpx is its width in pixels.
func drawShip(cv *layout.Canvas, px, py, wpx float64, tint layout.Color) {
	s := wpx / 20
	hull := layout.Lerp(tint, rgbC(210, 220, 235), 0.65)
	shade := layout.Lerp(tint, rgbC(40, 50, 80), 0.5)
	// nacelles behind
	for _, side := range []float64{-1, 1} {
		cv.Line(px+side*2*s, py+0.5*s, px+side*6.5*s, py+2*s, math.Max(1, 0.7*s), shade, 0.9)
		cv.Ellipse(px+side*7*s, py+2*s, 3*s, 0.9*s, layout.Lerp(tint, rgbC(60, 70, 110), 0.3), 1)
		cv.Glow(px+side*9.6*s, py+2*s, 2.4*s, rgbC(120, 190, 255), 0.7)
	}
	// secondary hull and neck
	cv.Ellipse(px, py+2.2*s, 2.6*s, 1.1*s, shade, 1)
	cv.Rect(int(px-0.6*s), int(py), int(math.Max(2, 1.2*s)), int(math.Max(1, 2*s)), shade, 1, true)
	// saucer, lit from above
	cv.Ellipse(px, py, 6*s, 2.3*s, hull, 1)
	cv.Ellipse(px, py-0.5*s, 4.2*s, 1.4*s, layout.Lerp(hull, rgbC(255, 255, 255), 0.4), 0.9)
	cv.Disc(px, py-1*s, math.Max(0.8, 0.7*s), rgbC(180, 230, 255), 0.9)
}

// fmtRange formats a distance in metres for a ring label: m, km, Mm, AU.
func fmtRange(m float64) string {
	switch {
	case m < 1000:
		return fmt.Sprintf("%.0fm", m)
	case m < 1e6:
		return fmt.Sprintf("%.0fkm", m/1000)
	case m < 1.4e10:
		return fmt.Sprintf("%.0fMm", m/1e6)
	}
	return fmt.Sprintf("%.1fAU", m/1.496e11)
}

// iffRGB is the colour of a contact by allegiance.
func iffRGB(iff string) layout.Color {
	switch iff {
	case "friend", "friendly":
		return rgbC(90, 230, 130)
	case "hostile", "enemy":
		return rgbC(255, 70, 60)
	case "neutral":
		return rgbC(90, 180, 255)
	}
	return rgbC(255, 210, 90)
}

// radarKind is the theme's radar style (cell.widgets.radar.kind: dish, hex, rings, wing).
func (d *drawer) radarKind() string {
	if k := jstr(jmap(jmap(cellDoc(d.env.Theme), "widgets"), "radar"), "kind"); k != "" {
		return k
	}
	return "dish"
}

// charRadar draws a radar from the glyph library's character sets (cellglyph, spec 19.8): a hexagon
// of ring characters for the klingon plot, concentric rings for the romulan one. Every colour is a
// role, so an alert recolours it like everything else.
func (d *drawer) charRadar(kind string, w, h int, cs []layout.RadarContact) *layout.CellBlock {
	lib := cellglyph.Lib().Radars[kind]
	th := d.env.Theme
	role := jstr(jmap(jmap(cellDoc(th), "widgets"), "radar"), "role")
	ring := th.role(orStrC(role, "x"))
	dim := layout.Lerp(ring, th.blank().BG, 0.55)
	blk := &layout.CellBlock{W: w, H: h, Cells: make([]layout.Cell, w*h)}
	for i := range blk.Cells {
		blk.Cells[i] = layout.Cell{R: ' ', A: layout.Attr{BG: th.blank().BG}}
	}
	put := func(x, y int, r rune, fg layout.Color, bold bool) {
		if x >= 0 && x < w && y >= 0 && y < h {
			blk.Cells[y*w+x] = layout.Cell{R: r, A: layout.Attr{FG: fg, BG: th.blank().BG, Bold: bold}}
		}
	}
	cx, cy := float64(w-1)/2, float64(h-1)/2
	rx, ry := cx, cy
	chars := []rune(lib.RingChars)
	axis := []rune(lib.Axis + "┼")[0]
	for x := 0; x < w; x++ {
		put(x, int(cy), '─', dim, false)
	}
	for y := 0; y < h; y++ {
		put(int(cx), y, '│', dim, false)
	}
	put(int(cx), int(cy), axis, ring, false)
	rings := max(lib.Rings, 2)
	for k := 1; k <= rings; k++ {
		f := float64(k) / float64(rings)
		for deg := 0; deg < 360; deg += 3 {
			a := float64(deg) * math.Pi / 180
			var px, py float64
			ch := chars[(k-1)%len(chars)]
			if lib.Shape == "hexagon" {
				// the hexagon's boundary at angle a: radius of a regular hexagon with a flat top
				sec := math.Mod(a+math.Pi/6, math.Pi/3) - math.Pi/6
				rr := math.Cos(math.Pi/6) / math.Cos(sec)
				px, py = math.Cos(a)*rr*f*rx, math.Sin(a)*rr*f*ry
				switch d := deg % 360; {
				case d >= 330 || d < 30 || (d >= 150 && d < 210):
					ch = '│'
				case (d >= 30 && d < 90) || (d >= 210 && d < 270):
					ch = '╲'
				default:
					ch = '╱'
				}
				if k < rings {
					ch = '·'
				}
			} else {
				px, py = math.Cos(a)*f*rx, math.Sin(a)*f*ry
			}
			col := dim
			if k == rings {
				col = ring
			}
			put(int(math.Round(cx+px)), int(math.Round(cy+py)), ch, col, k == rings)
		}
	}
	for _, c := range cs {
		a := (c.Bearing - 90) * math.Pi / 180
		f := math.Min(c.Range/100, 1)
		m := []rune(lib.Contacts[c.Kind])
		mark := '●'
		if len(m) > 0 {
			mark = m[0]
		}
		put(int(math.Round(cx+math.Cos(a)*f*rx)), int(math.Round(cy+math.Sin(a)*f*ry)), mark, d.iffColor(c.Kind), true)
	}
	return blk
}

// iffColor tints a contact by its standing: friend, hostile, neutral, unknown.
func (d *drawer) iffColor(k string) layout.Color {
	switch k {
	case "F":
		return rgbC(110, 220, 130)
	case "H":
		return rgbC(255, 80, 70)
	case "N":
		return rgbC(240, 200, 90)
	}
	return rgbC(190, 190, 200)
}
