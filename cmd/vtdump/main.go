// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

// Command vtdump renders a captured terminal byte stream through the vt
// emulator (developer tool): vtdump [-w 80] [-h 24] [-png out.png] file
// With -png it also writes a picture of the screen with colours, half blocks,
// Braille dots and text, for checking what a layout really looks like.
package main

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"strconv"
	"strings"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"

	"github.com/itsnotyoutoday/gtf/internal/ui/vt"
)

func main() {
	w := flag.Int("w", 80, "columns")
	h := flag.Int("h", 24, "rows")
	out := flag.String("png", "", "also write a PNG picture of the screen")
	flag.Parse()
	b, err := os.ReadFile(flag.Arg(0))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	s := vt.New(*w, *h)
	s.Write(b)
	fmt.Print(s.Dump())
	if *out != "" {
		if err := writePNG(s, *out); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
}

const cw, ch = 9, 18

var ansi16 = [16]color.RGBA{{0, 0, 0, 255}, {205, 0, 0, 255}, {0, 205, 0, 255}, {205, 205, 0, 255}, {80, 80, 238, 255}, {205, 0, 205, 255}, {0, 205, 205, 255}, {229, 229, 229, 255},
	{127, 127, 127, 255}, {255, 0, 0, 255}, {0, 255, 0, 255}, {255, 255, 0, 255}, {120, 120, 255, 255}, {255, 0, 255, 255}, {0, 255, 255, 255}, {255, 255, 255, 255}}

func xterm(n int) color.RGBA {
	switch {
	case n < 16:
		return ansi16[n]
	case n < 232:
		n -= 16
		lv := func(i int) uint8 {
			if i == 0 {
				return 0
			}
			return uint8(55 + 40*i)
		}
		return color.RGBA{lv(n / 36), lv(n / 6 % 6), lv(n % 6), 255}
	}
	g := uint8(8 + 10*(n-232))
	return color.RGBA{g, g, g, 255}
}

type style struct {
	fg, bg color.RGBA
	rev    bool
}

func parseAttr(a string) style {
	def := style{fg: ansi16[7], bg: color.RGBA{0, 0, 0, 255}}
	st := def
	for _, f := range strings.Fields(a) {
		if k := strings.IndexByte(f, '='); k > 0 {
			tag, val := f[:k], f[k+1:]
			var c color.RGBA
			if p := strings.Split(val, "/"); len(p) == 3 {
				r, _ := strconv.Atoi(p[0])
				g, _ := strconv.Atoi(p[1])
				b, _ := strconv.Atoi(p[2])
				c = color.RGBA{uint8(r), uint8(g), uint8(b), 255}
			} else {
				n, _ := strconv.Atoi(val)
				c = xterm(n)
			}
			if tag == "fg" {
				st.fg = c
			} else {
				st.bg = c
			}
			continue
		}
		switch k, _ := strconv.Atoi(f); {
		case k == 7:
			st.rev = true
		case k >= 30 && k <= 37:
			st.fg = ansi16[k-30]
		case k >= 90 && k <= 97:
			st.fg = ansi16[k-90+8]
		case k >= 40 && k <= 47:
			st.bg = ansi16[k-40]
		case k >= 100 && k <= 107:
			st.bg = ansi16[k-100+8]
		}
	}
	if st.rev {
		st.fg, st.bg = st.bg, st.fg
	}
	return st
}

func fill(img *image.RGBA, x0, y0, x1, y1 int, c color.RGBA) {
	draw.Draw(img, image.Rect(x0, y0, x1, y1), &image.Uniform{c}, image.Point{}, draw.Src)
}

// quadrantRunes is indexed by which quadrants are filled (bit 0 upper left, 1 upper right, 2 lower left, 3 lower right).
var quadrantRunes = [16]rune{' ', '▘', '▝', '▀', '▖', '▌', '▞', '▛', '▗', '▚', '▐', '▜', '▄', '▙', '▟', '█'}

// blockBits returns the fill pattern of a quadrant block, or -1 for any other rune.
func blockBits(r rune, table []rune) int {
	if r < 0x2596 || r > 0x259f {
		return -1
	}
	for i, q := range table {
		if q == r {
			return i
		}
	}
	return -1
}

// sextantBits is the 2x3 fill pattern of U+1FB00..U+1FB3B (the table skips the left and right halves).
func sextantBits(r rune) int {
	p := int(r-0x1fb00) + 1
	if p >= 21 {
		p++
	}
	if p >= 42 {
		p++
	}
	return p
}

func writePNG(s *vt.Screen, path string) error {
	img := image.NewRGBA(image.Rect(0, 0, s.W*cw, s.H*ch))
	fill(img, 0, 0, s.W*cw, s.H*ch, color.RGBA{0, 0, 0, 255})
	face := basicfont.Face7x13
	for y := 0; y < s.H; y++ {
		for x := 0; x < s.W; x++ {
			c := s.CellAt(x, y)
			st := parseAttr(c.Attr)
			px, py := x*cw, y*ch
			fill(img, px, py, px+cw, py+ch, st.bg)
			r := c.R
			switch {
			case r == 0 || r == ' ':
			case r == '█':
				fill(img, px, py, px+cw, py+ch, st.fg)
			case r == '▀':
				fill(img, px, py, px+cw, py+ch/2, st.fg)
			case r == '▄':
				fill(img, px, py+ch/2, px+cw, py+ch, st.fg)
			case r == '▌':
				fill(img, px, py, px+cw/2, py+ch, st.fg)
			case r == '▐':
				fill(img, px+cw/2, py, px+cw, py+ch, st.fg)
			case blockBits(r, quadrantRunes[:]) >= 0:
				bits := blockBits(r, quadrantRunes[:])
				for i := 0; i < 4; i++ {
					if bits&(1<<i) != 0 {
						qx, qy := i%2, i/2
						fill(img, px+qx*cw/2, py+qy*ch/2, px+(qx+1)*cw/2, py+(qy+1)*ch/2, st.fg)
					}
				}
			case r >= 0x1fb00 && r <= 0x1fb3b:
				bits := sextantBits(r)
				for i := 0; i < 6; i++ {
					if bits&(1<<i) != 0 {
						qx, qy := i%2, i/2
						fill(img, px+qx*cw/2, py+qy*ch/3, px+(qx+1)*cw/2, py+(qy+1)*ch/3, st.fg)
					}
				}
			case r >= 0x2800 && r <= 0x28ff:
				bits := int(r - 0x2800)
				pos := [8][2]int{{0, 0}, {0, 1}, {0, 2}, {1, 0}, {1, 1}, {1, 2}, {0, 3}, {1, 3}}
				for i, p := range pos {
					if bits&(1<<i) != 0 {
						dx, dy := px+1+p[0]*4, py+1+p[1]*4
						fill(img, dx, dy, dx+3, dy+3, st.fg)
					}
				}
			case r >= 0x2500 && r <= 0x259f:
				fill(img, px+cw/2-1, py, px+cw/2+1, py+ch, st.fg)
				fill(img, px, py+ch/2-1, px+cw, py+ch/2+1, st.fg)
			default:
				d := &font.Drawer{Dst: img, Src: &image.Uniform{st.fg}, Face: face, Dot: fixed.P(px+1, py+ch-4)}
				d.DrawString(string(r))
			}
		}
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}
