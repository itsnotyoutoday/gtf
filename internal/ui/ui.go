// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package ui

import (
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/itsnotyoutoday/gtf/internal/layout"
)

// UI composes the whole display: output window, status line(s), input window
// and docked panels. It is not safe for concurrent use; the client drives it
// from one goroutine.
type UI struct {
	term Terminal
	W, H int

	Depth  layout.Depth
	Visual bool
	ISize  int // input window height (tf: isize)
	StatH  int // status lines (tf: status_height)
	Wrap   WrapConfig

	Scr *Screen
	In  *Input // the built-in editor (used when Ed is nil; tests)
	// Ed supplies the input line; a client that owns the editor elsewhere
	// (the engine's InputBuffer) plugs it in here.
	Ed     Editor
	Hidden bool   // do not echo typed text (password mode)
	Prompt string // text shown before the input
	// PromptCells is the same prompt with its colours and attributes (the server's @prompt text is ANSI);
	// when set and equal to Prompt it is drawn instead of the plain text.
	PromptCells []layout.Cell

	status [][]layout.Cell
	old    []string // lines already entered, shown above the current one in the input window

	Layout   *layout.Layout
	Resolver layout.Resolver
	// Surf, when set (and sized for the current output area), is a drawn screen (a CMCP console)
	// that replaces the docked panels: its grid is painted first and the scrollback is drawn on
	// top inside Surf.Text, so the text pane is never covered.
	Surf  *Surface
	frame layout.Frame

	shadow   []layout.Cell
	shW      int
	shH      int
	curX     int
	curY     int
	needFull bool

	// non-visual mode bookkeeping
	nvPrinted int
	nvScr     *Screen
	hideAll   int      // physical lines hidden by dokey clear
	nvQueue   []nvItem // lines entered in non-visual mode, printed in order with the output
	// Cursor position after the last render (for tests).
	CursorX, CursorY int
}

// Surface is a drawn screen around the text pane (see UI.Surf).
type Surface struct {
	Grid *layout.Grid // OutRows rows, full width; the Text rectangle is blank in it
	Text layout.Rect  // where the scrollback goes
}

func (u *UI) surface() *Surface {
	if u.Surf != nil && u.Visual && u.Surf.Grid != nil && u.Surf.Grid.W == u.W && u.Surf.Grid.H == u.OutRows() && u.Surf.Text.W > 0 {
		return u.Surf
	}
	return nil
}

// Editor is the input line the UI displays.
type Editor interface {
	Text() string
	Cursor() int // byte offset
}

func (u *UI) editor() Editor {
	if u.Ed != nil {
		return u.Ed
	}
	return u.In
}

// New creates a UI drawing on term.
func New(term Terminal) *UI {
	w, h := term.Size()
	u := &UI{term: term, W: w, H: h, Depth: layout.Depth256, Visual: true, ISize: 3, StatH: 1, In: NewInput(), needFull: true}
	u.Wrap = WrapConfig{Wrap: true, Space: 4, Punct: 10}
	u.Scr = NewScreen("", 20000)
	return u
}

// Size returns the terminal size the UI believes in.
func (u *UI) Size() (int, int) { return u.W, u.H }

// OutRows is the height of the output window (tf: winlines()).
func (u *UI) OutRows() int {
	if !u.Visual {
		return u.H - 1
	}
	return max(u.H-u.StatH-u.ISize, 1)
}

// MainRect is the output region after docking panels.
func (u *UI) MainRect() layout.Rect {
	if f := u.computeFrame(); f.Main.W > 0 {
		return f.Main
	}
	return layout.Rect{X: 0, Y: 0, W: u.W, H: u.OutRows()}
}

// Winlines is the number of lines of the output window.
func (u *UI) Winlines() int { return u.MainRect().H }

