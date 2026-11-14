// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package ui

import (
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

// line-break classes (a small subset of UAX #14, enough for tf's wrapping)
const (
	lbAL = iota // letters, symbols: no break between them
	lbNU
	lbSP
	lbOP // ( [ { : no break after
	lbCP // ) ] : no break before, none before letters
	lbCL // } , etc: no break before
	lbIS // . , : ; no break before; none before letters/digits
	lbSY // / : no break before; break after unless a digit follows
	lbEX // ! ? : no break before
	lbHY // - : no break before; break after unless a digit follows
	lbQU // quotes: no break either side
	lbID // ideographs: break allowed anywhere
)

func lbClass(r rune) int {
	switch {
	case r == ' ' || r == '\t':
		return lbSP
	case r >= '0' && r <= '9':
		return lbNU
	case r == '(' || r == '[' || r == '{':
		return lbOP
	case r == ')' || r == ']':
		return lbCP
	case r == '}':
		return lbCL
	case r == '.' || r == ',' || r == ':' || r == ';':
		return lbIS
	case r == '/':
		return lbSY
	case r == '!' || r == '?':
		return lbEX
	case r == '-':
		return lbHY
	case r == '"' || r == '\'' || r == '`':
		return lbQU
	case RuneWidth(r) == 2 && r >= 0x2e80:
		return lbID
	}
	return lbAL
}

// canBreakBefore reports whether a line may break between s[i-1] and s[i].
func canBreakBefore(s []layout.Cell, i int) bool {
	if i <= 0 || i >= len(s) {
		return i == len(s)
	}
	if s[i].R == 0 || cellWidth(s[i]) == 0 {
		return false // inside a wide character or after a base: keep marks attached
	}
	p, c := lbClass(s[i-1].R), lbClass(s[i].R)
	if s[i-1].R == 0 { // placeholder: look at the base character
		for k := i - 1; k >= 0; k-- {
			if s[k].R != 0 {
				p = lbClass(s[k].R)
				break
			}
		}
	}
	switch {
	case c == lbSP || c == lbCP || c == lbCL || c == lbIS || c == lbSY || c == lbEX:
		return false // LB13: never before these, even after spaces
	case c == lbHY:
		return p == lbSP // LB18 (break after spaces) precedes LB21 (no break before a hyphen)
	case p == lbSP:
		return true
	case p == lbOP || c == lbQU || p == lbQU:
		return false
	case p == lbCP && (c == lbAL || c == lbNU):
		return false
	case p == lbIS && (c == lbAL || c == lbNU):
		return false
	case (p == lbSY || p == lbHY) && c == lbNU:
		return false
	case p == lbHY && (i < 2 || lbClass(s[i-2].R) == lbSP):
		return false // ICU keeps an option-style "-x" together: no break after a hyphen that starts a word
	case (p == lbAL || p == lbNU) && (c == lbAL || c == lbNU || c == lbOP):
		return false
	}
	return true
}

// wraplen returns how many cells of s fit on one physical line of width
// wrap (after indent), following tf's wraplen() in its Unicode build: the
// line breaks at the last line-break opportunity strictly before the first
// cell that does not fit, or one cell early when there is none.
func wraplen(s []layout.Cell, wrap, indent int, cfg WrapConfig) int {
	max := wrap - indent
	if max < 1 {
		max = 1
	}
	visible, i := 0, 0
	for i < len(s) && visible < max {
		visible += cellWidth(s[i])
		i++
	}
	if i >= len(s) {
		return len(s)
	}
	for i < len(s) && cellWidth(s[i]) == 0 {
		i++ // marks stay with their base
	}
	if i >= len(s) {
		return len(s)
	}
	if !cfg.Wrap {
		return i
	}
	for b := i - 1; b > 0; b-- {
		if canBreakBefore(s, b) {
			return b
		}
	}
	// no break opportunity: break before the previous character
	n := i - 1
	for n > 0 && (cellWidth(s[n]) == 0 || s[n].R == 0) {
		n--
	}
	if n <= 0 {
		return i
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
	// Keep, if set, decides which logical lines are shown (/limit).
	Keep  func([]layout.Cell) bool
	dirty bool
}

// Invalidate forces the next Rewrap to rebuild (the Keep filter changed).
func (s *Scrollback) Invalidate() { s.dirty = true }

// NewScrollback makes a scrollback keeping at most max logical lines.
func NewScrollback(max int) *Scrollback { return &Scrollback{Max: max} }

// Len returns the number of physical lines.
func (s *Scrollback) Len() int { return len(s.phys) }

// Logical returns the number of logical lines.
func (s *Scrollback) Logical() int { return len(s.lines) }

// Cells returns logical line i (0 <= i < Logical()).
func (s *Scrollback) Cells(i int) []layout.Cell { return s.lines[i].Cells }

// Phys returns physical line i.
func (s *Scrollback) Phys(i int) []layout.Cell { return s.phys[i] }

// Add appends a logical line and returns how many physical lines it became
// and how many physical lines were dropped from the front.
func (s *Scrollback) Add(cells []layout.Cell) (added, dropped int) {
	s.lines = append(s.lines, Line{cells})
	var p [][]layout.Cell
	if s.Keep == nil || s.Keep(cells) {
		p = Wrap(cells, s.width, s.cfg)
	}
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
	if width == s.width && cfg == s.cfg && !s.dirty {
		return func(i int) int { return i }
	}
	s.dirty = false
	oldOwner := append([]int(nil), s.owner...)
	s.width, s.cfg = width, cfg
	s.phys = nil
	s.owner = nil
	first := make([]int, len(s.lines)) // first physical index per logical line
	for i, l := range s.lines {
		first[i] = len(s.phys)
		if s.Keep != nil && !s.Keep(l.Cells) {
			continue
		}
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

// InsertAt puts a logical line before logical line idx (idx >= Logical appends).
// It returns the physical index where its first physical line landed and how
// many physical lines it became (0 when the /limit filter hides it).
func (s *Scrollback) InsertAt(idx int, cells []layout.Cell) (at, k int) {
	idx = min(max(idx, 0), len(s.lines))
	at = len(s.phys)
	for i, o := range s.owner {
		if o >= idx {
			at = i
			break
		}
	}
	s.lines = append(s.lines, Line{})
	copy(s.lines[idx+1:], s.lines[idx:])
	s.lines[idx] = Line{cells}
	before := len(s.phys)
	s.dirty = true
	s.Rewrap(s.width, s.cfg)
	return at, len(s.phys) - before
}

// Relayout rebuilds the physical lines at the current width (after Keep changed).
func (s *Scrollback) Relayout() { s.dirty = true; s.Rewrap(s.width, s.cfg) }

// Text returns the plain text of the logical lines (for /recall-like uses).
func (s *Scrollback) Text(i int) string { return StripCells(s.lines[i].Cells) }
