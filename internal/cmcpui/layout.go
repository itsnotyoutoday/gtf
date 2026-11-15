// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package cmcpui

import (
	"math"

	"github.com/itsnotyoutoday/cmcp/pack"
	"github.com/itsnotyoutoday/gtf/internal/layout"
)

// Minimum size of the real text pane (spec 6.3: the client MUST keep it usable).
const (
	minTextRows = 4
	minTextCols = 30
)

type placed struct {
	b *pack.BoundNode
	r Rect
}

type arranger struct {
	leaves  []placed
	text    Rect
	hasTxt  bool
	sidebar bool // widgets stacked in a right-hand column; every child is as tall as its content
	need    int  // rows the text pane must keep: 8 or a quarter of the height (spec 6.13)
}

// Draw lays the bound tree out in a w x h cell area, draws it and returns the frame. The text
// pane's rectangle is left empty for the caller's scrollback.
func Draw(root *pack.BoundNode, w, h int, env Env, st *State) *Frame {
	if st == nil {
		st = NewState()
	}
	g := layout.NewGrid(w, h)
	if env.Theme.Name == "" {
		env.Theme = ThemeByName("plain")
	}
	bl := env.Theme.blank()
	for i := range g.Cells {
		g.Cells[i].A = bl
	}
	f := &Frame{Grid: g, Text: Rect{W: w, H: h}}
	if root == nil || w < 8 || h < 3 {
		f.Compact = true
		return f
	}
	a := &arranger{need: max(8, h/4)}
	if side := sidebarWidth(env.Layout, w, h); side > 0 {
		// Wide terminal: the game's output gets the whole left side at full height and every widget
		// is stacked in a column on the right (spec 6.13: the cell class reflows the screen).
		if rest := stripText(root); rest != nil && (env.Layout == "side" || contentFits(rest, side, h-1)) {
			a.sidebar = true
			a.arrange(rest, Rect{X: w - side, Y: 1, W: side, H: h - 1}, true)
			a.text, a.hasTxt = Rect{X: 0, Y: 1, W: w - side - 1, H: h - 1}, true
		}
	}
	if !a.hasTxt {
		a.leaves = nil
		a.arrange(root, Rect{X: 0, Y: 1, W: w, H: h - 1}, true) // row 0 is the header
	}
	if !a.hasTxt {
		a.text = Rect{} // a screen must have a textport; without one, drop the screen
	}
	if a.text.H < minTextRows || a.text.W < min(minTextCols, w) {
		// too small to be useful: the text pane takes the whole area (reflow rule of spec 6.3)
		f.Compact = true
		f.Text = Rect{W: w, H: h}
		for i := range g.Cells {
			g.Cells[i].A = layout.Attr{}
		}
		return f
	}
	f.Text = a.text
	// the text pane is the UI's: blank it with default attributes
	for y := a.text.Y; y < a.text.Y+a.text.H; y++ {
		for x := a.text.X; x < a.text.X+a.text.W; x++ {
			g.Set(x, y, layout.Cell{R: ' '})
		}
	}
	d := &drawer{f: f, env: env, st: st, c: &canvas{g: g, clip: Rect{W: w, H: h}}}
	d.header()
	for _, p := range a.leaves {
		d.collect(p)
	}
	for _, p := range a.leaves {
		d.draw(p)
	}
	if i := f.index(st.Focus); i >= 0 {
		f.Hint = d.hint(f.Items[i])
	}
	return f
}

func cellsX(u float64) int { return int(math.Ceil(u/8 - 1e-9)) }
func cellsY(u float64) int { return int(math.Ceil(u/16 - 1e-9)) }

func gapX(u float64) int {
	if u <= 0 {
		return 0
	}
	return max(1, int(math.Round(u/8)))
}
func gapY(u float64) int { return int(math.Round(u / 16)) }

func isContainer(b *pack.BoundNode) bool {
	for _, c := range b.Children {
		if c.Visible {
			return true
		}
	}
	return false
}

func visibleKids(b *pack.BoundNode) []*pack.BoundNode {
	var k []*pack.BoundNode
	for _, c := range b.Children {
		if c.Visible {
			k = append(k, c)
		}
	}
	return k
}

