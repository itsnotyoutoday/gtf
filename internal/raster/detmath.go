// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package raster

import "math"

// Rendering must give the same bytes on every platform (the golden tests run
// on amd64 and arm64). Go may fuse x*y+z into one FMA instruction on arm64 and
// the math package's sin/cos/atan2/hypot are not bit-identical across
// architectures. So the geometry code never lets a product feed a sum without
// an explicit float64() conversion (which forbids fusion), and uses the small
// deterministic functions below instead of package math for trigonometry.

// mul is a product that cannot be fused into a following add.
func mul(a, b float64) float64 { return float64(a * b) }

// hyp is sqrt(x*x + y*y), correctly rounded on every platform.
func hyp(x, y float64) float64 { return math.Sqrt(float64(x*x) + float64(y*y)) }

const (
	pi      = 3.141592653589793
	pio2hi  = 1.5707963267948966
	pio2lo  = 6.123233995736766e-17
	deg2rad = pi / 180
)

var sinCoef, cosCoef, atanCoef [12]float64

func init() {
	fact := 1.0
	f := make([]float64, 26)
	f[0] = 1
	for i := 1; i < len(f); i++ {
		fact *= float64(i)
		f[i] = fact
	}
	sign := 1.0
	for k := 0; k < len(sinCoef); k++ {
		sinCoef[k] = sign / f[2*k+1]
		cosCoef[k] = sign / f[2*k]
		atanCoef[k] = sign / float64(2*k+1)
		sign = -sign
	}
}

// sincosSmall is sin and cos for |r| <= ~pi/4 (Taylor, Horner with explicit rounding).
func sincosSmall(r float64) (s, c float64) {
	r2 := mul(r, r)
	ps, pc := sinCoef[9], cosCoef[9]
	for k := 8; k >= 0; k-- {
		ps = mul(ps, r2) + sinCoef[k]
		pc = mul(pc, r2) + cosCoef[k]
	}
	return mul(ps, r), pc
}

func quadrant(s, c float64, q int64) (float64, float64) {
	switch ((q % 4) + 4) % 4 {
	case 1:
		return c, -s
	case 2:
		return -s, -c
	case 3:
		return -c, s
	}
	return s, c
}

// sincosRad returns sin and cos of an angle in radians.
func sincosRad(x float64) (float64, float64) {
	if !finite(x) {
		return 0, 1
	}
	k := math.Floor(x/pio2hi + 0.5)
	r := (x - mul(k, pio2hi)) - mul(k, pio2lo)
	s, c := sincosSmall(r)
	return quadrant(s, c, int64(k))
}

// sincosDeg returns sin and cos of an angle in degrees; multiples of 90 are exact.
func sincosDeg(d float64) (float64, float64) {
	if !finite(d) {
		return 0, 1
	}
	k := math.Floor(d/90 + 0.5)
	r := d - mul(k, 90)
	s, c := sincosSmall(mul(r, deg2rad))
	return quadrant(s, c, int64(k))
}

func atanSmall(x float64) float64 {
	x2 := mul(x, x)
	p := atanCoef[11]
	for k := 10; k >= 0; k-- {
		p = mul(p, x2) + atanCoef[k]
	}
	return mul(p, x)
}

// atanUnit is atan(t) for 0 <= t <= 1.
func atanUnit(t float64) float64 {
	if t > 0.4142135623730951 {
		return pi/4 + atanSmall((t-1)/(t+1))
	}
	return atanSmall(t)
}

// atan2 is a deterministic math.Atan2 (accurate to ~1e-15).
func atan2(y, x float64) float64 {
	if x == 0 && y == 0 {
		return 0
	}
	ax, ay := math.Abs(x), math.Abs(y)
	var a float64
	if ax >= ay {
		a = atanUnit(ay / ax)
	} else {
		a = pio2hi - atanUnit(ax/ay)
	}
	if x < 0 {
		a = pi - a
	}
	if y < 0 {
		a = -a
	}
	return a
}