func (u *UI) computeFrame() layout.Frame {
	if sf := u.surface(); sf != nil {
		return layout.Frame{W: u.W, H: u.OutRows(), Main: sf.Text}
	}
	if u.Layout == nil || !u.Visual || len(u.Layout.Panels()) == 0 {
		return layout.Frame{W: u.W, H: u.OutRows(), Main: layout.Rect{W: u.W, H: u.OutRows()}}
	}
	return u.Layout.Compute(u.W, u.OutRows(), u.Resolver)
}

// SetScreen shows another world's output.
func (u *UI) SetScreen(s *Screen) {
	u.Scr = s
	u.needFull = true
}

// SetStatus sets the status line rows (already laid out to the terminal width).
func (u *UI) SetStatus(rows [][]layout.Cell) { u.status = rows }

// SetVisual switches between visual and non-visual (scrolling) mode.
func (u *UI) SetVisual(v bool) {
	if u.Visual != v {
		u.Visual = v
		u.needFull = true
		u.nvScr = nil
	}
}

// Resize re-reads the terminal size.
func (u *UI) Resize() {
	u.W, u.H = u.term.Size()
	u.needFull = true
}

// Redraw forces a full repaint at the next Render.
func (u *UI) Redraw() { u.needFull = true }

// Beep rings the terminal bell.
func (u *UI) Beep() { u.term.Write([]byte{7}) }

// Clear erases the output window (dokey clear).
func (u *UI) Clear() {
	u.Scr.vis = u.Scr.SB.Len()
	u.Scr.Paused = false
	u.hideAll = u.Scr.SB.Len()
	u.needFull = true
}

// AddLine appends a logical line to screen s (the foreground one is drawn).
func (u *UI) AddLine(s *Screen, cells []layout.Cell) {
	main := u.MainRect()
	s.rewrap(main.W, u.Wrap)
	s.Add(cells, main.H)
	if s != u.Scr {
		s.Unseen++
	}
	if s.Tap != nil {
		s.Tap(cells)
	}
}

// Submit takes the built-in editor's line (see NoteSubmitted).
func (u *UI) Submit() string {
	hidden := u.Hidden
	line := u.In.Submit()
	if hidden {
		line = ""
	}
	u.NoteSubmitted(line)
	return line
}

// NoteSubmitted records that line was entered: it stays visible, scrolled up
// in the input window, as tf shows it, and the pager is re-armed.
func (u *UI) NoteSubmitted(line string) { u.NoteSubmittedAt(line, u.Scr.vis) }

// NoteSubmittedAt is NoteSubmitted for a line entered when the screen had
// shown `at` physical lines (output the line itself produced comes after it).
func (u *UI) NoteSubmittedAt(line string, at int) {
	if !u.Visual {
		u.nvQueue = append(u.nvQueue, nvItem{at: at, text: line})
	}
	u.old = append(u.old, line)
	if len(u.old) > 64 {
		u.old = u.old[len(u.old)-64:]
	}
	u.Scr.ResetCount(u.Winlines())
}

// ---- composition ----

type nvItem struct {
	at   int // physical lines of output that precede it
	text string
}

type grid struct {
	w, h  int
	cells []layout.Cell
}

func newGrid(w, h int) *grid {
	g := &grid{w, h, make([]layout.Cell, w*h)}
	for i := range g.cells {
		g.cells[i].R = ' '
	}
	return g
}

func (g *grid) put(x, y int, c layout.Cell) {
	if x >= 0 && y >= 0 && x < g.w && y < g.h {
		g.cells[y*g.w+x] = c
	}
}

func (g *grid) putLine(x, y, maxw int, cells []layout.Cell) {
	for i, c := range cells {
		if i >= maxw {
			break
		}
		g.put(x+i, y, c)
	}
}

