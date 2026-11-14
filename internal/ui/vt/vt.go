// Package vt is a small VT100/ANSI screen model used to test the terminal UI
// (and to compare it with the real tf). It understands the subset of control
// sequences that tf and gtf emit: cursor movement, erase, scroll regions,
// SGR attributes, insert/delete line, and the usual C0 controls.
package vt

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

// Cell is one screen position.
type Cell struct {
	R    rune
	Attr string // normalized SGR parameters ("" = default)
}

// Screen is an emulated terminal.
type Screen struct {
	W, H           int
	cells          [][]Cell
	X, Y           int // cursor, 0-based
	top, bot       int // scroll region, 0-based inclusive
	attr           string
	wrapPending    bool
	savedX, savedY int
	state          int
	csi            []byte
	pend           []byte // incomplete UTF-8
	Bell           int
	AppCursor      bool
	Title          string
}

const (
	stGround = iota
	stEsc
	stCSI
	stOSC
	stCharset
)

// New makes a blank w x h screen.
func New(w, h int) *Screen {
	s := &Screen{W: w, H: h, top: 0, bot: h - 1}
	s.cells = make([][]Cell, h)
	for i := range s.cells {
		s.cells[i] = s.blankRow()
	}
	return s
}

func (s *Screen) blankRow() []Cell {
	r := make([]Cell, s.W)
	for i := range r {
		r[i] = Cell{R: ' '}
	}
	return r
}

// Resize changes the screen size, keeping the top-left contents.
func (s *Screen) Resize(w, h int) {
	n := New(w, h)
	for y := 0; y < h && y < s.H; y++ {
		for x := 0; x < w && x < s.W; x++ {
			n.cells[y][x] = s.cells[y][x]
		}
	}
	n.X, n.Y = min(s.X, w-1), min(s.Y, h-1)
	n.attr = s.attr
	*s = *n
}

// Write feeds terminal output.
func (s *Screen) Write(p []byte) (int, error) {
	n0 := len(p)
	if len(s.pend) > 0 {
		p = append(s.pend, p...)
		s.pend = nil
	}
	for i := 0; i < len(p); {
		c := p[i]
		if s.state == stGround && c >= 0x80 {
			if !utf8.FullRune(p[i:]) {
				s.pend = append([]byte(nil), p[i:]...)
				break
			}
			r, n := utf8.DecodeRune(p[i:])
			s.put(r)
			i += n
			continue
		}
		i++
		s.feed(c)
	}
	return n0, nil
}

func (s *Screen) feed(c byte) {
	switch s.state {
	case stGround:
		switch {
		case c == 0x1b:
			s.state = stEsc
		case c == '\r':
			s.X, s.wrapPending = 0, false
		case c == '\n', c == 0x0b, c == 0x0c:
			s.index()
		case c == '\b':
			if s.X > 0 {
				s.X--
			}
			s.wrapPending = false
		case c == '\t':
			s.X = min((s.X/8+1)*8, s.W-1)
			s.wrapPending = false
		case c == 7:
			s.Bell++
		case c >= 0x20 && c < 0x7f:
			s.put(rune(c))
		}
	case stEsc:
		s.state = stGround
		switch c {
		case '[':
			s.state, s.csi = stCSI, s.csi[:0]
		case ']':
			s.state, s.csi = stOSC, s.csi[:0]
		case '(', ')', '*', '+', '#':
			s.state = stCharset
		case 'M':
			s.reverseIndex()
		case 'D':
			s.index()
		case 'E':
			s.X = 0
			s.index()
		case '7':
			s.savedX, s.savedY = s.X, s.Y
		case '8':
			s.X, s.Y = s.savedX, s.savedY
		case 'c':
			*s = *New(s.W, s.H)
		}
	case stCharset:
		s.state = stGround
	case stOSC:
		if c == 7 {
			s.Title = string(s.csi)
			s.state = stGround
		} else if c == 0x1b {
			s.Title = string(s.csi)
			s.state = stCharset // swallow the "\" of ST
		} else {
			s.csi = append(s.csi, c)
		}
	case stCSI:
		if c >= 0x40 && c <= 0x7e {
			s.doCSI(c, string(s.csi))
			s.state = stGround
		} else {
			s.csi = append(s.csi, c)
		}
	}
}

func (s *Screen) put(r rune) {
	if s.wrapPending {
		s.X = 0
		s.index()
		s.wrapPending = false
	}
	s.cells[s.Y][s.X] = Cell{R: r, Attr: s.attr}
	if s.X == s.W-1 {
		s.wrapPending = true
	} else {
		s.X++
	}
}

