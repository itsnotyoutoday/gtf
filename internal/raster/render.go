// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package raster

import (
	"math"
	"sort"
)

// Render rasterises the sprite into a new w x h premultiplied RGBA buffer
// with anti-aliasing. The result is deterministic: the same sprite, size and
// params always produce the same bytes. p and lim may be nil.
func (s *Sprite) Render(w, h int, p *Params, lim *Limits) (*Buffer, error) {
	l := limitsOrDefault(lim)
	if err := s.Prepare(&l); err != nil {
		return nil, err
	}
	if w < 1 || h < 1 {
		return nil, invalidErr("render size %dx%d", w, h)
	}
	if w > l.MaxPixelDim || h > l.MaxPixelDim || w*h > l.MaxPixels {
		return nil, limitErr("render size %dx%d exceeds %d px per side / %d px total", w, h, l.MaxPixelDim, l.MaxPixels)
	}
	var par Params
	if p != nil {
		par = *p
	}
	if par.PixelAspect == 0 {
		par.PixelAspect = 1
	}
	if !finite(par.PixelAspect) || par.PixelAspect < 0.1 || par.PixelAspect > 10 {
		return nil, invalidErr("pixel aspect %g out of range 0.1..10", par.PixelAspect)
	}
	switch par.Samples {
	case 0:
		par.Samples = 8
	case 4, 8, 16, 32:
	default:
		return nil, invalidErr("samples must be 4, 8, 16 or 32")
	}
	for k, v := range par.Zones {
		if !finite(v) {
			return nil, invalidErr("zone %q value is not finite", k)
		}
	}
	for k, v := range par.Glows {
		if !finite(v) {
			return nil, invalidErr("glow %q value is not finite", k)
		}
	}
	r := &renderer{
		w: w, h: h, par: &par, lim: &l, bud: budget{left: l.MaxOps},
		sc: newScanner(w, h, par.Samples), spr: s,
		zone: map[string][]uint8{},
	}
	root := s.rootTransform(w, h, par.PixelAspect)
	r.root = root
	if !finite(root.A + root.B + root.C + root.D + root.E + root.F) {
		return nil, invalidErr("viewBox fit produced a non-finite transform")
	}
	out := newBuffer(w, h)
	for i := range s.Layers {
		if err := r.layer(out, &s.Layers[i]); err != nil {
			return nil, err
		}
	}
	if err := r.tints(out); err != nil {
		return nil, err
	}
	if err := r.glows(out); err != nil {
		return nil, err
	}
	return out, nil
}

type renderer struct {
	w, h int
	par  *Params
	lim  *Limits
	bud  budget
	sc   *scanner
	spr  *Sprite
	root Affine
	zone map[string][]uint8
}

func (r *renderer) zoneActive(id string) bool {
	return id != "" && r.par.Zones[id] > 0
}

func (r *renderer) zoneMask(id string) ([]uint8, error) {
	if m, ok := r.zone[id]; ok {
		return m, nil
	}
	if err := r.bud.spend(int64(r.w * r.h)); err != nil {
		return nil, err
	}
	m := make([]uint8, r.w*r.h)
	r.zone[id] = m
	return m, nil
}

func opacityByte(o float64) int {
	if o == 0 {
		return 255
	}
	return int(mul(o, 255) + 0.5)
}

func (r *renderer) layer(dst *Buffer, ly *Layer) error {
	if ly.Hidden {
		return nil
	}
	m := r.root.Mul(ly.ctm)
	var clip []uint8
	if ly.Clip != nil {
		var err error
		if clip, err = r.clipMask(ly.Clip, m, nil); err != nil {
			return err
		}
	}
	op := opacityByte(ly.Opacity)
	target := dst
	if op < 255 {
		if err := r.bud.spend(int64(r.w * r.h)); err != nil {
			return err
		}
		target = newBuffer(r.w, r.h)
	}
	for i := range ly.Shapes {
		if err := r.shape(target, &ly.Shapes[i], m, clip); err != nil {
			return err
		}
	}
	if target != dst {
		return r.composite(dst, target, op)
	}
	return nil
}