// inputRows lays out the input window: returns the rows (cells) and the
// cursor position inside the window.
func (u *UI) inputRows() ([][]layout.Cell, int, int) {
	w := max(u.W-1, 1) // tf wraps the input line one column short of the edge
	ph := []rune(u.Prompt)
	// one cell per rune of prompt + input, so the prompt keeps its colours
	var text []layout.Cell
	if len(u.PromptCells) > 0 && StripCells(u.PromptCells) == u.Prompt {
		for _, c := range u.PromptCells {
			if c.R != 0 { // the filler after a double-width rune
				text = append(text, c)
			}
		}
	} else {
		for _, r := range ph {
			text = append(text, layout.Cell{R: r})
		}
	}
	cur := len(text)
	if !u.Hidden {
		ed := u.editor()
		t := ed.Text()
		bc := min(max(ed.Cursor(), 0), len(t))
		for _, r := range t {
			text = append(text, layout.Cell{R: r})
		}
		cur += utf8.RuneCountInString(t[:bc])
	}
	var cur_rows [][]layout.Cell
	for i := 0; i == 0 || i < len(text); i += w {
		end := min(i+w, len(text))
		cur_rows = append(cur_rows, append([]layout.Cell(nil), text[i:end]...))
	}
	cy, cx := cur/w, cur%w
	if cur > 0 && cur%w == 0 && cur == len(text) {
		// cursor just past a full row: starts a fresh row
		cur_rows = append(cur_rows, nil)
	}
	var rows [][]layout.Cell
	for _, o := range u.old {
		oo := []rune(o)
		for i := 0; i == 0 || i < len(oo); i += w {
			rows = append(rows, PlainCells(string(oo[i:min(i+w, len(oo))]), layout.Attr{}))
		}
	}
	off := len(rows)
	rows = append(rows, cur_rows...)
	cy += off
	if len(rows) > u.ISize {
		drop := len(rows) - u.ISize
		rows = rows[drop:]
		cy -= drop
	}
	if cy >= u.ISize {
		cy = u.ISize - 1
	}
	if cy < 0 {
		cy = 0
	}
	return rows, cx, cy
}

func (u *UI) compose() (*grid, int, int) {
	g := newGrid(u.W, u.H)
	outRows := u.OutRows()
	f := u.computeFrame()
	main := f.Main
	if f.Main.W == 0 {
		main = layout.Rect{W: u.W, H: outRows}
	}
	if sf := u.surface(); sf != nil {
		for y := 0; y < sf.Grid.H; y++ {
			for x := 0; x < sf.Grid.W; x++ {
				g.put(x, y, sf.Grid.At(x, y))
			}
		}
	}
	s := u.Scr
	s.rewrap(main.W, u.Wrap)
	// output window: bottom-aligned physical lines [vis-n, vis)
	n := min(s.vis-u.hideAll, main.H)
	if n < 0 {
		n = 0
	}
	for i := 0; i < n; i++ {
		line := s.SB.Phys(s.vis - n + i)
		g.putLine(main.X, main.Y+main.H-n+i, main.W, line)
	}
	// status
	for r := 0; r < u.StatH && u.Visual; r++ {
		y := outRows + r
		if r < len(u.status) {
			g.putLine(0, y, u.W, u.status[r])
		}
	}
	// panels
	for _, p := range f.Panels { // (none while a Surface is shown)
		pg := u.Layout.Render(p.P, p.W, p.H, u.Resolver)
		for y := 0; y < pg.H; y++ {
			for x := 0; x < pg.W; x++ {
				g.put(p.X+x, p.Y+y, pg.At(x, y))
			}
		}
	}
	u.frame = f
	// input window
	rows, cx, cy := u.inputRows()
	top := u.H - u.ISize
	if !u.Visual {
		top = u.H - 1
		rows = rows[max(len(rows)-1, 0):]
		cy = 0
	}
	for i, r := range rows {
		g.putLine(0, top+i, u.W, r)
	}
	return g, cx, top + cy
}

// Frame returns the last computed panel frame.
func (u *UI) Frame() layout.Frame { return u.frame }

