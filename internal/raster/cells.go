// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package raster

import (
	"strconv"
	"strings"
)

// Mode is how pixels map onto a terminal cell.
type Mode uint8

const (
	HalfBlock Mode = iota // 1x2 pixels per cell: "▀" with fg/bg colours (the default)
	Quadrant              // 2x2: U+2596.. quadrant blocks, two colours per cell
	Sextant               // 2x3: U+1FB00.. sextants (needs a recent font), two colours per cell
	Braille               // 2x4 dots, one colour per cell
	ASCII                 // 1x2 pixels -> one of " .:-=+*#%@" by ink, one colour per cell
)

// PixelsPerCell is the pixel block one cell shows in the mode.
func (m Mode) PixelsPerCell() (px, py int) {
	switch m {
	case Quadrant:
		return 2, 2
	case Sextant:
		return 2, 3
	case Braille:
		return 2, 4
	}
	return 1, 2
}

// PixelAspect is the pixel height/width ratio assuming a cell is twice as tall
// as it is wide, so that Render can keep shapes undistorted.
func (m Mode) PixelAspect() float64 {
	px, py := m.PixelsPerCell()
	return float64(px) * 2 / float64(py)
}

// Options controls cell conversion. It is comparable so it can key a cache.
type Options struct {
	Mode  Mode
	Depth Depth
	// Background, when HasBG, is composited under the sprite so that
	// anti-aliased edges blend into the panel; otherwise pixels under
	// AlphaCut (default 128) are left transparent (terminal default colour)
	// and the rest are shown opaque.
	Background Color
	HasBG      bool
	AlphaCut   uint8
	// Luma makes Braille/ASCII use brightness*alpha as the "ink" instead of alpha alone.
	Luma bool
}

// Cell is one terminal cell.
type Cell struct {
	R      rune
	FG, BG TermColor
}

// Cells is a grid of terminal cells.
type Cells struct {
	W, H int
	C    []Cell
}

// At returns the cell at column x, row y.
func (c *Cells) At(x, y int) Cell { return c.C[y*c.W+x] }

