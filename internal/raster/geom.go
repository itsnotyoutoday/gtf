// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package raster

import "math"

type pt struct{ x, y float64 }

// subpath is a flattened run of points.
type subpath struct {
	pts    []pt
	closed bool
}

// flatten turns a path into polylines in the path's own coordinates. tol is
// the allowed deviation in the same units.
func flatten(p *Path, tol float64, bud *budget) ([]subpath, error) {
	var out []subpath
	var cur []pt
	var start pt
	flush := func(closed bool) {
		if len(cur) > 0 {
			out = append(out, subpath{pts: cur, closed: closed})
		}
		cur = nil
	}
	last := func() pt {
		if len(cur) > 0 {
			return cur[len(cur)-1]
		}
		return start
	}
	if tol <= 0 || !finite(tol) {
		tol = 0.05
	}
	for _, c := range p.cmds {
		if err := bud.spend(1); err != nil {
			return nil, err
		}
		switch c.op {
		case opMove:
			flush(false)
			start = pt{c.p[0], c.p[1]}
			cur = []pt{start}
		case opLine:
			if cur == nil {
				cur = []pt{start}
			}
			cur = append(cur, pt{c.p[0], c.p[1]})
		case opQuad:
			if cur == nil {
				cur = []pt{start}
			}
			p0 := last()
			p1, p2 := pt{c.p[0], c.p[1]}, pt{c.p[2], c.p[3]}
			dd := hyp(p0.x-2*p1.x+p2.x, p0.y-2*p1.y+p2.y)
			n := segCount(math.Sqrt(dd/(4*tol)), bud)
			if err := bud.spend(int64(n)); err != nil {
				return nil, err
			}
			for i := 1; i <= n; i++ {
				t := float64(i) / float64(n)
				u := 1 - t
				a, b, cc := mul(u, u), mul(2*u, t), mul(t, t)
				cur = append(cur, pt{
					mul(a, p0.x) + mul(b, p1.x) + mul(cc, p2.x),
					mul(a, p0.y) + mul(b, p1.y) + mul(cc, p2.y),
				})
			}
			cur[len(cur)-1] = p2
		case opCubic:
			if cur == nil {
				cur = []pt{start}
			}
			p0 := last()
			p1, p2, p3 := pt{c.p[0], c.p[1]}, pt{c.p[2], c.p[3]}, pt{c.p[4], c.p[5]}
			d1 := hyp(p0.x-2*p1.x+p2.x, p0.y-2*p1.y+p2.y)
			d2 := hyp(p1.x-2*p2.x+p3.x, p1.y-2*p2.y+p3.y)
			n := segCount(math.Sqrt(0.75*math.Max(d1, d2)/tol), bud)
			if err := bud.spend(int64(n)); err != nil {
				return nil, err
			}
			for i := 1; i <= n; i++ {
				t := float64(i) / float64(n)
				u := 1 - t
				a, b, cc, d := mul(mul(u, u), u), mul(mul(3*u, u), t), mul(mul(3*u, t), t), mul(mul(t, t), t)
				cur = append(cur, pt{
					mul(a, p0.x) + mul(b, p1.x) + mul(cc, p2.x) + mul(d, p3.x),
					mul(a, p0.y) + mul(b, p1.y) + mul(cc, p2.y) + mul(d, p3.y),
				})
			}
			cur[len(cur)-1] = p3
		case opClose:
			flush(true)
			// a drawing command after Z continues from the subpath start
			cur = nil
		}
	}
	flush(false)
	return out, nil
}

func segCount(f float64, bud *budget) int {
	if !finite(f) || f < 1 {
		return 1
	}
	if f > 400 {
		return 400
	}
	return int(math.Ceil(f))
}

// polysOf converts subpaths to fill polygons (open ones are implicitly closed).
func polysOf(sp []subpath) [][]pt {
	out := make([][]pt, 0, len(sp))
	for _, s := range sp {
		if len(s.pts) >= 3 {
			out = append(out, s.pts)
		}
	}
	return out
}