func (a *arranger) arrange(b *pack.BoundNode, r Rect, top bool) {
	if !b.Visible || r.W <= 0 || r.H <= 0 {
		return
	}
	if !isContainer(b) {
		if b.Widget == "textport" {
			a.text, a.hasTxt = r, true
			return
		}
		if b.Widget == "" {
			return
		}
		a.leaves = append(a.leaves, placed{b, r})
		return
	}
	px, py := cellsX(b.Layout.Pad), cellsY(b.Layout.Pad)
	r = Rect{X: r.X + px, Y: r.Y + py, W: r.W - 2*px, H: r.H - 2*py}
	kids := visibleKids(b)
	switch b.Layout.Dir {
	case "row":
		// spec 6.13 reflow: a row whose children's minimum widths do not fit is stacked as a column
		need, g := 0, gapX(b.Layout.Gap)
		for i, k := range kids {
			w := 0
			if k.Layout.HasSize {
				w = cellsX(k.Layout.Size)
			} else {
				w = minWidth(k)
			}
			need += w
			if i > 0 {
				need += g
			}
		}
		if need > r.W && len(kids) > 1 {
			a.flow(kids, r, false, gapY(b.Layout.Gap))
		} else {
			a.flow(kids, r, true, g)
		}
	case "stack":
		for _, k := range kids {
			a.arrange(k, r, false)
		}
	case "grid":
		a.grid(b, kids, r)
	default:
		a.flow(kids, r, false, gapY(b.Layout.Gap))
	}
}

func (a *arranger) grid(b *pack.BoundNode, kids []*pack.BoundNode, r Rect) {
	cols := max(b.Layout.Cols, 1)
	rows := b.Layout.Rows
	if rows == 0 {
		rows = (len(kids) + cols - 1) / cols
	}
	if rows == 0 {
		return
	}
	cw, ch := r.W/cols, r.H/rows
	for i, k := range kids {
		col, row := k.Layout.Col, k.Layout.Row
		if col == 0 && row == 0 && i > 0 || (col == 0 && row == 0) {
			col, row = i%cols, i/cols
		}
		cs, rs := max(k.Layout.ColSpan, 1), max(k.Layout.RowSpan, 1)
		a.arrange(k, Rect{X: r.X + col*cw, Y: r.Y + row*ch, W: cw * cs, H: ch * rs}, false)
	}
}

// flow lays kids along one axis (horizontal for a row).
func (a *arranger) flow(kids []*pack.BoundNode, r Rect, horiz bool, gap int) {
	if len(kids) == 0 {
		return
	}
	total := r.H
	if horiz {
		total = r.W
	}
	ext := make([]int, len(kids))
	wt := make([]float64, len(kids))
	var fixed, wsum float64
	prot := -1
	for i, k := range kids {
		w := k.Layout.Weight
		switch {
		case k.Layout.HasSize && !horiz && isContainer(k) && k.Layout.Dir == "row" && minWidth(k) > r.W:
			// a fixed-height row that had to be stacked is as tall as its children together
			_, ih := intrinsic(k, r.W)
			ext[i] = ih
		case k.Layout.HasSize:
			if horiz {
				ext[i] = cellsX(k.Layout.Size)
			} else {
				ext[i] = cellsY(k.Layout.Size)
			}
		case w > 0:
			wt[i] = w
		case isText(k):
			wt[i] = 1
		default:
			iw, ih := intrinsic(k, r.W)
			if horiz {
				ext[i] = iw
			} else {
				ext[i] = ih
			}
		}
		if hasText(k) {
			prot = i
			if !horiz && wt[i] == 0 {
				ext[i] = max(ext[i], a.need)
			}
		}
		wsum += wt[i]
		fixed += float64(ext[i])
	}
	gaps := gap * (len(kids) - 1)
	if !horiz && prot >= 0 && isText(kids[prot]) {
		// The text pane takes everything the widgets above it do not need: the game's output fills
		// the free area (with the player's wrap), instead of widgets stretching over empty space.
		// Every other child is as tall as its content (a fixed size is kept), at most what leaves
		// the pane its minimum.
		fixed, wsum = 0, 0
		for i, k := range kids {
			wt[i] = 0
			if i == prot {
				ext[i] = 0
				continue
			}
			if !k.Layout.HasSize {
				_, ih := intrinsic(k, r.W)
				ext[i] = ih
			}
			fixed += float64(ext[i])
		}
		wt[prot], wsum = 1, 1
		over := int(fixed) + gaps + a.need - total
		for i := len(kids) - 1; i >= 0 && over > 0; i-- {
			if i == prot {
				continue
			}
			cut := min(ext[i], over)
			ext[i] -= cut
			over -= cut
		}
		fixed = 0
		for _, e := range ext {
			fixed += float64(e)
		}
	}
	avail := total - gaps - int(fixed)
	if avail < 0 {
		// over-full: shrink the fixed extents that are not the text pane first, last child first
		need := -avail
		for need > 0 {
			big := -1
			for i := range kids {
				if i != prot && wt[i] <= 0 && ext[i] > 1 && (big < 0 || ext[i] > ext[big]) {
					big = i
				}
			}
			if big < 0 {
				break
			}
			ext[big]--
			need--
		}
		if need > 0 && prot >= 0 {
			cut := min(max(ext[prot]-1, 0), need)
			ext[prot] -= cut
			need -= cut
		}
		avail = 0
	}
	// flexible children share what is left
	if wsum > 0 {
		left := avail
		var last = -1
		for i := range kids {
			if wt[i] > 0 {
				last = i
			}
		}
		for i := range kids {
			if wt[i] <= 0 {
				continue
			}
			share := int(math.Floor(float64(avail) * wt[i] / wsum))
			if i == last {
				share = left
			}
			ext[i] = share
			left -= share
		}
	}
	pos := r.Y
	if horiz {
		pos = r.X
	}
	for i, k := range kids {
		var kr Rect
		if horiz {
			kr = Rect{X: pos, Y: r.Y, W: ext[i], H: r.H}
		} else {
			kr = Rect{X: r.X, Y: pos, W: r.W, H: ext[i]}
		}
		a.arrange(k, kr, false)
		pos += ext[i] + gap
	}
}

