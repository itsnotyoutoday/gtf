// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package cmcpui

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/itsnotyoutoday/gtf/internal/layout"
)

// Rect is a cell rectangle.
type Rect = layout.Rect

func inter(a, b Rect) Rect {
	x0, y0 := max(a.X, b.X), max(a.Y, b.Y)
	x1, y1 := min(a.X+a.W, b.X+b.W), min(a.Y+a.H, b.Y+b.H)
	if x1 <= x0 || y1 <= y0 {
		return Rect{}
	}
	return Rect{X: x0, Y: y0, W: x1 - x0, H: y1 - y0}
}

// canvas draws into a grid inside a clip rectangle.
type canvas struct {
	g    *layout.Grid
	clip Rect
}

func (c *canvas) put(x, y int, r rune, a layout.Attr) {
	if x < c.clip.X || y < c.clip.Y || x >= c.clip.X+c.clip.W || y >= c.clip.Y+c.clip.H {
		return
	}
	c.g.Set(x, y, layout.Cell{R: r, A: a})
}

// text draws s at x,y (at most maxw cells) and returns the cells used.
func (c *canvas) text(x, y int, s string, a layout.Attr, maxw int) int {
	n := 0
	for _, r := range clean(s) {
		if maxw >= 0 && n >= maxw {
			break
		}
		c.put(x+n, y, r, a)
		n++
	}
	return n
}

func (c *canvas) fill(r Rect, ch rune, a layout.Attr) {
	for y := r.Y; y < r.Y+r.H; y++ {
		for x := r.X; x < r.X+r.W; x++ {
			c.put(x, y, ch, a)
		}
	}
}

// clean removes control characters and escape sequences from a server string and replaces
// double-width characters (the grid is one cell per rune); spec 10.3 "strings shown".
func clean(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r < 0x20 || (r >= 0x7f && r < 0xa0):
			continue
		case r == 0xfffd:
			b.WriteRune('?')
		case wide(r):
			b.WriteRune('?')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func wide(r rune) bool {
	return r >= 0x1100 && (r <= 0x115f || (r >= 0x2e80 && r <= 0xa4cf) || (r >= 0xac00 && r <= 0xd7a3) ||
		(r >= 0xf900 && r <= 0xfaff) || (r >= 0xfe30 && r <= 0xfe6f) || (r >= 0xff00 && r <= 0xff60) ||
		(r >= 0xffe0 && r <= 0xffe6) || (r >= 0x1f300 && r <= 0x1faff) || (r >= 0x20000 && r <= 0x3fffd))
}

func runeLen(s string) int { return len([]rune(clean(s))) }

// ---- value helpers: props are JSON-like values ----

func num(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, !math.IsNaN(x)
	case float32:
		return float64(x), true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case uint32:
		return float64(x), true
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(x), 64)
		return f, err == nil
	}
	return 0, false
}

func numOr(v any, d float64) float64 {
	if f, ok := num(v); ok {
		return f
	}
	return d
}

func str(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case bool:
		if x {
			return "yes"
		}
		return "no"
	case float64:
		return fmtNum(x)
	case map[string]any:
		if az, ok := num(x["az"]); ok {
			mk, _ := num(x["mark"])
			return fmt.Sprintf("%03.0f%+.0f", az, mk) // the game's bearing form: 333+22
		}
		if x["name"] != nil {
			return str(x["name"])
		}
		return ""
	case []any:
		p := make([]string, len(x))
		for i, e := range x {
			p[i] = str(e)
		}
		return strings.Join(p, ",")
	}
	return fmt.Sprint(v)
}

func fmtNum(f float64) string {
	if f == math.Trunc(f) && math.Abs(f) < 1e12 {
		return strconv.FormatInt(int64(f), 10)
	}
	return strconv.FormatFloat(f, 'f', 2, 64)
}

func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func asList(v any) []any {
	l, _ := v.([]any)
	return l
}

// sortedKeys returns a map's keys, numerically when they all are numbers.
func sortedKeys(m map[string]any) []string {
	k := make([]string, 0, len(m))
	numeric := true
	for key := range m {
		k = append(k, key)
		if _, err := strconv.ParseFloat(key, 64); err != nil {
			numeric = false
		}
	}
	sort.Slice(k, func(i, j int) bool {
		if numeric {
			a, _ := strconv.ParseFloat(k[i], 64)
			b, _ := strconv.ParseFloat(k[j], 64)
			return a < b
		}
		return k[i] < k[j]
	})
	return k
}

// keyValue is what a select event carries for a row key: a number when the key is one.
func keyValue(k string) any {
	if f, err := strconv.ParseFloat(k, 64); err == nil {
		return f
	}
	return k
}

// ---- bars ----

