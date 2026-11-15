// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package raster

// ColorKind says how a terminal colour is expressed.
type ColorKind uint8

const (
	KindDefault ColorKind = iota // the terminal's own colour
	KindANSI                     // N = 0..15
	KindIndexed                  // N = 0..255
	KindRGB                      // R, G, B
)

// TermColor is a colour as a terminal can show it.
type TermColor struct {
	Kind    ColorKind
	N       uint8
	R, G, B uint8
}

// RGBValue is the colour's RGB (the xterm default palette for ANSI/indexed).
func (c TermColor) RGBValue() (r, g, b uint8) {
	switch c.Kind {
	case KindRGB:
		return c.R, c.G, c.B
	case KindANSI, KindIndexed:
		p := paletteRGB(int(c.N))
		return p[0], p[1], p[2]
	}
	return 0, 0, 0
}

// Depth is how many colours the output may use.
type Depth uint8

const (
	TrueColor  Depth = iota // 24-bit
	Indexed256              // xterm 256 (cube + greys; 0..15 avoided, themes vary)
	ANSI16                  // 16 colours
	Mono                    // no colour: default foreground only
)

var ansi16RGB = [16][3]uint8{{0, 0, 0}, {205, 0, 0}, {0, 205, 0}, {205, 205, 0}, {0, 0, 238}, {205, 0, 205}, {0, 205, 205}, {229, 229, 229},
	{127, 127, 127}, {255, 0, 0}, {0, 255, 0}, {255, 255, 0}, {92, 92, 255}, {255, 0, 255}, {0, 255, 255}, {255, 255, 255}}

func paletteRGB(n int) [3]uint8 {
	switch {
	case n < 16:
		return ansi16RGB[n]
	case n < 232:
		n -= 16
		lv := func(i int) uint8 {
			if i == 0 {
				return 0
			}
			return uint8(55 + 40*i)
		}
		return [3]uint8{lv(n / 36), lv(n / 6 % 6), lv(n % 6)}
	}
	g := uint8(8 + 10*(n-232))
	return [3]uint8{g, g, g}
}

// dist is a "redmean" colour distance: cheap, integer, and much closer to
// perception than plain RGB distance. Smaller is nearer.
func dist(r1, g1, b1, r2, g2, b2 int) int {
	rm := (r1 + r2) / 2
	dr, dg, db := r1-r2, g1-g2, b1-b2
	return (512+rm)*dr*dr/256 + 4*dg*dg + (767-rm)*db*db/256
}

// cubeLevels brackets v between two neighbouring xterm cube levels (indexes 0..5).
func cubeLevels(v int) (lo, hi int) {
	if v >= 255 {
		return 5, 5
	}
	lo = max(0, (v-55)/40) // level i>0 is 55+40*i, level 0 is 0
	return lo, min(lo+1, 5)
}

// quantize maps an RGB colour into the depth.
func quantize(r, g, b uint8, d Depth) TermColor {
	switch d {
	case TrueColor:
		return TermColor{Kind: KindRGB, R: r, G: g, B: b}
	case Indexed256:
		// best of the 8 cube corners around the colour and the two nearest greys
		best, bd := 16, 1<<30
		rl, rh := cubeLevels(int(r))
		gl, gh := cubeLevels(int(g))
		bl, bh := cubeLevels(int(b))
		for _, ri := range [2]int{rl, rh} {
			for _, gi := range [2]int{gl, gh} {
				for _, bi := range [2]int{bl, bh} {
					idx := 16 + 36*ri + 6*gi + bi
					cp := paletteRGB(idx)
					if dd := dist(int(r), int(g), int(b), int(cp[0]), int(cp[1]), int(cp[2])); dd < bd {
						best, bd = idx, dd
					}
				}
			}
		}
		avg := (int(r) + int(g) + int(b)) / 3
		gi2 := (avg - 3) / 10
		if gi2 < 0 {
			gi2 = 0
		}
		if gi2 > 23 {
			gi2 = 23
		}
		for _, k := range [2]int{gi2, min(gi2+1, 23)} {
			gp := paletteRGB(232 + k)
			if dd := dist(int(r), int(g), int(b), int(gp[0]), int(gp[1]), int(gp[2])); dd < bd {
				best, bd = 232+k, dd
			}
		}
		return TermColor{Kind: KindIndexed, N: uint8(best)}
	case ANSI16:
		best, bd := 0, 1<<30
		for i, p := range ansi16RGB {
			if dd := dist(int(r), int(g), int(b), int(p[0]), int(p[1]), int(p[2])); dd < bd {
				best, bd = i, dd
			}
		}
		return TermColor{Kind: KindANSI, N: uint8(best)}
	}
	return TermColor{}
}
