// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package raster

import (
	"math"
	"strconv"
)

// Path operations. Everything is absolute; arcs are converted to cubics.
const (
	opMove  = 'M'
	opLine  = 'L'
	opQuad  = 'Q'
	opCubic = 'C'
	opClose = 'Z'
)

type pathCmd struct {
	op byte
	p  [6]float64 // move/line: x y; quad: cx cy x y; cubic: c1x c1y c2x c2y x y
}

// Path is a vector outline. Build it with the methods or ParsePath.
type Path struct {
	cmds       []pathCmd
	cx, cy     float64 // current point
	sx, sy     float64 // subpath start
	lastCtl    [2]float64
	lastKind   byte // previous command kind, for smooth S/T
	maxSeg     int
	maxCoord   float64
	err        error
	haveCursor bool
}

// NewPath makes an empty path with the default limits.
func NewPath() *Path { return newPath(DefaultLimits()) }

func newPath(l Limits) *Path { return &Path{maxSeg: l.MaxSegments, maxCoord: l.MaxCoord} }

// Err reports the first error a builder call hit (limit, non-finite number).
func (p *Path) Err() error { return p.err }

// Len is the number of path commands.
func (p *Path) Len() int { return len(p.cmds) }

func (p *Path) add(c pathCmd, nums int) bool {
	if p.err != nil {
		return false
	}
	for i := 0; i < nums; i++ {
		if err := checkNum("path coordinate", c.p[i], p.maxCoord); err != nil {
			p.err = err
			return false
		}
	}
	if len(p.cmds) >= p.maxSeg {
		p.err = limitErr("path has more than %d segments", p.maxSeg)
		return false
	}
	p.cmds = append(p.cmds, c)
	return true
}

// MoveTo starts a new subpath.
func (p *Path) MoveTo(x, y float64) *Path {
	if p.add(pathCmd{op: opMove, p: [6]float64{x, y}}, 2) {
		p.cx, p.cy, p.sx, p.sy, p.lastKind, p.haveCursor = x, y, x, y, 'M', true
	}
	return p
}

func (p *Path) ensureStart() {
	if !p.haveCursor {
		p.MoveTo(0, 0)
	}
}

// LineTo draws a straight line.
func (p *Path) LineTo(x, y float64) *Path {
	p.ensureStart()
	if p.add(pathCmd{op: opLine, p: [6]float64{x, y}}, 2) {
		p.cx, p.cy, p.lastKind = x, y, 'L'
	}
	return p
}

// QuadTo draws a quadratic Bezier.
func (p *Path) QuadTo(cx, cy, x, y float64) *Path {
	p.ensureStart()
	if p.add(pathCmd{op: opQuad, p: [6]float64{cx, cy, x, y}}, 4) {
		p.lastCtl = [2]float64{cx, cy}
		p.cx, p.cy, p.lastKind = x, y, 'Q'
	}
	return p
}

// CubicTo draws a cubic Bezier.
func (p *Path) CubicTo(c1x, c1y, c2x, c2y, x, y float64) *Path {
	p.ensureStart()
	if p.add(pathCmd{op: opCubic, p: [6]float64{c1x, c1y, c2x, c2y, x, y}}, 6) {
		p.lastCtl = [2]float64{c2x, c2y}
		p.cx, p.cy, p.lastKind = x, y, 'C'
	}
	return p
}

// Close closes the current subpath.
func (p *Path) Close() *Path {
	if p.haveCursor && p.add(pathCmd{op: opClose}, 0) {
		p.cx, p.cy, p.lastKind = p.sx, p.sy, 'Z'
	}
	return p
}

