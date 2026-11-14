package ui

import (
	"unicode"

	"github.com/itsnotyoutoday/gtf/internal/layout"
)

// WrapConfig mirrors tf's wrap, wrapsize, wrapspace and wrappunct variables.
type WrapConfig struct {
	Wrap  bool // word-wrap (otherwise hard break)
	Size  int  // wrapsize; 0 = screen width
	Space int  // indent of continuation lines
	Punct int  // wrappunct
}

func cellWidth(c layout.Cell) int {
	if c.R == 0 {
		return 1
	}
	if RuneWidth(c.R) == 0 {
		return 0
	}
	return 1
}

func isSpace(r rune) bool { return r == ' ' || r == '\t' || r == '\n' }
func isPunct(r rune) bool { return r < 128 && unicode.IsPunct(r) || r < 128 && unicode.IsSymbol(r) }

// wraplen returns how many cells of s fit on one physical line of width
// wrap (after indent), following tf's wraplen().
func wraplen(s []layout.Cell, wrap, indent int, cfg WrapConfig) int {
	max := wrap - indent
	if max < 1 {
		max = 1
	}
	visible, total := 0, 0
	for total < len(s) && visible < max {
		visible += cellWidth(s[total])
		total++
	}
	// do not split before a zero-width mark
	for total < len(s) && cellWidth(s[total]) == 0 {
		total++
	}
	if total >= len(s) {
		return len(s)
	}
	n := total
	if cfg.Wrap {
		for n > 0 && !isSpace(s[n-1].R) {
			n--
		}
		if cfg.Punct > 0 && n < total-cfg.Punct {
			n = total
			for n > 0 && !isSpace(s[n-1].R) && !isPunct(s[n-1].R) {
				n--
			}
		}
	}
	if n == 0 {
		return total
	}
	return n
}

// Wrap splits a logical line into physical lines of at most width columns.
func Wrap(s []layout.Cell, width int, cfg WrapConfig) [][]layout.Cell {
	w := width
	if cfg.Size > 0 && cfg.Size < w {
		w = cfg.Size
	}
	if w < 1 {
		w = 1
	}
	if len(s) == 0 {
		return [][]layout.Cell{nil}
	}
	var out [][]layout.Cell
	off := 0
	for {
		indent := 0
		if cfg.Wrap && off > 0 && cfg.Space < w {
			indent = cfg.Space
		}
		n := wraplen(s[off:], w, indent, cfg)
		line := s[off : off+n]
		if indent > 0 {
			pad := make([]layout.Cell, indent, indent+n)
			for i := range pad {
				pad[i] = layout.Cell{R: ' '}
			}
			line = append(pad, line...)
		}
		out = append(out, line)
		off += n
		if off >= len(s) {
			return out
		}
	}
}

// Line is one logical line of output.
type Line struct {
	Cells []layout.Cell
}

// Scrollback holds logical lines and their wrapped physical form at the
// current width. Held lines (more-paging) are the physical lines past Shown.
type Scrollback struct {
	lines []Line
	phys  [][]layout.Cell
	owner []int // logical index of each physical line
	width int
	cfg   WrapConfig
	Max   int // max logical lines kept
	drop  int // logical lines dropped from the front so far
}

// NewScrollback makes a scrollback keeping at most max logical lines.
func NewScrollback(max int) *Scrollback { return &Scrollback{Max: max} }

// Len returns the number of physical lines.
func (s *Scrollback) Len() int { return len(s.phys) }

// Logical returns the number of logical lines.
func (s *Scrollback) Logical() int { return len(s.lines) }

// Phys returns physical line i.
func (s *Scrollback) Phys(i int) []layout.Cell { return s.phys[i] }

// Add appends a logical line and returns how many physical lines it became
// and how many physical lines were dropped from the front.
func (s *Scrollback) Add(cells []layout.Cell) (added, dropped int) {
	s.lines = append(s.lines, Line{cells})
	p := Wrap(cells, s.width, s.cfg)
	for _, l := range p {
		s.phys = append(s.phys, l)
		s.owner = append(s.owner, len(s.lines)-1)
	}
	if s.Max > 0 && len(s.lines) > s.Max+s.Max/4 {
		k := len(s.lines) - s.Max
		cut := 0
		for cut < len(s.owner) && s.owner[cut] < k {
			cut++
		}
		s.lines = append([]Line(nil), s.lines[k:]...)
		s.phys = append([][]layout.Cell(nil), s.phys[cut:]...)
		own := make([]int, len(s.owner)-cut)
		for i := range own {
			own[i] = s.owner[cut+i] - k
		}
		s.owner = own
		s.drop += k
		dropped = cut
	}
	return len(p), dropped
}

// Rewrap recomputes physical lines for a new width/config. It returns a
// mapping function from an old physical index to the new one (by owning
// logical line and offset ratio).
func (s *Scrollback) Rewrap(width int, cfg WrapConfig) (remap func(old int) int) {
	if width == s.width && cfg == s.cfg {
		return func(i int) int { return i }
	}
	oldOwner := append([]int(nil), s.owner...)
	s.width, s.cfg = width, cfg
	s.phys = nil
	s.owner = nil
	first := make([]int, len(s.lines)) // first physical index per logical line
	for i, l := range s.lines {
		first[i] = len(s.phys)
		for _, p := range Wrap(l.Cells, width, cfg) {
			s.phys = append(s.phys, p)
			s.owner = append(s.owner, i)
		}
	}
	return func(old int) int {
		if old < 0 {
			return old
		}
		if old >= len(oldOwner) {
			return len(s.phys) + (old - len(oldOwner))
		}
		// old is an exclusive end; keep everything of its last logical line shown
		if old == 0 {
			return 0
		}
		li := oldOwner[old-1]
		end := len(s.phys)
		if li+1 < len(first) {
			end = first[li+1]
		}
		return end
	}
}

// Text returns the plain text of the logical lines (for /recall-like uses).
func (s *Scrollback) Text(i int) string { return StripCells(s.lines[i].Cells) }
