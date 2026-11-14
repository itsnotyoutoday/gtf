// Package layout is the panel/layout system: a small template language with
// data bindings, widgets (bars, gauges, sparklines, lists), and a layout
// engine that docks panels around the main scrollback region. It performs no
// terminal I/O: everything it produces is a grid of styled cells.
package layout

import (
	"fmt"
	"strconv"
	"strings"
)

// ColorKind selects how a Color is interpreted.
type ColorKind uint8

const (
	Default ColorKind = iota
	ANSI              // N = 0..15
	Indexed           // N = 0..255
	RGB
)

// Color is a terminal color.
type Color struct {
	Kind    ColorKind
	N       uint8
	R, G, B uint8
}

// Attr is a text style.
type Attr struct {
	FG, BG                                Color
	Bold, Dim, Italic, Underline, Reverse bool
}

// Cell is one styled character.
type Cell struct {
	R rune
	A Attr
}

// Grid is a w x h rectangle of cells (row-major).
type Grid struct {
	W, H  int
	Cells []Cell
}

// NewGrid makes a blank grid.
func NewGrid(w, h int) *Grid {
	g := &Grid{W: w, H: h, Cells: make([]Cell, w*h)}
	for i := range g.Cells {
		g.Cells[i].R = ' '
	}
	return g
}

// At returns the cell at x,y.
func (g *Grid) At(x, y int) Cell { return g.Cells[y*g.W+x] }

// Set stores a cell if in range.
func (g *Grid) Set(x, y int, c Cell) {
	if x >= 0 && y >= 0 && x < g.W && y < g.H {
		g.Cells[y*g.W+x] = c
	}
}

// Row returns row y as plain text (trailing blanks kept).
func (g *Grid) Row(y int) string {
	var b strings.Builder
	for x := 0; x < g.W; x++ {
		b.WriteRune(g.At(x, y).R)
	}
	return b.String()
}

// Text returns all rows joined by newlines.
func (g *Grid) Text() string {
	rows := make([]string, g.H)
	for y := range rows {
		rows[y] = strings.TrimRight(g.Row(y), " ")
	}
	return strings.Join(rows, "\n")
}

var colorNames = map[string]uint8{
	"black": 0, "red": 1, "green": 2, "yellow": 3, "blue": 4, "magenta": 5, "cyan": 6, "white": 7,
	"gray": 8, "grey": 8, "brred": 9, "brgreen": 10, "bryellow": 11, "brblue": 12, "brmagenta": 13, "brcyan": 14, "brwhite": 15,
	"orange": 208,
}

// ParseColor understands names (red, brgreen, gray), "#rrggbb", "rgb:r,g,b",
// "ansi:N", "256:N" and a bare number 0-255.
func ParseColor(s string) (Color, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	switch {
	case s == "" || s == "default":
		return Color{}, nil
	case strings.HasPrefix(s, "#") && len(s) == 7:
		v, err := strconv.ParseUint(s[1:], 16, 32)
		if err != nil {
			return Color{}, fmt.Errorf("bad color %q", s)
		}
		return Color{Kind: RGB, R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v)}, nil
	case strings.HasPrefix(s, "rgb:"):
		p := strings.Split(s[4:], ",")
		if len(p) == 3 {
			var c Color
			c.Kind = RGB
			for i, d := range []*uint8{&c.R, &c.G, &c.B} {
				n, err := strconv.Atoi(strings.TrimSpace(p[i]))
				if err != nil || n < 0 || n > 255 {
					return Color{}, fmt.Errorf("bad color %q", s)
				}
				*d = uint8(n)
			}
			return c, nil
		}
	case strings.HasPrefix(s, "ansi:"), strings.HasPrefix(s, "256:"):
		n, err := strconv.Atoi(s[strings.IndexByte(s, ':')+1:])
		if err == nil && n >= 0 && n < 256 {
			if strings.HasPrefix(s, "ansi:") && n < 16 {
				return Color{Kind: ANSI, N: uint8(n)}, nil
			}
			return Color{Kind: Indexed, N: uint8(n)}, nil
		}
	}
	if n, ok := colorNames[s]; ok {
		if n < 16 {
			return Color{Kind: ANSI, N: n}, nil
		}
		return Color{Kind: Indexed, N: n}, nil
	}
	if n, err := strconv.Atoi(s); err == nil && n >= 0 && n < 256 {
		return Color{Kind: Indexed, N: uint8(n)}, nil
	}
	return Color{}, fmt.Errorf("unknown color %q", s)
}

// Depth is a terminal's color capability.
type Depth int

const (
	Depth16   Depth = 16
	Depth256  Depth = 256
	DepthTrue Depth = 1 << 24
)

// Quantize reduces a color to what the terminal can show.
func (c Color) Quantize(d Depth) Color {
	switch c.Kind {
	case RGB:
		if d >= DepthTrue {
			return c
		}
		idx := rgbTo256(c.R, c.G, c.B)
		if d >= Depth256 {
			return Color{Kind: Indexed, N: idx}
		}
		return Color{Kind: ANSI, N: indexTo16(idx)}
	case Indexed:
		if d >= Depth256 || c.N < 16 {
			return c
		}
		return Color{Kind: ANSI, N: indexTo16(c.N)}
	}
	return c
}

func rgbTo256(r, g, b uint8) uint8 {
	if r == g && g == b {
		if r < 8 {
			return 16
		}
		if r > 248 {
			return 231
		}
		return uint8(232 + (int(r)-8)*24/247)
	}
	q := func(v uint8) int { return (int(v)*5 + 127) / 255 }
	return uint8(16 + 36*q(r) + 6*q(g) + q(b))
}

func indexTo16(n uint8) uint8 {
	if n < 16 {
		return n
	}
	if n >= 232 {
		if n < 244 {
			return 0
		}
		return 7
	}
	n -= 16
	r, g, b := int(n/36), int(n/6%6), int(n%6)
	idx := uint8(0)
	if r >= 3 {
		idx |= 1
	}
	if g >= 3 {
		idx |= 2
	}
	if b >= 3 {
		idx |= 4
	}
	if r > 3 || g > 3 || b > 3 {
		idx |= 8
	}
	if idx == 0 && (r > 0 || g > 0 || b > 0) {
		idx = 8
	}
	return idx
}

// SGR returns the escape sequence selecting a (always starting from reset).
func (a Attr) SGR(d Depth) string {
	p := []string{"0"}
	if a.Bold {
		p = append(p, "1")
	}
	if a.Dim {
		p = append(p, "2")
	}
	if a.Italic {
		p = append(p, "3")
	}
	if a.Underline {
		p = append(p, "4")
	}
	if a.Reverse {
		p = append(p, "7")
	}
	p = append(p, colorSGR(a.FG.Quantize(d), false)...)
	p = append(p, colorSGR(a.BG.Quantize(d), true)...)
	return "\x1b[" + strings.Join(p, ";") + "m"
}

func colorSGR(c Color, bg bool) []string {
	base := 30
	if bg {
		base = 40
	}
	switch c.Kind {
	case ANSI:
		if c.N < 8 {
			return []string{strconv.Itoa(base + int(c.N))}
		}
		return []string{strconv.Itoa(base + 60 + int(c.N-8))}
	case Indexed:
		return []string{strconv.Itoa(base + 8), "5", strconv.Itoa(int(c.N))}
	case RGB:
		return []string{strconv.Itoa(base + 8), "2", strconv.Itoa(int(c.R)), strconv.Itoa(int(c.G)), strconv.Itoa(int(c.B))}
	}
	return nil
}
