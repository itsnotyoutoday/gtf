// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package raster

import (
	"math"
	"strings"
)

// Affine is the matrix [a c e; b d f; 0 0 1] (the SVG matrix(a b c d e f)):
// x' = a*x + c*y + e, y' = b*x + d*y + f.
type Affine struct{ A, B, C, D, E, F float64 }

// Identity is the identity transform.
var Identity = Affine{1, 0, 0, 1, 0, 0}

// Mul returns m*n: n is applied first, then m.
func (m Affine) Mul(n Affine) Affine {
	return Affine{
		A: float64(m.A*n.A) + float64(m.C*n.B),
		B: float64(m.B*n.A) + float64(m.D*n.B),
		C: float64(m.A*n.C) + float64(m.C*n.D),
		D: float64(m.B*n.C) + float64(m.D*n.D),
		E: float64(m.A*n.E) + float64(m.C*n.F) + m.E,
		F: float64(m.B*n.E) + float64(m.D*n.F) + m.F,
	}
}

// Apply transforms a point.
func (m Affine) Apply(x, y float64) (float64, float64) {
	return float64(m.A*x) + float64(m.C*y) + m.E, float64(m.B*x) + float64(m.D*y) + m.F
}

// Det is the determinant.
func (m Affine) Det() float64 { return float64(m.A*m.D) - float64(m.B*m.C) }

// Invert returns the inverse; ok is false for a singular matrix.
func (m Affine) Invert() (Affine, bool) {
	d := m.Det()
	if d == 0 || !finite(d) {
		return Identity, false
	}
	a, b, c, dd := m.D/d, -m.B/d, -m.C/d, m.A/d
	return Affine{a, b, c, dd, -(float64(a*m.E) + float64(c*m.F)), -(float64(b*m.E) + float64(dd*m.F))}, true
}

// Scale is the geometric mean scale factor, used to size curve tolerances.
func (m Affine) Scale() float64 { return math.Sqrt(math.Abs(m.Det())) }

func translate(x, y float64) Affine { return Affine{1, 0, 0, 1, x, y} }
func scaleM(x, y float64) Affine    { return Affine{x, 0, 0, y, 0, 0} }
func rotateM(deg float64) Affine {
	s, c := sincosDeg(deg)
	return Affine{c, s, -s, c, 0, 0}
}

// ParseTransform parses an SVG-style transform list:
//
//	"translate(10 20) rotate(45) scale(2) rotate(30 50 50) scale(1 -1) matrix(a b c d e f)"
//
// Functions apply right to left like SVG (the last one touches the shape first).
// rotate takes degrees and an optional centre; translate/scale take one or two
// numbers. An empty string is the identity.
func ParseTransform(s string, maxCoord float64) (Affine, error) {
	m := Identity
	s = strings.TrimSpace(s)
	n := 0
	for s != "" {
		n++
		if n > 64 {
			return Identity, limitErr("transform list too long")
		}
		i := strings.IndexByte(s, '(')
		j := strings.IndexByte(s, ')')
		if i <= 0 || j < i {
			return Identity, invalidErr("bad transform %q", s)
		}
		name := strings.ToLower(strings.TrimSpace(s[:i]))
		args, err := parseNumList(s[i+1:j], maxCoord)
		if err != nil {
			return Identity, err
		}
		s = strings.TrimLeft(s[j+1:], " ,\t\n\r")
		var t Affine
		switch {
		case name == "translate" && (len(args) == 1 || len(args) == 2):
			t = translate(args[0], pick(args, 1, 0))
		case name == "scale" && (len(args) == 1 || len(args) == 2):
			t = scaleM(args[0], pick(args, 1, args[0]))
		case name == "rotate" && (len(args) == 1 || len(args) == 3):
			t = rotateM(args[0])
			if len(args) == 3 {
				t = translate(args[1], args[2]).Mul(t).Mul(translate(-args[1], -args[2]))
			}
		case name == "matrix" && len(args) == 6:
			t = Affine{args[0], args[1], args[2], args[3], args[4], args[5]}
		default:
			return Identity, invalidErr("bad transform function %s with %d arguments", name, len(args))
		}
		m = m.Mul(t)
		if !finite(m.A+m.B+m.C+m.D+m.E+m.F) || math.Abs(m.A)+math.Abs(m.B)+math.Abs(m.C)+math.Abs(m.D) > maxCoord*1e3 {
			return Identity, limitErr("transform out of range")
		}
	}
	return m, nil
}

func pick(a []float64, i int, def float64) float64 {
	if i < len(a) {
		return a[i]
	}
	return def
}

func parseNumList(s string, maxCoord float64) ([]float64, error) {
	var out []float64
	sc := numScanner{s: s}
	for {
		sc.skipSep()
		if sc.done() {
			return out, nil
		}
		v, err := sc.number()
		if err != nil {
			return nil, err
		}
		if err := checkNum("transform value", v, maxCoord); err != nil {
			return nil, err
		}
		out = append(out, v)
		if len(out) > 6 {
			return nil, invalidErr("too many transform arguments")
		}
	}
}