func transformPolys(polys [][]pt, m Affine) [][]pt {
	out := make([][]pt, len(polys))
	for i, p := range polys {
		q := make([]pt, len(p))
		for j, v := range p {
			q[j].x, q[j].y = m.Apply(v.x, v.y)
		}
		out[i] = q
	}
	return out
}

// ---- strokes ----

// Cap styles for the ends of open strokes.
const (
	CapButt   = "butt"
	CapRound  = "round"
	CapSquare = "square"
)

// Join styles for corners.
const (
	JoinMiter = "miter"
	JoinRound = "round"
	JoinBevel = "bevel"
)

type strokeStyle struct {
	width      float64
	cap, join  string
	miterLimit float64
}

// stroke outlines the subpaths as a set of consistently wound polygons whose
// non-zero union is the stroke. devScale (device pixels per unit) sizes round
// caps/joins.
func stroke(sp []subpath, st strokeStyle, devScale float64, bud *budget) ([][]pt, error) {
	hw := st.width / 2
	var out [][]pt
	emit := func(poly []pt) error {
		if err := bud.spend(int64(len(poly)) + 4); err != nil {
			return err
		}
		if area2(poly) < 0 {
			for i, j := 0, len(poly)-1; i < j; i, j = i+1, j-1 {
				poly[i], poly[j] = poly[j], poly[i]
			}
		}
		out = append(out, poly)
		return nil
	}
	arcN := 8
	if r := hw * devScale; r > 0 {
		// segments for a 0.1 px chord error: pi / acos(1 - 0.1/r) ~ pi * sqrt(r / 0.2)
		arcN = int(math.Ceil(pi * math.Sqrt(5*math.Max(r, 0.2))))
		if arcN < 8 {
			arcN = 8
		}
		if arcN > 96 {
			arcN = 96
		}
	}
	circle := func(c pt) []pt {
		poly := make([]pt, 2*arcN)
		for i := range poly {
			s, co := sincosRad(mul(float64(i), pi) / float64(arcN))
			poly[i] = pt{c.x + mul(hw, co), c.y + mul(hw, s)}
		}
		return poly
	}
	for _, s := range sp {
		// drop repeated points
		pts := make([]pt, 0, len(s.pts))
		for _, p := range s.pts {
			if len(pts) == 0 || p != pts[len(pts)-1] {
				pts = append(pts, p)
			}
		}
		closed := s.closed
		if closed && len(pts) > 1 && pts[0] == pts[len(pts)-1] {
			pts = pts[:len(pts)-1]
		}
		if len(pts) == 1 {
			// a dot: only round and square caps show it
			c := pts[0]
			switch st.cap {
			case CapRound:
				if err := emit(circle(c)); err != nil {
					return nil, err
				}
			case CapSquare:
				if err := emit([]pt{{c.x - hw, c.y - hw}, {c.x + hw, c.y - hw}, {c.x + hw, c.y + hw}, {c.x - hw, c.y + hw}}); err != nil {
					return nil, err
				}
			}
			continue
		}
		if len(pts) < 2 || (closed && len(pts) < 3 && false) {
			continue
		}
		nseg := len(pts) - 1
		if closed {
			nseg = len(pts)
		}
		// segment rectangles
		type dir struct{ dx, dy float64 }
		dirs := make([]dir, nseg)
		for i := 0; i < nseg; i++ {
			a, b := pts[i], pts[(i+1)%len(pts)]
			dx, dy := b.x-a.x, b.y-a.y
			l := hyp(dx, dy)
			if l == 0 {
				continue
			}
			dirs[i] = dir{dx / l, dy / l}
			nx, ny := -dirs[i].dy*hw, dirs[i].dx*hw
			a0, b0 := a, b
			if !closed && st.cap == CapSquare {
				if i == 0 {
					a0 = pt{a.x - mul(dirs[i].dx, hw), a.y - mul(dirs[i].dy, hw)}
				}
				if i == nseg-1 {
					b0 = pt{b.x + mul(dirs[i].dx, hw), b.y + mul(dirs[i].dy, hw)}
				}
			}
			if err := emit([]pt{{a0.x + nx, a0.y + ny}, {b0.x + nx, b0.y + ny}, {b0.x - nx, b0.y - ny}, {a0.x - nx, a0.y - ny}}); err != nil {
				return nil, err
			}
		}
		// joins
		joinAt := func(v pt, d0, d1 dir) error {
			cross := mul(d0.dx, d1.dy) - mul(d0.dy, d1.dx)
			dot := mul(d0.dx, d1.dx) + mul(d0.dy, d1.dy)
			if math.Abs(cross) < 1e-9 && dot > 0 {
				return nil // straight
			}
			if st.join == JoinRound {
				return emit(circle(v))
			}
			// outer side: opposite to the turn direction
			sgn := 1.0
			if cross > 0 {
				sgn = -1
			}
			n0 := pt{-d0.dy * hw * sgn, d0.dx * hw * sgn}
			n1 := pt{-d1.dy * hw * sgn, d1.dx * hw * sgn}
			p0 := pt{v.x + n0.x, v.y + n0.y}
			p1 := pt{v.x + n1.x, v.y + n1.y}
			if st.join == JoinMiter {
				// miter length / width = 1/sin(theta/2); theta = angle between segments
				cosHalf := math.Sqrt(math.Max(0, (1+dot)/2)) // cos of half the turning angle
				if cosHalf > 1e-6 && 1/cosHalf <= st.miterLimit {
					mx, my := n0.x+n1.x, n0.y+n1.y
					ml := hyp(mx, my)
					if ml > 1e-12 {
						k := hw / cosHalf / ml
						tip := pt{v.x + mul(mx, k), v.y + mul(my, k)}
						return emit([]pt{v, p0, tip, p1})
					}
				}
			}
			return emit([]pt{v, p0, p1})
		}
		for i := 1; i < nseg; i++ {
			if err := joinAt(pts[i], dirs[i-1], dirs[i]); err != nil {
				return nil, err
			}
		}
		if closed {
			if err := joinAt(pts[0], dirs[nseg-1], dirs[0]); err != nil {
				return nil, err
			}
		} else if st.cap == CapRound {
			if err := emit(circle(pts[0])); err != nil {
				return nil, err
			}
			if err := emit(circle(pts[len(pts)-1])); err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}

func area2(p []pt) float64 {
	a := 0.0
	for i := range p {
		j := (i + 1) % len(p)
		a += float64(p[i].x*p[j].y) - float64(p[j].x*p[i].y)
	}
	return a
}

// ---- shape primitives as paths ----

const kappa = 0.5522847498307936

func ellipsePath(cx, cy, rx, ry float64, p *Path) {
	kx, ky := mul(rx, kappa), mul(ry, kappa)
	p.MoveTo(cx+rx, cy)
	p.CubicTo(cx+rx, cy+ky, cx+kx, cy+ry, cx, cy+ry)
	p.CubicTo(cx-kx, cy+ry, cx-rx, cy+ky, cx-rx, cy)
	p.CubicTo(cx-rx, cy-ky, cx-kx, cy-ry, cx, cy-ry)
	p.CubicTo(cx+kx, cy-ry, cx+rx, cy-ky, cx+rx, cy)
	p.Close()
}

func rectPath(x, y, w, h, rx, ry float64, p *Path) {
	rx, ry = math.Min(rx, w/2), math.Min(ry, h/2)
	if rx <= 0 || ry <= 0 {
		p.MoveTo(x, y).LineTo(x+w, y).LineTo(x+w, y+h).LineTo(x, y+h).Close()
		return
	}
	kx, ky := mul(rx, kappa), mul(ry, kappa)
	p.MoveTo(x+rx, y)
	p.LineTo(x+w-rx, y)
	p.CubicTo(x+w-rx+kx, y, x+w, y+ry-ky, x+w, y+ry)
	p.LineTo(x+w, y+h-ry)
	p.CubicTo(x+w, y+h-ry+ky, x+w-rx+kx, y+h, x+w-rx, y+h)
	p.LineTo(x+rx, y+h)
	p.CubicTo(x+rx-kx, y+h, x, y+h-ry+ky, x, y+h-ry)
	p.LineTo(x, y+ry)
	p.CubicTo(x, y+ry-ky, x+rx-kx, y, x+rx, y)
	p.Close()
}