func (s *Screen) index() {
	if s.Y == s.bot {
		s.scrollUp(s.top, s.bot, 1)
	} else if s.Y < s.H-1 {
		s.Y++
	}
}

func (s *Screen) reverseIndex() {
	if s.Y == s.top {
		s.scrollDown(s.top, s.bot, 1)
	} else if s.Y > 0 {
		s.Y--
	}
}

func (s *Screen) scrollUp(top, bot, n int) {
	for ; n > 0; n-- {
		copy(s.cells[top:bot], s.cells[top+1:bot+1])
		s.cells[bot] = s.blankRow()
	}
}

func (s *Screen) scrollDown(top, bot, n int) {
	for ; n > 0; n-- {
		copy(s.cells[top+1:bot+1], s.cells[top:bot])
		s.cells[top] = s.blankRow()
	}
}

func params(p string) []int {
	p = strings.TrimLeft(p, "?>=!")
	if p == "" {
		return []int{0}
	}
	var out []int
	for _, f := range strings.Split(p, ";") {
		n, err := strconv.Atoi(f)
		if err != nil {
			n = 0
		}
		out = append(out, n)
	}
	return out
}

func (s *Screen) doCSI(c byte, p string) {
	priv := strings.HasPrefix(p, "?")
	a := params(p)
	n := func(i, def int) int {
		if i < len(a) && a[i] > 0 {
			return a[i]
		}
		return def
	}
	s.wrapPending = false
	switch c {
	case 'A':
		s.Y = max(s.Y-n(0, 1), 0)
	case 'B', 'e':
		s.Y = min(s.Y+n(0, 1), s.H-1)
	case 'C', 'a':
		s.X = min(s.X+n(0, 1), s.W-1)
	case 'D':
		s.X = max(s.X-n(0, 1), 0)
	case 'E':
		s.Y, s.X = min(s.Y+n(0, 1), s.H-1), 0
	case 'F':
		s.Y, s.X = max(s.Y-n(0, 1), 0), 0
	case 'G', '`':
		s.X = min(n(0, 1)-1, s.W-1)
	case 'd':
		s.Y = min(n(0, 1)-1, s.H-1)
	case 'H', 'f':
		s.Y, s.X = min(n(0, 1)-1, s.H-1), min(n(1, 1)-1, s.W-1)
	case 'J':
		switch n(0, 0) {
		case 0:
			s.clear(s.Y, s.X, s.W)
			for y := s.Y + 1; y < s.H; y++ {
				s.clear(y, 0, s.W)
			}
		case 1:
			for y := 0; y < s.Y; y++ {
				s.clear(y, 0, s.W)
			}
			s.clear(s.Y, 0, s.X+1)
		default:
			for y := 0; y < s.H; y++ {
				s.clear(y, 0, s.W)
			}
		}
	case 'K':
		switch n(0, 0) {
		case 0:
			s.clear(s.Y, s.X, s.W)
		case 1:
			s.clear(s.Y, 0, s.X+1)
		default:
			s.clear(s.Y, 0, s.W)
		}
	case 'L':
		if s.Y >= s.top && s.Y <= s.bot {
			s.scrollDown(s.Y, s.bot, n(0, 1))
		}
	case 'M':
		if s.Y >= s.top && s.Y <= s.bot {
			s.scrollUp(s.Y, s.bot, n(0, 1))
		}
	case 'P':
		k := min(n(0, 1), s.W-s.X)
		row := s.cells[s.Y]
		copy(row[s.X:], row[s.X+k:])
		for i := s.W - k; i < s.W; i++ {
			row[i] = Cell{R: ' '}
		}
	case '@':
		k := min(n(0, 1), s.W-s.X)
		row := s.cells[s.Y]
		copy(row[s.X+k:], row[s.X:s.W-k])
		for i := s.X; i < s.X+k; i++ {
			row[i] = Cell{R: ' '}
		}
	case 'X':
		s.clear(s.Y, s.X, min(s.X+n(0, 1), s.W))
	case 'S':
		s.scrollUp(s.top, s.bot, n(0, 1))
	case 'T':
		s.scrollDown(s.top, s.bot, n(0, 1))
	case 'r':
		t, b := n(0, 1)-1, n(1, s.H)-1
		if t < b && b < s.H {
			s.top, s.bot = t, b
		}
		s.X, s.Y = 0, 0
	case 'm':
		s.sgr(a)
	case 's':
		s.savedX, s.savedY = s.X, s.Y
	case 'u':
		s.X, s.Y = s.savedX, s.savedY
	case 'h', 'l':
		if priv {
			for _, m := range a {
				if m == 1 {
					s.AppCursor = c == 'h'
				}
			}
		}
	}
}