var eighths = []rune{' ', '▏', '▎', '▍', '▌', '▋', '▊', '▉', '█'}

// barCells draws a fractional bar of w cells: frac 0..1.
func (c *canvas) bar(x, y, w int, frac float64, fill, empty layout.Attr) {
	if w <= 0 {
		return
	}
	frac = math.Max(0, math.Min(1, frac))
	full := frac * float64(w)
	n := int(full)
	part := int((full - float64(n)) * 8)
	for i := 0; i < w; i++ {
		switch {
		case i < n:
			c.put(x+i, y, '█', fill)
		case i == n && part > 0:
			c.put(x+i, y, eighths[part], fill)
		default:
			c.put(x+i, y, '░', empty)
		}
	}
}

// ---- braille ----

type braille struct {
	w, h int // cells
	bits []uint8
	col  []layout.Color
	set  []bool
}

func newBraille(w, h int) *braille {
	return &braille{w: w, h: h, bits: make([]uint8, w*h), col: make([]layout.Color, w*h), set: make([]bool, w*h)}
}

var dotBit = [4][2]uint8{{0x01, 0x08}, {0x02, 0x10}, {0x04, 0x20}, {0x40, 0x80}}

func (b *braille) dot(px, py int, col layout.Color) {
	if px < 0 || py < 0 || px >= b.w*2 || py >= b.h*4 {
		return
	}
	i := (py/4)*b.w + px/2
	b.bits[i] |= dotBit[py%4][px%2]
	b.col[i] = col
	b.set[i] = true
}

func (b *braille) line(x0, y0, x1, y1 int, col layout.Color) {
	dx, dy := abs(x1-x0), -abs(y1-y0)
	sx, sy := 1, 1
	if x0 > x1 {
		sx = -1
	}
	if y0 > y1 {
		sy = -1
	}
	e := dx + dy
	for i := 0; i < 4096; i++ {
		b.dot(x0, y0, col)
		if x0 == x1 && y0 == y1 {
			return
		}
		e2 := 2 * e
		if e2 >= dy {
			e += dy
			x0 += sx
		}
		if e2 <= dx {
			e += dx
			y0 += sy
		}
	}
}

func (b *braille) circle(cx, cy, rx, ry float64, col layout.Color) {
	n := int(math.Max(24, (rx+ry)*3))
	for i := 0; i < n; i++ {
		a := 2 * math.Pi * float64(i) / float64(n)
		b.dot(int(math.Round(cx+rx*math.Cos(a))), int(math.Round(cy+ry*math.Sin(a))), col)
	}
}

func (b *braille) blit(c *canvas, x, y int, base layout.Attr) {
	for j := 0; j < b.h; j++ {
		for i := 0; i < b.w; i++ {
			k := j*b.w + i
			if !b.set[k] {
				continue
			}
			a := base
			a.FG = b.col[k]
			c.put(x+i, y+j, rune(0x2800)+rune(b.bits[k]), a)
		}
	}
}

func abs(a int) int {
	if a < 0 {
		return -a
	}
	return a
}

// FirstKey is the first key of a map in display order, as a select event value.
func FirstKey(m map[string]any) any {
	k := sortedKeys(m)
	if len(k) == 0 {
		return nil
	}
	return keyValue(k[0])
}

// asciiMap is what the ASCII-only mode draws in place of the block, braille and box characters.
var asciiMap = map[rune]rune{
	'█': '#', '▉': '#', '▊': '#', '▋': '#', '▌': '#', '▍': '=', '▎': '=', '▏': '-', '░': '.', '▀': '"', '▄': '_',
	'▁': '_', '▂': '_', '▃': '-', '▅': '=', '▆': '#', '▇': '#',
	'·': '.', '▲': '^', '●': 'o', '◆': '*', '✛': '+', '◀': '<', '▶': '>', '◖': '(', '◗': ')', '─': '-', '│': '|',
	'┌': '+', '┐': '+', '└': '+', '┘': '+', '✸': '*', '╱': '/', '▾': 'v', '◉': 'o', '╰': '\\', '╯': '/',
}

// asciify replaces every non-ASCII character of the grid (the %cmcp_ascii mode, or a terminal
// without UTF-8): braille becomes a dot where it has any, other characters are mapped or '?'.
func asciify(g *layout.Grid) {
	for i, c := range g.Cells {
		switch {
		case c.R < 0x80:
		case c.R >= 0x2800 && c.R <= 0x28ff:
			if c.R == 0x2800 {
				g.Cells[i].R = ' '
			} else {
				g.Cells[i].R = '.'
			}
		default:
			if r, ok := asciiMap[c.R]; ok {
				g.Cells[i].R = r
			} else {
				g.Cells[i].R = '?'
			}
		}
	}
}