// ArcTo draws an SVG elliptical arc from the current point to (x, y).
// rot is the x-axis rotation in degrees.
func (p *Path) ArcTo(rx, ry, rot float64, large, sweep bool, x, y float64) *Path {
	p.ensureStart()
	if p.err != nil {
		return p
	}
	for _, v := range []float64{rx, ry, rot, x, y} {
		if err := checkNum("arc value", v, p.maxCoord); err != nil {
			p.err = err
			return p
		}
	}
	x0, y0 := p.cx, p.cy
	rx, ry = math.Abs(rx), math.Abs(ry)
	if x0 == x && y0 == y {
		return p
	}
	if rx == 0 || ry == 0 {
		return p.LineTo(x, y)
	}
	sphi, cphi := sincosDeg(rot)
	dx, dy := (x0-x)/2, (y0-y)/2
	x1 := mul(cphi, dx) + mul(sphi, dy)
	y1 := mul(-sphi, dx) + mul(cphi, dy)
	lam := mul(x1, x1)/mul(rx, rx) + mul(y1, y1)/mul(ry, ry)
	if lam > 1 {
		s := math.Sqrt(lam)
		rx, ry = mul(rx, s), mul(ry, s)
	}
	rx2, ry2 := mul(rx, rx), mul(ry, ry)
	num := mul(rx2, ry2) - mul(rx2, mul(y1, y1)) - mul(ry2, mul(x1, x1))
	den := mul(rx2, mul(y1, y1)) + mul(ry2, mul(x1, x1))
	co := 0.0
	if den != 0 && num > 0 {
		co = math.Sqrt(num / den)
	}
	if large == sweep {
		co = -co
	}
	cxp, cyp := mul(co, rx)*y1/ry, -mul(co, ry)*x1/rx
	ccx := mul(cphi, cxp) - mul(sphi, cyp) + (x0+x)/2
	ccy := mul(sphi, cxp) + mul(cphi, cyp) + (y0+y)/2
	ang := func(ux, uy, vx, vy float64) float64 {
		return atan2(mul(ux, vy)-mul(uy, vx), mul(ux, vx)+mul(uy, vy))
	}
	th1 := ang(1, 0, (x1-cxp)/rx, (y1-cyp)/ry)
	dth := ang((x1-cxp)/rx, (y1-cyp)/ry, (-x1-cxp)/rx, (-y1-cyp)/ry)
	if !sweep && dth > 0 {
		dth -= 2 * pi
	} else if sweep && dth < 0 {
		dth += 2 * pi
	}
	n := int(math.Ceil(mul(math.Abs(dth)/(pi/2), 0.999999)))
	if n < 1 {
		n = 1
	}
	d := dth / float64(n)
	ts, tc := sincosRad(d / 4)
	t := mul(4.0/3.0, ts/tc)
	pt := func(th float64) (float64, float64, float64, float64) {
		s, c := sincosRad(th)
		// point and derivative direction on the rotated ellipse
		rc, rs := mul(rx, c), mul(ry, s)
		px := ccx + mul(rc, cphi) - mul(rs, sphi)
		py := ccy + mul(rc, sphi) + mul(rs, cphi)
		rs2, rc2 := mul(rx, s), mul(ry, c)
		tx := -mul(rs2, cphi) - mul(rc2, sphi)
		ty := -mul(rs2, sphi) + mul(rc2, cphi)
		return px, py, tx, ty
	}
	th := th1
	for i := 0; i < n; i++ {
		ax, ay, atx, aty := pt(th)
		bx, by, btx, bty := pt(th + d)
		ex, ey := bx, by
		if i == n-1 {
			ex, ey = x, y
		}
		p.CubicTo(ax+mul(t, atx), ay+mul(t, aty), bx-mul(t, btx), by-mul(t, bty), ex, ey)
		th += d
	}
	p.lastKind = 'A'
	return p
}

// ---- SVG path data ----