func isText(b *pack.BoundNode) bool { return b.Widget == "textport" && !isContainer(b) }

func hasText(b *pack.BoundNode) bool {
	if isText(b) {
		return true
	}
	for _, c := range b.Children {
		if c.Visible && hasText(c) {
			return true
		}
	}
	return false
}

// intrinsic is the natural size in cells of a node whose parent offers width w.
func intrinsic(b *pack.BoundNode, w int) (int, int) {
	if isContainer(b) {
		kids := visibleKids(b)
		px, py := cellsX(b.Layout.Pad), cellsY(b.Layout.Pad)
		var iw, ih int
		dir := b.Layout.Dir
		if dir == "row" && w > 0 && len(kids) > 1 && minWidth(b) > w {
			dir = "col" // the row is stacked (spec 6.13), so it is as tall as its children together
		}
		switch dir {
		case "row":
			g := gapX(b.Layout.Gap)
			for i, k := range kids {
				kw, kh := intrinsic(k, w)
				if k.Layout.HasSize {
					kw = cellsX(k.Layout.Size)
				}
				iw += kw
				if i > 0 {
					iw += g
				}
				ih = max(ih, kh)
			}
		case "stack", "grid":
			for _, k := range kids {
				kw, kh := intrinsic(k, w)
				iw, ih = max(iw, kw), max(ih, kh)
			}
		default:
			g := gapY(b.Layout.Gap)
			for i, k := range kids {
				kw, kh := intrinsic(k, w)
				if k.Layout.HasSize {
					kh = cellsY(k.Layout.Size)
				}
				ih += kh
				if i > 0 {
					ih += g
				}
				iw = max(iw, kw)
			}
		}
		return iw + 2*px, ih + 2*py
	}
	return leafSize(b, w)
}

// minWidth is the least width a node needs to be usable (spec 6.13: minimum sizes are the
// client's business): a bar a dozen cells, a table or picker a couple of columns, a button its
// label; containers add up their children (rows) or take the widest (columns).
func minWidth(b *pack.BoundNode) int {
	if isContainer(b) {
		kids := visibleKids(b)
		w := 0
		for i, k := range kids {
			kw := minWidth(k)
			if k.Layout.HasSize && b.Layout.Dir == "row" {
				kw = cellsX(k.Layout.Size)
			}
			if b.Layout.Dir == "row" {
				w += kw
				if i > 0 {
					w += gapX(b.Layout.Gap)
				}
			} else {
				w = max(w, kw)
			}
		}
		return w + 2*cellsX(b.Layout.Pad)
	}
	iw, _ := leafSize(b, 0)
	switch b.Widget {
	case "button", "toggle", "label":
		return iw
	case "textport":
		return 8
	case "table", "list", "bar-list", "contact-picker", "team-assigner", "log", "pad-diagram":
		return min(iw, 26)
	}
	return min(iw, 14)
}

// sidebarWidth is the width of the widget column of the optional side layout (%cmcp_layout side:
// the output on the whole left, every widget stacked on the right), 0 for the default top layout:
// a compact band of widgets (viewscreen, radar, controls as the pack arranges them) over a text
// pane that takes all the remaining area.
func sidebarWidth(mode string, w, h int) int {
	if mode != "side" || w < 80 || h < 16 {
		return 0
	}
	return max(36, min(52, w*4/10))
}

// stripText returns a copy of the tree without its textport (nil if nothing is left).
func stripText(b *pack.BoundNode) *pack.BoundNode {
	if !b.Visible {
		return nil
	}
	if !isContainer(b) {
		if b.Widget == "textport" {
			return nil
		}
		return b
	}
	c := *b
	c.Children = nil
	for _, k := range b.Children {
		if s := stripText(k); s != nil {
			c.Children = append(c.Children, s)
		}
	}
	if len(c.Children) == 0 {
		return nil
	}
	return &c
}

// contentFits says whether the widgets, as tall as their content, fit in a column of the given
// size once the big pictures shrink to a minimum: otherwise the top layout is better.
func contentFits(rest *pack.BoundNode, w, h int) bool {
	_, ih := intrinsic(rest, w)
	return ih <= h
}