func (s *Screen) clear(y, x0, x1 int) {
	for x := x0; x < x1 && x < s.W; x++ {
		s.cells[y][x] = Cell{R: ' '}
	}
}

func (s *Screen) sgr(a []int) {
	cur := map[int]bool{}
	var extra []string
	for _, f := range strings.Fields(s.attr) {
		if v, err := strconv.Atoi(f); err == nil {
			cur[v] = true
		} else {
			extra = append(extra, f)
		}
	}
	rng := func(lo, hi int) {
		for k := lo; k <= hi; k++ {
			delete(cur, k)
		}
	}
	for i := 0; i < len(a); i++ {
		v := a[i]
		switch {
		case v == 0:
			cur = map[int]bool{}
			extra = nil
		case v == 22:
			delete(cur, 1)
			delete(cur, 2)
		case v >= 23 && v <= 29:
			delete(cur, v-20)
		case v == 39:
			rng(30, 37)
			rng(90, 97)
			extra = dropPrefix(extra, "fg")
		case v == 49:
			rng(40, 47)
			rng(100, 107)
			extra = dropPrefix(extra, "bg")
		case v == 38 || v == 48:
			tag := "fg"
			if v == 48 {
				tag = "bg"
			}
			extra = dropPrefix(extra, tag)
			if i+2 < len(a) && a[i+1] == 5 {
				extra = append(extra, tag+"="+strconv.Itoa(a[i+2]))
				i += 2
			} else if i+4 < len(a) && a[i+1] == 2 {
				extra = append(extra, tag+"="+strconv.Itoa(a[i+2])+"/"+strconv.Itoa(a[i+3])+"/"+strconv.Itoa(a[i+4]))
				i += 4
			}
		case v >= 30 && v <= 37, v >= 90 && v <= 97:
			rng(30, 37)
			rng(90, 97)
			extra = dropPrefix(extra, "fg")
			cur[v] = true
		case v >= 40 && v <= 47, v >= 100 && v <= 107:
			rng(40, 47)
			rng(100, 107)
			extra = dropPrefix(extra, "bg")
			cur[v] = true
		default:
			cur[v] = true
		}
	}
	var parts []string
	for k := 1; k < 110; k++ {
		if cur[k] {
			parts = append(parts, strconv.Itoa(k))
		}
	}
	parts = append(parts, extra...)
	s.attr = strings.Join(parts, " ")
}

func dropPrefix(l []string, p string) []string {
	var o []string
	for _, x := range l {
		if !strings.HasPrefix(x, p+"=") {
			o = append(o, x)
		}
	}
	return o
}

// Row returns row y as text with trailing blanks trimmed.
func (s *Screen) Row(y int) string {
	var b strings.Builder
	for _, c := range s.cells[y] {
		b.WriteRune(c.R)
	}
	return strings.TrimRight(b.String(), " ")
}

// Lines returns all rows (trailing blanks trimmed).
func (s *Screen) Lines() []string {
	out := make([]string, s.H)
	for y := range out {
		out[y] = s.Row(y)
	}
	return out
}

// Text is Lines joined by newlines with trailing empty rows removed.
func (s *Screen) Text() string {
	l := s.Lines()
	for len(l) > 0 && l[len(l)-1] == "" {
		l = l[:len(l)-1]
	}
	return strings.Join(l, "\n")
}

// CellAt returns the cell at x,y (0-based).
func (s *Screen) CellAt(x, y int) Cell { return s.cells[y][x] }

// AttrRow summarizes the attributes on row y ("0-4:1 5-9:7").
func (s *Screen) AttrRow(y int) string {
	var parts []string
	start := 0
	for x := 1; x <= s.W; x++ {
		if x == s.W || s.cells[y][x].Attr != s.cells[y][start].Attr {
			if s.cells[y][start].Attr != "" {
				parts = append(parts, strconv.Itoa(start)+"-"+strconv.Itoa(x-1)+":"+strings.ReplaceAll(s.cells[y][start].Attr, " ", ","))
			}
			start = x
		}
	}
	return strings.Join(parts, " ")
}

// Size makes a Screen usable as a ui.Terminal.
func (s *Screen) Size() (int, int) { return s.W, s.H }

// Dump renders the screen with a row-number gutter and the cursor position.
func (s *Screen) Dump() string {
	var b strings.Builder
	for y := 0; y < s.H; y++ {
		b.WriteString(strconv.Itoa(y+1) + "\t|" + s.Row(y) + "\n")
	}
	b.WriteString("cursor: row " + strconv.Itoa(s.Y+1) + " col " + strconv.Itoa(s.X+1) + "\n")
	return b.String()
}