// Render brings the terminal up to date.
func (u *UI) Render() {
	if !u.Visual {
		u.renderNonVisual()
		return
	}
	g, cx, cy := u.compose()
	var b strings.Builder
	if u.needFull || u.shW != u.W || u.shH != u.H || u.shadow == nil {
		b.WriteString("\x1b[0m\x1b[H\x1b[2J")
		u.shadow = newGrid(u.W, u.H).cells
		u.shW, u.shH = u.W, u.H
		u.needFull = false
	}
	b.WriteString("\x1b[?25l")
	cur := layout.Attr{}
	for y := 0; y < u.H; y++ {
		row := g.cells[y*u.W : (y+1)*u.W]
		old := u.shadow[y*u.W : (y+1)*u.W]
		first, last := -1, -1
		for x := 0; x < u.W; x++ {
			if row[x] != old[x] {
				if first < 0 {
					first = x
				}
				last = x
			}
		}
		if first < 0 {
			continue
		}
		// start on a wide cell's left half
		if first > 0 && row[first].R == 0 {
			first--
		}
		b.WriteString("\x1b[" + strconv.Itoa(y+1) + ";" + strconv.Itoa(first+1) + "H")
		for x := first; x <= last; x++ {
			c := row[x]
			if c.R == 0 {
				continue
			}
			if c.A != cur {
				b.WriteString(c.A.SGR(u.Depth))
				cur = c.A
			}
			b.WriteRune(c.R)
		}
		copy(old[first:last+1], row[first:last+1])
	}
	if cur != (layout.Attr{}) {
		b.WriteString("\x1b[0m")
	}
	b.WriteString("\x1b[" + strconv.Itoa(cy+1) + ";" + strconv.Itoa(cx+1) + "H\x1b[?25h")
	u.CursorX, u.CursorY = cx, cy
	u.term.Write([]byte(b.String()))
}

// renderNonVisual prints newly visible lines in the normal scrolling way and
// keeps the input line on the last row.
func (u *UI) renderNonVisual() {
	s := u.Scr
	s.rewrap(u.W, u.Wrap)
	var b strings.Builder
	if u.nvScr != s {
		first := u.nvScr == nil
		u.nvScr, u.nvPrinted = s, s.vis
		if first {
			u.nvPrinted = 0
		}
		u.needFull = false
	}
	if u.needFull {
		b.WriteString("\x1b[0m\x1b[H\x1b[2J")
		u.nvPrinted = max(s.vis-(u.H-1), 0)
		u.needFull = false
	}
	b.WriteString("\r\x1b[K")
	flushQ := func(upto int) {
		for len(u.nvQueue) > 0 && u.nvQueue[0].at <= upto {
			b.WriteString(u.nvQueue[0].text + "\r\n")
			u.nvQueue = u.nvQueue[1:]
		}
	}
	for ; u.nvPrinted < s.vis; u.nvPrinted++ {
		flushQ(u.nvPrinted)
		cur := layout.Attr{}
		for _, c := range s.SB.Phys(u.nvPrinted) {
			if c.R == 0 {
				continue
			}
			if c.A != cur {
				b.WriteString(c.A.SGR(u.Depth))
				cur = c.A
			}
			b.WriteRune(c.R)
		}
		if cur != (layout.Attr{}) {
			b.WriteString("\x1b[0m")
		}
		b.WriteString("\r\n")
	}
	flushQ(s.vis)
	rows, cx, _ := u.inputRows()
	if len(rows) > 0 {
		for _, c := range rows[len(rows)-1] {
			if c.R != 0 {
				b.WriteRune(c.R)
			}
		}
	}
	b.WriteString("\r")
	if cx > 0 {
		b.WriteString("\x1b[" + strconv.Itoa(cx) + "C")
	}
	u.CursorX, u.CursorY = cx, u.H-1
	u.term.Write([]byte(b.String()))
}