// Text returns the glyphs, one line per row.
func (c *Cells) Text() string {
	var b strings.Builder
	for y := 0; y < c.H; y++ {
		for x := 0; x < c.W; x++ {
			b.WriteRune(c.C[y*c.W+x].R)
		}
		if y < c.H-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func sgrColor(b *strings.Builder, c TermColor, fg bool) {
	switch c.Kind {
	case KindDefault:
		if fg {
			b.WriteString(";39")
		} else {
			b.WriteString(";49")
		}
	case KindANSI:
		base := 30
		if !fg {
			base = 40
		}
		if c.N >= 8 {
			base += 60
		}
		b.WriteString(";" + strconv.Itoa(base+int(c.N&7)))
	case KindIndexed:
		b.WriteString(map[bool]string{true: ";38;5;", false: ";48;5;"}[fg] + strconv.Itoa(int(c.N)))
	case KindRGB:
		b.WriteString(map[bool]string{true: ";38;2;", false: ";48;2;"}[fg] + strconv.Itoa(int(c.R)) + ";" + strconv.Itoa(int(c.G)) + ";" + strconv.Itoa(int(c.B)))
	}
}

// ANSI returns the cells as lines of text with SGR colour codes, resetting at
// each line end. Colour changes are emitted only when needed.
func (c *Cells) ANSI() []string {
	lines := make([]string, c.H)
	for y := 0; y < c.H; y++ {
		var b strings.Builder
		var fg, bg TermColor
		for x := 0; x < c.W; x++ {
			ce := c.C[y*c.W+x]
			if ce.FG != fg || ce.BG != bg {
				b.WriteString("\x1b[0")
				sgrColor(&b, ce.FG, true)
				sgrColor(&b, ce.BG, false)
				b.WriteByte('m')
				fg, bg = ce.FG, ce.BG
			}
			b.WriteRune(ce.R)
		}
		b.WriteString("\x1b[0m")
		lines[y] = b.String()
	}
	return lines
}

// Golden renders the grid as text for golden-file tests: the glyph rows, then
// per row the runs of cells whose fg/bg are not the defaults.
func (c *Cells) Golden() string {
	var b strings.Builder
	b.WriteString("cells " + strconv.Itoa(c.W) + "x" + strconv.Itoa(c.H) + "\n")
	b.WriteString(c.Text())
	b.WriteString("\n--- colours (row: x0-x1 fg bg)\n")
	name := func(t TermColor) string {
		switch t.Kind {
		case KindDefault:
			return "-"
		case KindANSI:
			return "a" + strconv.Itoa(int(t.N))
		case KindIndexed:
			return "i" + strconv.Itoa(int(t.N))
		}
		return Color{t.R, t.G, t.B, 255}.String()
	}
	for y := 0; y < c.H; y++ {
		x := 0
		for x < c.W {
			ce := c.C[y*c.W+x]
			x1 := x
			for x1+1 < c.W && c.C[y*c.W+x1+1].FG == ce.FG && c.C[y*c.W+x1+1].BG == ce.BG {
				x1++
			}
			if ce.FG.Kind != KindDefault || ce.BG.Kind != KindDefault {
				b.WriteString(strconv.Itoa(y) + ": " + strconv.Itoa(x) + "-" + strconv.Itoa(x1) + " " + name(ce.FG) + " " + name(ce.BG) + "\n")
			}
			x = x1 + 1
		}
	}
	return b.String()
}

// RenderCells renders the sprite at cw x ch terminal cells in the given mode
// and converts the result. The pixel aspect of the mode is applied, so art is
// not stretched. p.PixelAspect is overridden.
func RenderCells(s *Sprite, cw, ch int, p *Params, o Options, lim *Limits) (*Cells, error) {
	l := limitsOrDefault(lim)
	if cw < 1 || ch < 1 {
		return nil, invalidErr("cell size %dx%d", cw, ch)
	}
	if cw > l.MaxPixelDim || ch > l.MaxPixelDim || cw*ch > l.MaxCells {
		return nil, limitErr("cell size %dx%d exceeds %d cells", cw, ch, l.MaxCells)
	}
	px, py := o.Mode.PixelsPerCell()
	var par Params
	if p != nil {
		par = *p
	}
	par.PixelAspect = o.Mode.PixelAspect()
	buf, err := s.Render(cw*px, ch*py, &par, &l)
	if err != nil {
		return nil, err
	}
	return Convert(buf, o)
}

// Convert turns a buffer whose size is a whole number of cells for o.Mode into cells.
func Convert(buf *Buffer, o Options) (*Cells, error) {
	px, py := o.Mode.PixelsPerCell()
	if buf.W%px != 0 || buf.H%py != 0 || buf.W == 0 || buf.H == 0 {
		return nil, invalidErr("buffer %dx%d is not a whole number of %dx%d-pixel cells", buf.W, buf.H, px, py)
	}
	cut := int(o.AlphaCut)
	if cut == 0 {
		cut = 128
	}
	cw, ch := buf.W/px, buf.H/py
	out := &Cells{W: cw, H: ch, C: make([]Cell, cw*ch)}
	var pix [8]cpix
	n := px * py
	cache := map[uint32]TermColor{}
	q := func(r, g, b int) TermColor {
		key := uint32(r)<<16 | uint32(g)<<8 | uint32(b)
		if c, ok := cache[key]; ok {
			return c
		}
		c := quantize(uint8(r), uint8(g), uint8(b), o.Depth)
		cache[key] = c
		return c
	}
	for cy := 0; cy < ch; cy++ {
		for cx := 0; cx < cw; cx++ {
			for j := 0; j < py; j++ {
				for i := 0; i < px; i++ {
					pix[j*px+i] = o.pixel(buf, cx*px+i, cy*py+j, cut)
				}
			}
			out.C[cy*cw+cx] = o.cell(pix[:n], q)
		}
	}
	return out, nil
}

// cpix is one pixel prepared for conversion.
type cpix struct {
	r, g, b int
	on      bool // visible (opaque enough or backed by a background)
	ink     int  // 0..255 for the mono modes
}

func (o Options) pixel(buf *Buffer, x, y, cut int) cpix {
	c := buf.At(x, y)
	var p cpix
	a := int(c.A)
	if o.HasBG {
		inv := 255 - a
		p = cpix{
			r:  mul255(int(c.R), a) + mul255(int(o.Background.R), inv),
			g:  mul255(int(c.G), a) + mul255(int(o.Background.G), inv),
			b:  mul255(int(c.B), a) + mul255(int(o.Background.B), inv),
			on: true,
		}
	} else if a >= cut {
		p = cpix{r: int(c.R), g: int(c.G), b: int(c.B), on: true}
	}
	// ink for the monochrome modes: coverage, optionally weighted by brightness
	p.ink = a
	if o.Luma {
		luma := (299*int(c.R) + 587*int(c.G) + 114*int(c.B)) / 1000
		p.ink = mul255(a, luma)
	}
	return p
}

var quadrantRunes = [16]rune{' ', '▘', '▝', '▀', '▖', '▌', '▞', '▛', '▗', '▚', '▐', '▜', '▄', '▙', '▟', '█'}

// sextantRune maps a 6-bit pattern (bit n = sextant n+1, row-major from the top-left).
func sextantRune(p int) rune {
	switch p {
	case 0:
		return ' '
	case 21:
		return '▌'
	case 42:
		return '▐'
	case 63:
		return '█'
	}
	r := 0x1FB00 + p - 1
	if p > 21 {
		r--
	}
	if p > 42 {
		r--
	}
	return rune(r)
}

func (o Options) cell(pix []cpix, q func(r, g, b int) TermColor) Cell {
	switch o.Mode {
	case HalfBlock:
		return o.twoColour(pix, 1, func(p int) rune {
			switch p {
			case 0:
				return ' '
			case 1:
				return '▀'
			case 2:
				return '▄'
			}
			return '█'
		}, q)
	case Quadrant:
		return o.twoColour(pix, 2, func(p int) rune { return quadrantRunes[p] }, q)
	case Sextant:
		return o.twoColour(pix, 6, sextantRune, q)
	case Braille:
		return o.mono(pix, q)
	default:
		return o.mono(pix, q)
	}
}

// twoColour picks the best two-colour split of the cell's pixels. Pattern bit
// i set means pixel i takes the foreground. Pixel order is row-major.
func (o Options) twoColour(pix []cpix, _ int, glyph func(int) rune, q func(r, g, b int) TermColor) Cell {
	n := len(pix)
	full := 1<<n - 1
	// the bit order of the glyph tables: pixel i of a row-major cell is bit i,
	// which matches the half block (top=1,bottom=2), quadrants and sextants.
	vis := 0
	for i, p := range pix {
		if p.on {
			vis |= 1 << i
		}
	}
	if vis == 0 {
		return Cell{R: ' '}
	}
	mean := func(mask int) (int, int, int) {
		var r, g, b, c int
		for i := 0; i < n; i++ {
			if mask&(1<<i) != 0 {
				r, g, b, c = r+pix[i].r, g+pix[i].g, b+pix[i].b, c+1
			}
		}
		if c == 0 {
			return 0, 0, 0
		}
		return (r + c/2) / c, (g + c/2) / c, (b + c/2) / c
	}
	tc := func(mask int) TermColor {
		if o.Depth == Mono {
			return TermColor{}
		}
		r, g, b := mean(mask)
		return q(r, g, b)
	}
	if vis != full {
		// some transparent pixels: those stay terminal-default, the rest take the foreground
		fg := tc(vis)
		if o.Depth == Mono {
			return Cell{R: glyph(vis)}
		}
		return Cell{R: glyph(vis), FG: fg}
	}
	if o.Depth == Mono {
		// no colours at all: light the pixels whose ink is above the cell mean
		return Cell{R: glyph(o.inkPattern(pix))}
	}
	bestErr, bestPat := -1, 0
	// bit n-1 stays background so complementary splits are tried once
	for pat := 0; pat < 1<<(n-1); pat++ {
		bg := full &^ pat
		fr, fg, fb := mean(pat)
		br, bgc, bb := mean(bg)
		e := 0
		for i := 0; i < n; i++ {
			if pat&(1<<i) != 0 {
				e += sq(pix[i].r-fr) + sq(pix[i].g-fg) + sq(pix[i].b-fb)
			} else {
				e += sq(pix[i].r-br) + sq(pix[i].g-bgc) + sq(pix[i].b-bb)
			}
		}
		if bestErr < 0 || e < bestErr {
			bestErr, bestPat = e, pat
		}
	}
	fgc, bgc := tc(bestPat), tc(full&^bestPat)
	if fgc == bgc || bestPat == 0 {
		// one colour: a space with that background has no font seams
		return Cell{R: ' ', BG: tc(full)}
	}
	return Cell{R: glyph(bestPat), FG: fgc, BG: bgc}
}

func sq(v int) int { return v * v }

func (o Options) inkPattern(pix []cpix) int {
	sum := 0
	for _, p := range pix {
		sum += p.ink
	}
	pat := 0
	for i, p := range pix {
		if p.ink*len(pix) > sum {
			pat |= 1 << i
		}
	}
	if pat == 0 && sum > 0 {
		pat = 1<<len(pix) - 1
	}
	return pat
}

var asciiRamp = []rune(" .:-=+*#%@")

// brailleDot maps row-major pixel index (2 wide, 4 tall) to the braille bit.
var brailleDot = [8]int{0x01, 0x08, 0x02, 0x10, 0x04, 0x20, 0x40, 0x80}

// mono renders Braille (one dot per pixel) and ASCII (a density ramp).
func (o Options) mono(pix []cpix, q func(r, g, b int) TermColor) Cell {
	var r, g, b, c, ink int
	for _, p := range pix {
		ink += p.ink
		if p.on {
			r, g, b, c = r+p.r, g+p.g, b+p.b, c+1
		}
	}
	var fg TermColor
	if c > 0 && o.Depth != Mono {
		fg = q((r+c/2)/c, (g+c/2)/c, (b+c/2)/c)
	}
	if o.Mode == Braille {
		bits := 0
		for i, p := range pix {
			if p.ink >= 128 {
				bits |= brailleDot[i]
			}
		}
		if bits == 0 {
			return Cell{R: ' '}
		}
		return Cell{R: rune(0x2800 + bits), FG: fg}
	}
	idx := (ink/len(pix)*(len(asciiRamp)-1) + 127) / 255
	if idx <= 0 {
		return Cell{R: ' '}
	}
	return Cell{R: asciiRamp[idx], FG: fg}
}
