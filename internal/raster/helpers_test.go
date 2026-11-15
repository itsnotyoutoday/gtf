// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package raster

import (
	"encoding/json"
	"flag"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

var update = flag.Bool("update", false, "rewrite golden files")

func loadSprite(t testing.TB, name string) *Sprite {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	s, err := ParseJSON(b, nil)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return s
}

// cellsImage draws a cell grid the way a terminal would: each cell is cw x ch
// pixels, glyphs are drawn from their block geometry, background is the panel colour.
func cellsImage(c *Cells, cw, ch int, panel color.RGBA) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, c.W*cw, c.H*ch))
	fill := func(x0, y0, x1, y1 int, col color.RGBA) {
		for y := y0; y < y1; y++ {
			for x := x0; x < x1; x++ {
				img.SetRGBA(x, y, col)
			}
		}
	}
	rgba := func(t TermColor, def color.RGBA) color.RGBA {
		if t.Kind == KindDefault {
			return def
		}
		r, g, b := t.RGBValue()
		return color.RGBA{r, g, b, 255}
	}
	for y := 0; y < c.H; y++ {
		for x := 0; x < c.W; x++ {
			ce := c.At(x, y)
			px, py := x*cw, y*ch
			bg := rgba(ce.BG, panel)
			fg := rgba(ce.FG, color.RGBA{230, 230, 230, 255})
			fill(px, py, px+cw, py+ch, bg)
			r := ce.R
			switch {
			case r == ' ':
			case r == '█':
				fill(px, py, px+cw, py+ch, fg)
			case r == '▀':
				fill(px, py, px+cw, py+ch/2, fg)
			case r == '▄':
				fill(px, py+ch/2, px+cw, py+ch, fg)
			case r == '▌':
				fill(px, py, px+cw/2, py+ch, fg)
			case r == '▐':
				fill(px+cw/2, py, px+cw, py+ch, fg)
			case r >= 0x2596 && r <= 0x259F:
				for i := 0; i < 4; i++ {
					if quadrantRunes[1:][0] == 0 {
						break
					}
				}
				for pat, qr := range quadrantRunes {
					if qr == r {
						for i := 0; i < 4; i++ {
							if pat&(1<<i) != 0 {
								qx, qy := i%2, i/2
								fill(px+qx*cw/2, py+qy*ch/2, px+(qx+1)*cw/2, py+(qy+1)*ch/2, fg)
							}
						}
					}
				}
			case r >= 0x1FB00 && r <= 0x1FB3B:
				for pat := 1; pat < 63; pat++ {
					if sextantRune(pat) == r {
						for i := 0; i < 6; i++ {
							if pat&(1<<i) != 0 {
								qx, qy := i%2, i/2
								fill(px+qx*cw/2, py+qy*ch/3, px+(qx+1)*cw/2, py+(qy+1)*ch/3, fg)
							}
						}
					}
				}
			case r >= 0x2800 && r <= 0x28FF:
				bits := int(r - 0x2800)
				for i, d := range brailleDot {
					if bits&d != 0 {
						qx, qy := i%2, i/2
						fill(px+qx*cw/2+1, py+qy*ch/4+1, px+(qx+1)*cw/2-1, py+(qy+1)*ch/4-1, fg)
					}
				}
			default: // ASCII ramp: brightness by density
				for i, rr := range asciiRamp {
					if rr == r {
						v := uint8(i * 255 / (len(asciiRamp) - 1))
						col := color.RGBA{uint8(int(fg.R) * int(v) / 255), uint8(int(fg.G) * int(v) / 255), uint8(int(fg.B) * int(v) / 255), 255}
						fill(px+1, py+1, px+cw-1, py+ch-1, col)
					}
				}
			}
		}
	}
	return img
}

func writePNG(t testing.TB, path string, img image.Image) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}

func formatFloat(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }

func jsonMarshal(v any) ([]byte, error) { return json.Marshal(v) }