// ParsePath parses SVG path data (the `d` attribute): M m L l H h V v C c S s
// Q q T t A a Z z, with implicit repeated commands and compact numbers
// ("M10-5.5.5" and arc flags written without separators). Errors wrap
// ErrInvalid or ErrLimit.
func ParsePath(d string, lim *Limits) (*Path, error) {
	l := limitsOrDefault(lim)
	if len(d) > l.MaxPathBytes {
		return nil, limitErr("path data is %d bytes (max %d)", len(d), l.MaxPathBytes)
	}
	p := newPath(l)
	sc := numScanner{s: d}
	var cmd byte
	first := true
	for {
		sc.skipSep()
		if sc.done() {
			break
		}
		c := sc.s[sc.i]
		if isCmdLetter(c) {
			cmd = c
			sc.i++
			if first && cmd != 'M' && cmd != 'm' {
				return nil, invalidErr("path data must start with M")
			}
			first = false
			if cmd == 'Z' || cmd == 'z' {
				p.Close()
				if p.err != nil {
					return nil, p.err
				}
				continue
			}
		} else if cmd == 0 || cmd == 'Z' || cmd == 'z' {
			return nil, invalidErr("unexpected %q in path data at offset %d", c, sc.i)
		}
		rel := cmd >= 'a'
		up := cmd &^ 0x20
		n := map[byte]int{'M': 2, 'L': 2, 'H': 1, 'V': 1, 'C': 6, 'S': 4, 'Q': 4, 'T': 2, 'A': 7}[up]
		var a [7]float64
		for i := 0; i < n; i++ {
			sc.skipSep()
			var v float64
			var err error
			if up == 'A' && (i == 3 || i == 4) {
				v, err = sc.flag()
			} else {
				v, err = sc.number()
			}
			if err != nil {
				return nil, err
			}
			a[i] = v
		}
		ox, oy := 0.0, 0.0
		if rel {
			ox, oy = p.cx, p.cy
		}
		switch up {
		case 'M':
			p.MoveTo(a[0]+ox, a[1]+oy)
			if cmd == 'M' {
				cmd = 'L'
			} else {
				cmd = 'l'
			}
		case 'L':
			p.LineTo(a[0]+ox, a[1]+oy)
		case 'H':
			p.LineTo(a[0]+ox, p.cy)
		case 'V':
			p.LineTo(p.cx, a[0]+oy)
		case 'C':
			p.CubicTo(a[0]+ox, a[1]+oy, a[2]+ox, a[3]+oy, a[4]+ox, a[5]+oy)
		case 'S':
			c1x, c1y := p.cx, p.cy
			if p.lastKind == 'C' {
				c1x, c1y = 2*p.cx-p.lastCtl[0], 2*p.cy-p.lastCtl[1]
			}
			p.CubicTo(c1x, c1y, a[0]+ox, a[1]+oy, a[2]+ox, a[3]+oy)
		case 'Q':
			p.QuadTo(a[0]+ox, a[1]+oy, a[2]+ox, a[3]+oy)
		case 'T':
			cx, cy := p.cx, p.cy
			if p.lastKind == 'Q' {
				cx, cy = 2*p.cx-p.lastCtl[0], 2*p.cy-p.lastCtl[1]
			}
			p.QuadTo(cx, cy, a[0]+ox, a[1]+oy)
		case 'A':
			p.ArcTo(a[0], a[1], a[2], a[3] != 0, a[4] != 0, a[5]+ox, a[6]+oy)
		}
		if p.err != nil {
			return nil, p.err
		}
	}
	if first {
		return nil, invalidErr("empty path data")
	}
	return p, nil
}

func isCmdLetter(c byte) bool {
	switch c | 0x20 {
	case 'm', 'l', 'h', 'v', 'c', 's', 'q', 't', 'a', 'z':
		return true
	}
	return false
}

// numScanner reads SVG numbers.
type numScanner struct {
	s string
	i int
}

func (n *numScanner) done() bool { return n.i >= len(n.s) }

func (n *numScanner) skipSep() {
	for n.i < len(n.s) {
		switch n.s[n.i] {
		case ' ', '\t', '\n', '\r', ',':
			n.i++
		default:
			return
		}
	}
}

func (n *numScanner) flag() (float64, error) {
	if n.i < len(n.s) && (n.s[n.i] == '0' || n.s[n.i] == '1') {
		v := float64(n.s[n.i] - '0')
		n.i++
		return v, nil
	}
	return 0, invalidErr("bad arc flag at offset %d", n.i)
}

func (n *numScanner) number() (float64, error) {
	s, i := n.s, n.i
	start := i
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		i++
	}
	digits := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
		digits++
	}
	if i < len(s) && s[i] == '.' {
		i++
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
			digits++
		}
	}
	if digits == 0 {
		return 0, invalidErr("expected a number at offset %d", start)
	}
	if i < len(s) && (s[i] == 'e' || s[i] == 'E') {
		j := i + 1
		if j < len(s) && (s[j] == '+' || s[j] == '-') {
			j++
		}
		k := j
		for k < len(s) && s[k] >= '0' && s[k] <= '9' {
			k++
		}
		if k > j {
			i = k
		}
	}
	v, err := strconv.ParseFloat(s[start:i], 64)
	if err != nil && !isRangeErr(err) {
		return 0, invalidErr("bad number %q", s[start:i])
	}
	n.i = i
	return v, nil // an out-of-range value comes back as Inf and is rejected by checkNum
}

func isRangeErr(err error) bool {
	ne, ok := err.(*strconv.NumError)
	return ok && ne.Err == strconv.ErrRange
}