// composite lays src (premultiplied) over dst scaled by op (0..255).
func (r *renderer) composite(dst, src *Buffer, op int) error {
	if err := r.bud.spend(int64(r.w * r.h)); err != nil {
		return err
	}
	for i := 0; i < len(src.Pix); i += 4 {
		sa := int(src.Pix[i+3])
		if sa == 0 {
			continue
		}
		a := mul255(sa, op)
		inv := 255 - a
		d := dst.Pix[i : i+4 : i+4]
		d[0] = uint8(mul255(int(src.Pix[i]), op) + mul255(int(d[0]), inv))
		d[1] = uint8(mul255(int(src.Pix[i+1]), op) + mul255(int(d[1]), inv))
		d[2] = uint8(mul255(int(src.Pix[i+2]), op) + mul255(int(d[2]), inv))
		d[3] = uint8(a + mul255(int(d[3]), inv))
	}
	return nil
}

func intersect(a, b []uint8) []uint8 {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	out := make([]uint8, len(a))
	for i := range a {
		out[i] = uint8(mul255(int(a[i]), int(b[i])))
	}
	return out
}

func (r *renderer) shape(dst *Buffer, sh *Shape, ctm Affine, clip []uint8) error {
	if sh.Hidden {
		return nil
	}
	m := ctm.Mul(sh.tf)
	if sh.Clip != nil {
		cm, err := r.clipMask(sh.Clip, m, nil)
		if err != nil {
			return err
		}
		clip = intersect(clip, cm)
	}
	op := opacityByte(sh.Opacity)
	if sh.Type == TypeGroup {
		target := dst
		if op < 255 {
			if err := r.bud.spend(int64(r.w * r.h)); err != nil {
				return err
			}
			target = newBuffer(r.w, r.h)
		}
		for i := range sh.Children {
			if err := r.shape(target, &sh.Children[i], m, clip); err != nil {
				return err
			}
		}
		if target != dst {
			return r.composite(dst, target, op)
		}
		return nil
	}
	if sh.path == nil || m.Det() == 0 {
		return nil
	}
	scale := m.Scale()
	if !finite(scale) || scale <= 0 {
		return nil
	}
	sp, err := flatten(sh.path, 0.08/scale, &r.bud)
	if err != nil {
		return err
	}
	fill := sh.Fill
	if fill == nil && sh.Stroke == nil {
		fill = Solid(Color{0, 0, 0, 255})
	}
	evenodd := sh.FillRule == EvenOdd
	if fill != nil && fill.Kind != PaintNone || r.zoneActive(sh.Zone) {
		polys := transformPolys(polysOf(sp), m)
		if len(polys) > 0 {
			mk, err := r.sc.coverage(polys, evenodd, &r.bud)
			if err != nil {
				return err
			}
			if fill != nil && fill.Kind != PaintNone {
				if pnt, ok := newPainter(fill, m); ok {
					if err := r.paintMask(dst, mk, clip, &pnt, op); err != nil {
						return err
					}
				}
			}
			if r.zoneActive(sh.Zone) {
				zm, err := r.zoneMask(sh.Zone)
				if err != nil {
					return err
				}
				r.unionMask(zm, mk, clip)
			}
		}
	}
	if sk := sh.Stroke; sk != nil {
		w := sk.Width
		if w == 0 {
			w = 1
		}
		ml := sk.MiterLimit
		if ml == 0 {
			ml = 4
		}
		st := strokeStyle{width: w, cap: sk.Cap, join: sk.Join, miterLimit: ml}
		if st.cap == "" {
			st.cap = CapButt
		}
		if st.join == "" {
			st.join = JoinMiter
		}
		polys, err := stroke(sp, st, scale, &r.bud)
		if err != nil {
			return err
		}
		if len(polys) > 0 {
			mk, err := r.sc.coverage(transformPolys(polys, m), false, &r.bud)
			if err != nil {
				return err
			}
			paint := sk.Paint
			if paint == nil {
				paint = Solid(Color{0, 0, 0, 255})
			}
			if pnt, ok := newPainter(paint, m); ok {
				if err := r.paintMask(dst, mk, clip, &pnt, op); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// paintMask composites the painter through the mask (and clip) onto dst.
func (r *renderer) paintMask(dst *Buffer, mk mask, clip []uint8, pnt *painter, op int) error {
	if mk.w == 0 {
		return nil
	}
	if err := r.bud.spend(int64(mk.w * mk.h)); err != nil {
		return err
	}
	for y := 0; y < mk.h; y++ {
		row := mk.a[y*mk.w : (y+1)*mk.w]
		di := ((mk.y0+y)*dst.W + mk.x0) * 4
		ci := (mk.y0+y)*r.w + mk.x0
		for x, cov := range row {
			if cov == 0 {
				continue
			}
			a := int(cov)
			if clip != nil {
				a = mul255(a, int(clip[ci+x]))
				if a == 0 {
					continue
				}
			}
			if op < 255 {
				a = mul255(a, op)
			}
			c := pnt.at(mk.x0+x, mk.y0+y)
			if c.A < 255 {
				a = mul255(a, int(c.A))
			}
			dst.over(di+x*4, c, a)
		}
	}
	return nil
}

// unionMask merges coverage (through clip) into a full-size mask: a + b - ab.
func (r *renderer) unionMask(dst []uint8, mk mask, clip []uint8) {
	for y := 0; y < mk.h; y++ {
		row := mk.a[y*mk.w : (y+1)*mk.w]
		ci := (mk.y0+y)*r.w + mk.x0
		for x, cov := range row {
			if cov == 0 {
				continue
			}
			a := int(cov)
			if clip != nil {
				a = mul255(a, int(clip[ci+x]))
			}
			d := int(dst[ci+x])
			dst[ci+x] = uint8(d + a - mul255(d, a))
		}
	}
}

// clipMask returns the coverage of a clip shape's geometry over the whole target.
func (r *renderer) clipMask(sh *Shape, m Affine, into []uint8) ([]uint8, error) {
	if into == nil {
		if err := r.bud.spend(int64(r.w * r.h)); err != nil {
			return nil, err
		}
		into = make([]uint8, r.w*r.h)
	}
	return into, r.unionShape(into, sh, m)
}

func (r *renderer) unionShape(dst []uint8, sh *Shape, ctm Affine) error {
	m := ctm.Mul(sh.tf)
	var sub []uint8
	if sh.Clip != nil {
		cm, err := r.clipMask(sh.Clip, m, nil)
		if err != nil {
			return err
		}
		sub = cm
	}
	if sh.Type == TypeGroup {
		for i := range sh.Children {
			if err := r.unionShape(dst, &sh.Children[i], m); err != nil {
				return err
			}
		}
		return nil
	}
	if sh.path == nil || m.Det() == 0 {
		return nil
	}
	scale := m.Scale()
	if !finite(scale) || scale <= 0 {
		return nil
	}
	sp, err := flatten(sh.path, 0.08/scale, &r.bud)
	if err != nil {
		return err
	}
	polys := transformPolys(polysOf(sp), m)
	if len(polys) == 0 {
		return nil
	}
	mk, err := r.sc.coverage(polys, sh.FillRule == EvenOdd, &r.bud)
	if err != nil {
		return err
	}
	r.unionMask(dst, mk, sub)
	return nil
}

var defaultZoneColor = Color{255, 48, 48, 180}

// tints applies active zones over the finished sprite.
func (r *renderer) tints(out *Buffer) error {
	var ids []string
	for id, v := range r.par.Zones {
		if v > 0 {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	for _, id := range ids {
		col := defaultZoneColor
		var zdef *Zone
		for i := range r.spr.Zones {
			if r.spr.Zones[i].ID == id {
				zdef = &r.spr.Zones[i]
				col = zdef.Color
				if col == (Color{}) {
					col = defaultZoneColor
				}
				break
			}
		}
		mk, err := r.zoneMask(id)
		if err != nil {
			return err
		}
		if zdef != nil {
			for i := range zdef.Shapes {
				if err := r.unionShape(mk, &zdef.Shapes[i], r.root); err != nil {
					return err
				}
			}
		}
		v := math.Min(1, r.par.Zones[id])
		strength := int(mul(v, float64(col.A)) + 0.5) // 0..255
		if err := r.bud.spend(int64(r.w * r.h)); err != nil {
			return err
		}
		for i, m := range mk {
			if m == 0 {
				continue
			}
			p := out.Pix[i*4 : i*4+4 : i*4+4]
			da := int(p[3])
			if da == 0 {
				continue
			}
			k := mul255(int(m), strength)
			inv := 255 - k
			// premultiplied: tint colour scaled by the pixel's own alpha (source-atop)
			p[0] = uint8(mul255(int(p[0]), inv) + mul255(mul255(int(col.R), k), da))
			p[1] = uint8(mul255(int(p[1]), inv) + mul255(mul255(int(col.G), k), da))
			p[2] = uint8(mul255(int(p[2]), inv) + mul255(mul255(int(col.B), k), da))
		}
	}
	return nil
}

// glows adds the radial lights on top.
func (r *renderer) glows(out *Buffer) error {
	for i := range r.spr.Glows {
		g := &r.spr.Glows[i]
		in := g.Intensity
		if in == 0 {
			in = 1
		}
		if v, ok := r.par.Glows[g.ID]; ok {
			in = v
		}
		in = math.Min(1, in)
		if in <= 0 || g.R <= 0 || g.Color.A == 0 {
			continue
		}
		cx, cy := r.root.Apply(g.X, g.Y)
		rx := g.R * hyp(r.root.A, r.root.B)
		ry := g.R * hyp(r.root.C, r.root.D)
		if !finite(cx+cy+rx+ry) || rx <= 0 || ry <= 0 {
			continue
		}
		x0, x1 := int(math.Max(0, math.Floor(cx-rx))), int(math.Min(float64(r.w), math.Ceil(cx+rx)))
		y0, y1 := int(math.Max(0, math.Floor(cy-ry))), int(math.Min(float64(r.h), math.Ceil(cy+ry)))
		if x1 <= x0 || y1 <= y0 {
			continue
		}
		if err := r.bud.spend(int64(x1-x0) * int64(y1-y0)); err != nil {
			return err
		}
		peak := in * float64(g.Color.A)
		for y := y0; y < y1; y++ {
			dy := (float64(y) + 0.5 - cy) / ry
			for x := x0; x < x1; x++ {
				dx := (float64(x) + 0.5 - cx) / rx
				d2 := mul(dx, dx) + mul(dy, dy)
				if d2 >= 1 {
					continue
				}
				f := 1 - d2
				sa := int(mul(mul(peak, f), f) + 0.5)
				if sa <= 0 {
					continue
				}
				p := out.Pix[(y*r.w+x)*4 : (y*r.w+x)*4+4 : (y*r.w+x)*4+4]
				na := sa + int(p[3]) - mul255(sa, int(p[3]))
				add := func(old uint8, c uint8) uint8 {
					return uint8(min(na, int(old)+mul255(int(c), sa)))
				}
				p[0], p[1], p[2], p[3] = add(p[0], g.Color.R), add(p[1], g.Color.G), add(p[2], g.Color.B), uint8(na)
			}
		}
	}
	return nil
}
