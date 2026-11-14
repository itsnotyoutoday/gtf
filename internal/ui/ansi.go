package ui

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/itsnotyoutoday/gtf/internal/layout"
)

// AnsiParser converts text containing ANSI SGR sequences into styled cells.
// The current attribute carries across lines, as a terminal's would. Tabs
// are expanded and other control characters dropped.
type AnsiParser struct {
	Cur     layout.Attr
	TabSize int
	Bells   int // BELs seen since last reset
}

// Parse converts one logical line (without its newline).
func (p *AnsiParser) Parse(s string) []layout.Cell {
	tab := p.TabSize
	if tab <= 0 {
		tab = 8
	}
	out := make([]layout.Cell, 0, len(s))
	col := 0
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == 0x1b:
			i++
			if i < len(s) && s[i] == '[' {
				j := i + 1
				for j < len(s) && (s[j] < 0x40 || s[j] > 0x7e) {
					j++
				}
				if j < len(s) {
					if s[j] == 'm' {
						p.sgr(s[i+1 : j])
					}
					i = j + 1
				} else {
					i = j
				}
			} else if i < len(s) && s[i] == ']' { // OSC: skip to BEL or ST
				for i < len(s) && s[i] != 7 && !(s[i] == 0x1b && i+1 < len(s) && s[i+1] == '\\') {
					i++
				}
				if i < len(s) && s[i] == 7 {
					i++
				} else {
					i += 2
				}
			} else if i < len(s) {
				i++
			}
		case c == '\t':
			n := tab - col%tab
			for k := 0; k < n; k++ {
				out = append(out, layout.Cell{R: ' ', A: p.Cur})
			}
			col += n
			i++
		case c == 7:
			p.Bells++
			i++
		case c < 0x20 || c == 0x7f:
			i++
		default:
			r, n := utf8.DecodeRuneInString(s[i:])
			if r == utf8.RuneError && n == 1 {
				r = rune(c)
			}
			i += n
			w := RuneWidth(r)
			if w == 0 {
				if len(out) > 0 {
					// combining mark: keep with previous cell by appending as its own zero-width cell
					out = append(out, layout.Cell{R: r, A: p.Cur})
				}
				continue
			}
			out = append(out, layout.Cell{R: r, A: p.Cur})
			col += w
			if w == 2 {
				out = append(out, layout.Cell{R: 0, A: p.Cur}) // placeholder for the right half
			}
		}
	}
	return out
}

func (p *AnsiParser) sgr(params string) {
	if params == "" {
		p.Cur = layout.Attr{}
		return
	}
	f := strings.Split(params, ";")
	n := make([]int, len(f))
	for i, s := range f {
		n[i], _ = strconv.Atoi(s)
	}
	a := &p.Cur
	for i := 0; i < len(n); i++ {
		v := n[i]
		switch {
		case v == 0:
			*a = layout.Attr{}
		case v == 1:
			a.Bold = true
		case v == 2:
			a.Dim = true
		case v == 3:
			a.Italic = true
		case v == 4:
			a.Underline = true
		case v == 7:
			a.Reverse = true
		case v == 22:
			a.Bold, a.Dim = false, false
		case v == 23:
			a.Italic = false
		case v == 24:
			a.Underline = false
		case v == 27:
			a.Reverse = false
		case v >= 30 && v <= 37:
			a.FG = layout.Color{Kind: layout.ANSI, N: uint8(v - 30)}
		case v >= 90 && v <= 97:
			a.FG = layout.Color{Kind: layout.ANSI, N: uint8(v - 90 + 8)}
		case v >= 40 && v <= 47:
			a.BG = layout.Color{Kind: layout.ANSI, N: uint8(v - 40)}
		case v >= 100 && v <= 107:
			a.BG = layout.Color{Kind: layout.ANSI, N: uint8(v - 100 + 8)}
		case v == 39:
			a.FG = layout.Color{}
		case v == 49:
			a.BG = layout.Color{}
		case v == 38 || v == 48:
			var c layout.Color
			if i+2 < len(n) && n[i+1] == 5 {
				c = layout.Color{Kind: layout.Indexed, N: uint8(n[i+2])}
				i += 2
			} else if i+4 < len(n) && n[i+1] == 2 {
				c = layout.Color{Kind: layout.RGB, R: uint8(n[i+2]), G: uint8(n[i+3]), B: uint8(n[i+4])}
				i += 4
			} else {
				continue
			}
			if v == 38 {
				a.FG = c
			} else {
				a.BG = c
			}
		}
	}
}

// RuneWidth is the number of terminal columns a rune occupies (0, 1 or 2).
func RuneWidth(r rune) int {
	switch {
	case r == 0:
		return 0
	case r < 0x300:
		return 1
	case unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) || r == 0x200b || r == 0x200d || (r >= 0xfe00 && r <= 0xfe0f):
		return 0
	case (r >= 0x1100 && r <= 0x115f) || (r >= 0x2e80 && r <= 0xa4cf) || (r >= 0xac00 && r <= 0xd7a3) ||
		(r >= 0xf900 && r <= 0xfaff) || (r >= 0xfe30 && r <= 0xfe6f) || (r >= 0xff00 && r <= 0xff60) ||
		(r >= 0xffe0 && r <= 0xffe6) || (r >= 0x1f300 && r <= 0x1f64f) || (r >= 0x1f900 && r <= 0x1f9ff) ||
		(r >= 0x20000 && r <= 0x3fffd):
		return 2
	}
	return 1
}

// StripCells returns the plain text of cells (wide placeholders removed).
func StripCells(cs []layout.Cell) string {
	var b strings.Builder
	for _, c := range cs {
		if c.R != 0 {
			b.WriteRune(c.R)
		}
	}
	return b.String()
}

// PlainCells builds unstyled cells from text (no ANSI interpretation).
func PlainCells(s string, a layout.Attr) []layout.Cell {
	out := make([]layout.Cell, 0, len(s))
	for _, r := range s {
		out = append(out, layout.Cell{R: r, A: a})
		if RuneWidth(r) == 2 {
			out = append(out, layout.Cell{R: 0, A: a})
		}
	}
	return out
}
