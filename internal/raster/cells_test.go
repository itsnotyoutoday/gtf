// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package raster

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// solid builds a buffer from rows of single-letter colours: r g b w k (black) . (transparent)
func bufFrom(rows ...string) *Buffer {
	b := newBuffer(len(rows[0]), len(rows))
	pal := map[byte]Color{'r': {255, 0, 0, 255}, 'g': {0, 255, 0, 255}, 'b': {0, 0, 255, 255}, 'w': {255, 255, 255, 255}, 'k': {0, 0, 0, 255}, 'y': {255, 255, 0, 255}}
	for y, row := range rows {
		for x := range row {
			if c, ok := pal[row[x]]; ok {
				i := (y*b.W + x) * 4
				b.Pix[i], b.Pix[i+1], b.Pix[i+2], b.Pix[i+3] = c.R, c.G, c.B, 255
			}
		}
	}
	return b
}

func conv(t *testing.T, b *Buffer, o Options) *Cells {
	t.Helper()
	c, err := Convert(b, o)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func rgbc(r, g, b uint8) TermColor { return TermColor{Kind: KindRGB, R: r, G: g, B: b} }

func TestHalfBlockCells(t *testing.T) {
	// columns: top only | bottom only | two colours | same colour | empty
	c := conv(t, bufFrom("r.rbw.", ".gbbw."), Options{})
	if c.W != 6 || c.H != 1 {
		t.Fatalf("%dx%d", c.W, c.H)
	}
	want := []Cell{
		{'▀', rgbc(255, 0, 0), TermColor{}},
		{'▄', rgbc(0, 255, 0), TermColor{}},
		{'▀', rgbc(255, 0, 0), rgbc(0, 0, 255)},
		{' ', TermColor{}, rgbc(0, 0, 255)}, // one colour: a space with that background, no font seams
		{' ', TermColor{}, rgbc(255, 255, 255)},
		{' ', TermColor{}, TermColor{}},
	}
	// column 4 is 'w','w' -> white background; column 3 is b,b
	for i, w := range want {
		if got := c.C[i]; got != w {
			t.Errorf("cell %d = %+v, want %+v", i, got, w)
		}
	}
}

func TestQuadrantAndSextantGlyphs(t *testing.T) {
	// every 2x2 split of two colours must pick the glyph that draws exactly those pixels
	for pat := 1; pat < 15; pat++ {
		rows := [2][2]byte{}
		for i := 0; i < 4; i++ {
			col := byte('b')
			if pat&(1<<i) != 0 {
				col = 'r'
			}
			rows[i/2][i%2] = col
		}
		c := conv(t, bufFrom(string(rows[0][:]), string(rows[1][:])), Options{Mode: Quadrant})
		ce := c.C[0]
		// reconstruct which pixels the cell shows in fg
		got := 0
		for i := 0; i < 4; i++ {
			lit := false
			for p, r := range quadrantRunes {
				if r == ce.R && p&(1<<i) != 0 {
					lit = true
				}
			}
			isFG := (lit && ce.FG == rgbc(255, 0, 0)) || (!lit && ce.BG == rgbc(255, 0, 0))
			if isFG {
				got |= 1 << i
			}
		}
		if got != pat {
			t.Errorf("pattern %04b drawn as %q fg=%v bg=%v (reads back as %04b)", pat, ce.R, ce.FG, ce.BG, got)
		}
	}
	// sextants: every pattern maps to a distinct valid rune and the table skips exactly 21 and 42
	seen := map[rune]int{}
	for p := 0; p < 64; p++ {
		r := sextantRune(p)
		if q, dup := seen[r]; dup {
			t.Errorf("patterns %d and %d share %U", q, p, r)
		}
		seen[r] = p
		if p != 0 && p != 21 && p != 42 && p != 63 && (r < 0x1FB00 || r > 0x1FB3B) {
			t.Errorf("pattern %d -> %U out of the sextant block", p, r)
		}
	}
	if sextantRune(1) != 0x1FB00 || sextantRune(20) != 0x1FB13 || sextantRune(22) != 0x1FB14 || sextantRune(62) != 0x1FB3B {
		t.Errorf("sextant anchors: %U %U %U %U", sextantRune(1), sextantRune(20), sextantRune(22), sextantRune(62))
	}
	// and the converter splits a 2x3 cell correctly: left column red, right blue -> left-half block
	c := conv(t, bufFrom("rb", "rb", "rb"), Options{Mode: Sextant})
	if c.C[0].R != '▌' || c.C[0].FG != rgbc(255, 0, 0) || c.C[0].BG != rgbc(0, 0, 255) {
		t.Errorf("sextant split: %+v", c.C[0])
	}
}

func TestBrailleAndASCII(t *testing.T) {
	c := conv(t, bufFrom("w.", "..", "..", ".w"), Options{Mode: Braille})
	if c.C[0].R != 0x2800+0x01+0x80 {
		t.Errorf("braille = %U", c.C[0].R)
	}
	if c.C[0].FG != rgbc(255, 255, 255) {
		t.Errorf("braille fg = %+v", c.C[0].FG)
	}
	m := conv(t, bufFrom("w.", "..", "..", ".w"), Options{Mode: Braille, Depth: Mono})
	if m.C[0].FG.Kind != KindDefault {
		t.Error("mono braille should not set a colour")
	}
	a := conv(t, bufFrom("ww..", "ww.."), Options{Mode: ASCII})
	if a.C[0].R != '@' || a.C[2].R != ' ' {
		t.Errorf("ascii ramp: %q %q", a.C[0].R, a.C[2].R)
	}
	half := conv(t, bufFrom("w", "."), Options{Mode: ASCII})
	if r := half.C[0].R; r == ' ' || r == '@' {
		t.Errorf("half-ink cell should be mid ramp, got %q", r)
	}
}

func TestQuantisation(t *testing.T) {
	cases := []struct {
		r, g, b uint8
		idx256  int
		ansi    int
	}{
		{0, 0, 0, 16, 0},
		{255, 255, 255, 231, 15},
		{255, 0, 0, 196, 9},
		{0, 255, 0, 46, 10},
		{128, 128, 128, 244, 8}, // a grey ramp entry, not the cube
		{95, 135, 215, 68, 12},  // exactly a cube entry
	}
	for _, c := range cases {
		q := quantize(c.r, c.g, c.b, Indexed256)
		if q.Kind != KindIndexed || int(q.N) != c.idx256 {
			t.Errorf("256(%d,%d,%d) = %+v want %d", c.r, c.g, c.b, q, c.idx256)
		}
		a := quantize(c.r, c.g, c.b, ANSI16)
		if a.Kind != KindANSI || int(a.N) != c.ansi {
			t.Errorf("16(%d,%d,%d) = %+v want %d", c.r, c.g, c.b, a, c.ansi)
		}
	}
	// 256-colour output never uses the theme-dependent 0..15 range
	for v := 0; v < 256; v += 5 {
		for w := 0; w < 256; w += 17 {
			if q := quantize(uint8(v), uint8(w), uint8(255-v), Indexed256); q.N < 16 {
				t.Fatalf("256 mode produced base colour %d for %d,%d", q.N, v, w)
			}
		}
	}
	// perceptual: a dark desaturated blue is nearer dark blue than black in 16 colours
	if q := quantize(20, 20, 160, ANSI16); q.N != 4 {
		t.Errorf("dark blue -> ansi %d", q.N)
	}
}

func TestTransparencyAndBackground(t *testing.T) {
	// a half-transparent pixel: dropped without a background, blended with one
	b := newBuffer(1, 2)
	b.Pix = []uint8{100, 0, 0, 100, 0, 0, 0, 0} // 39% red over nothing
	c := conv(t, b, Options{})
	if c.C[0].R != ' ' || c.C[0].FG.Kind != KindDefault || c.C[0].BG.Kind != KindDefault {
		t.Errorf("under the alpha cut: %+v", c.C[0])
	}
	c = conv(t, b, Options{AlphaCut: 90})
	if c.C[0].R != '▀' || c.C[0].FG != rgbc(255, 0, 0) {
		t.Errorf("over the alpha cut: %+v", c.C[0])
	}
	c = conv(t, b, Options{HasBG: true, Background: Color{0, 0, 255, 255}})
	if c.C[0].R != '▀' || c.C[0].FG != rgbc(100, 0, 155) || c.C[0].BG != rgbc(0, 0, 255) {
		t.Errorf("blended: %+v", c.C[0])
	}
}

func TestConvertSizeCheck(t *testing.T) {
	if _, err := Convert(newBuffer(3, 3), Options{Mode: Quadrant}); err == nil {
		t.Error("3x3 is not whole quadrant cells")
	}
	if _, err := Convert(newBuffer(0, 0), Options{}); err == nil {
		t.Error("empty buffer")
	}
}

func TestANSIOutput(t *testing.T) {
	c := conv(t, bufFrom("rb", "gk"), Options{})
	lines := c.ANSI()
	if len(lines) != 1 || !strings.Contains(lines[0], "\x1b[0;38;2;255;0;0;48;2;0;255;0m▀") || !strings.HasSuffix(lines[0], "\x1b[0m") {
		t.Errorf("ansi: %q", lines)
	}
	c = conv(t, bufFrom("rr", "rr"), Options{Depth: ANSI16})
	if !strings.Contains(c.ANSI()[0], ";49;41m") && !strings.Contains(c.ANSI()[0], ";39;101m") {
		t.Errorf("16 colour sgr: %q", c.ANSI()[0])
	}
}

func TestSampleSpritesAreVisible(t *testing.T) {
	for _, n := range []string{"saucer", "warbird", "planet"} {
		s := loadSprite(t, n)
		c, err := RenderCells(s, 30, 15, nil, Options{}, nil)
		if err != nil {
			t.Fatal(err)
		}
		filled := 0
		for _, ce := range c.C {
			if ce.R != ' ' || ce.BG.Kind != KindDefault {
				filled++
			}
		}
		if filled < 30*15/6 {
			t.Errorf("%s: only %d of %d cells drawn", n, filled, len(c.C))
		}
	}
}

// ---- goldens ----

var goldenCases = []struct {
	name   string
	sprite string
	cw, ch int
	opt    Options
	params *Params
}{
	{"saucer_half_24x10", "saucer", 24, 10, Options{}, nil},
	{"saucer_half_bg_48x20", "saucer", 48, 20, Options{HasBG: true, Background: Color{14, 16, 24, 255}}, nil},
	{"saucer_quadrant_32x12", "saucer", 32, 12, Options{Mode: Quadrant, HasBG: true, Background: Color{14, 16, 24, 255}}, nil},
	{"saucer_damaged_32x12", "saucer", 32, 12, Options{HasBG: true, Background: Color{14, 16, 24, 255}},
		&Params{Zones: map[string]float64{"port": 1, "bridge": 0.6}, Glows: map[string]float64{"starboard-engine": 0}}},
	{"warbird_half_20x10", "warbird", 20, 10, Options{}, nil},
	{"warbird_sextant_24x12", "warbird", 24, 12, Options{Mode: Sextant}, nil},
	{"warbird_braille_24x12", "warbird", 24, 12, Options{Mode: Braille}, nil},
	{"warbird_ascii_30x14", "warbird", 30, 14, Options{Mode: ASCII, Depth: Mono}, nil},
	{"planet_half_bg_24x12", "planet", 24, 12, Options{HasBG: true, Background: Color{6, 8, 16, 255}}, nil},
	{"planet_256_32x16", "planet", 32, 16, Options{Depth: Indexed256, HasBG: true, Background: Color{6, 8, 16, 255}}, nil},
	{"planet_16_32x16", "planet", 32, 16, Options{Depth: ANSI16, HasBG: true, Background: Color{0, 0, 0, 255}}, nil},
}

func TestGoldens(t *testing.T) {
	for _, g := range goldenCases {
		t.Run(g.name, func(t *testing.T) {
			c, err := RenderCells(loadSprite(t, g.sprite), g.cw, g.ch, g.params, g.opt, nil)
			if err != nil {
				t.Fatal(err)
			}
			got := c.Golden()
			path := filepath.Join("testdata", "golden", g.name+".txt")
			if *update {
				os.MkdirAll(filepath.Dir(path), 0o755)
				if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("%v (run go test -update)", err)
			}
			if string(want) != got {
				t.Errorf("cell output changed for %s (run go test -update to accept):\n%s", g.name, firstDiff(string(want), got))
			}
		})
	}
}

func firstDiff(a, b string) string {
	al, bl := strings.Split(a, "\n"), strings.Split(b, "\n")
	for i := 0; i < len(al) && i < len(bl); i++ {
		if al[i] != bl[i] {
			return fmt.Sprintf("line %d:\n  want %q\n  got  %q", i+1, al[i], bl[i])
		}
	}
	return fmt.Sprintf("length differs: %d vs %d lines", len(al), len(bl))
}
