// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package cmcpui

import (
	"fmt"
	"math"
	"strings"

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
	leaves   []placed
	text     Rect
	hasTxt   bool
	noWeight bool // inside a stacked row: no flexible children
	summary  Rect // the one-line strip of the expanded (log) view
	sidebar  bool // widgets stacked in a right-hand column; every child is as tall as its content
	need     int  // rows the text pane must keep: 8 or a quarter of the height (spec 6.13)
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
	if env.AlertRecolour != "off" && strings.EqualFold(env.Alert, "yellow") {
		env.AlertRecolour = "edge" // yellow alert is edge-style in every theme (spec 19.3)
	}
	switch env.AlertRecolour {
	case "off":
	case "edge":
		// only the frame edge, the alert segment and the title accent take the alert's colours
		alt := env.Theme.withAlert(env.Alert)
		if alt.Set != env.Theme.Set {
			env.Theme.edge = &alt
		}
	default:
		env.Theme = env.Theme.withAlert(env.Alert)
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
	need := env.TextRows
	if need <= 0 {
		need = max(8, h*35/100) // the output stays big: balanced is 35% of the height
	}
	a := &arranger{need: need}
	top, bottom := env.Theme.margins(h)
	left := env.Theme.leftCols(w)
	inner := Rect{X: left, Y: top, W: w - left, H: h - top - bottom}
	// %cmcp_layout: stacked (a band of pictures over the output, data on the right), side-left (the
	// output on the left, every widget in a column on the right, pictures first) or side-right (mirrored).
	// Unset: side-left for the hand-built tng and plain themes on a terminal of 120 columns or more,
	// the theme's own arrangement for the other reference themes, stacked on narrower terminals.
	mode := env.Layout
	mirror := false
	switch mode {
	case "stacked":
		mode = "console"
	case "side-left", "side":
		mode = "side"
	case "side-right":
		mode, mirror = "side", true
	}
	if env.Expanded {
		// the log view: the output takes about 80% of the height; every other panel is one summary line
		mode = "expanded"
	} else if mode == "" || mode == "auto" {
		mode = "tree"
		if env.Builtin && inner.W >= 70 && inner.H >= 14 {
			mode = "console" // the built-in screens are arranged by the theme (docs/CMCP-THEMES.md)
			switch {
			case w < 120 && env.Theme.isCellTheme() && jmap(jmap(cellDoc(env.Theme), "arrange"), env.ScreenID) != nil:
				mode = "cell" // narrower terminals: the theme's own fractions (spec 19.8)
			case w >= 120:
				mode = "side" // the owner's layout for every theme: output left, widgets in a column on the right
			}
		}
	}
	switch {
	case mode == "expanded":
		a.text, a.hasTxt = Rect{X: inner.X, Y: inner.Y + 1, W: inner.W, H: inner.H - 1}, true
		a.summary = Rect{X: 0, Y: inner.Y, W: w, H: 1}
	case mode == "cell":
		rows := min(max(need, 6), inner.H-2)
		if rest := stripText(root); rest != nil {
			panels := Rect{X: inner.X, Y: inner.Y, W: inner.W, H: inner.H - rows}
			a.text, a.hasTxt = Rect{X: inner.X, Y: inner.Y + panels.H, W: inner.W, H: rows}, true
			a.cellArrange(rest, panels, env.Theme, env.ScreenID)
		}
	case mode == "console":
		if rest := stripText(root); rest != nil {
			a.console(rest, inner)
		}
	case mode == "side" && inner.W >= 80 && inner.H >= 14:
		side := max(40, min(56, inner.W*4/10))
		if cap := 80 + 2; inner.W-side-1 > cap {
			// the output is formatted for about 80 columns (spec 6.13.1 1b, port.max_cols): the
			// text pane stops there and the widget column takes the rest of the width
			side = inner.W - cap - 1
		}
		if rest := stripText(root); rest != nil {
			rest = pictureFirst(rest)
			a.orderPictures(rest, env.Theme, env.ScreenID)
			a.sidebar = true
			colX, textX := w-side, inner.X
			if mirror {
				colX, textX = inner.X, inner.X+side+1
			}
			a.arrange(rest, Rect{X: colX, Y: inner.Y, W: side, H: inner.H}, true)
			a.text, a.hasTxt = Rect{X: textX, Y: inner.Y, W: w - inner.X - side - 1, H: inner.H}, true
		}
	}
	if !a.hasTxt {
		a.leaves = nil
		a.arrange(root, inner, true)
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
	outer := a.text
	if env.Theme.Style != "" && h >= 28 && a.text.W > minTextCols+2 && a.text.H > minTextRows+2 {
		// a framed panel: the border ring is outside the text pane's rectangle, never on it
		a.text = Rect{X: a.text.X + 1, Y: a.text.Y + 1, W: a.text.W - 2, H: a.text.H - 2}
	}
	f.Text = a.text
	// the text pane is the UI's: blank it with default attributes
	for y := a.text.Y; y < a.text.Y+a.text.H; y++ {
		for x := a.text.X; x < a.text.X+a.text.W; x++ {
			g.Set(x, y, layout.Cell{R: ' '})
		}
	}
	d := &drawer{f: f, env: env, st: st, c: &canvas{g: g, clip: Rect{W: w, H: h}}}
	d.navItem(top, bottom, left)
	for _, p := range a.leaves {
		d.collect(p)
		f.Boxes = append(f.Boxes, p.r)
	}
	if a.summary.W > 0 {
		d.c.clip = a.summary
		d.summaryLine(root, a.summary)
	}
	for _, p := range a.leaves {
		d.draw(p)
	}
	if i := f.index(st.Focus); i >= 0 {
		f.Hint = d.hint(f.Items[i])
		if st.Active {
			f.FocusName = d.focusName(f.Items[i])
			f.FocusKind = f.Items[i].Kind
			if f.FocusKind == "nav" && len(d.env.Nav) > 0 {
				f.FocusLabel = d.env.Nav[max(0, min(st.ws("~nav").cur, len(d.env.Nav)-1))].Title
			}
			d.markFocus(f.Items[i])
		}
	}
	if outer != f.Text {
		d.textBorder(outer)
	}
	if rest := stripText(root); rest != nil && mode != "expanded" {
		total := 0
		rest.Walk(func(b *pack.BoundNode) {
			if b.Visible && !isContainer(b) && b.Widget != "" {
				total++
			}
		})
		if dropped := total - len(a.leaves); dropped > 0 {
			f.Dropped = dropped
			d.env.Tags = append(append([]string(nil), d.env.Tags...), fmt.Sprintf("%d hidden: F3 log, F4 off", dropped))
		}
	}
	d.frameTop()
	d.frameBottom()
	if env.ASCII {
		asciify(g)
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
		if need > r.W && len(kids) > 1 && smallControls(kids) {
			// a row of buttons wraps onto more lines instead of becoming a tall column
			x, y := r.X, r.Y
			for _, k := range kids {
				kw, _ := intrinsic(k, r.W)
				if x > r.X && x+kw > r.X+r.W {
					x, y = r.X, y+1
				}
				if y >= r.Y+r.H {
					break
				}
				a.arrange(k, Rect{X: x, Y: y, W: min(kw, r.W), H: 1}, false)
				x += kw + g
			}
		} else if need > r.W && len(kids) > 1 {
			// a stacked row: its children are as tall as their content, weights no longer apply
			old := a.noWeight
			a.noWeight = true
			a.flow(kids, r, false, gapY(b.Layout.Gap))
			a.noWeight = old
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
		if (a.noWeight || a.sidebar) && !horiz {
			w = 0
		}
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
			// pictures give way first, down to a small but readable size: the controls and the
			// readouts below them are what the player operates
			for i := range kids {
				if i != prot && wt[i] <= 0 && !isContainer(kids[i]) && pictureWidget(kids[i].Widget) && ext[i] > 6 && (big < 0 || ext[i] > ext[big]) {
					big = i
				}
			}
			if big < 0 {
				for i := range kids {
					if i != prot && wt[i] <= 0 && ext[i] > 1 && (big < 0 || ext[i] > ext[big]) {
						big = i
					}
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
	end := r.Y + r.H
	if horiz {
		end = r.X + r.W
	}
	for i, k := range kids {
		if pos >= end {
			break // no room left: what does not fit is dropped, never drawn over a sibling
		}
		ext[i] = min(ext[i], end-pos)
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
		if dir == "row" && w > 0 && len(kids) > 1 && minWidth(b) > w && smallControls(kids) {
			// wrapped lines of controls
			x, lines := 0, 1
			for _, k := range kids {
				kw, _ := intrinsic(k, w)
				if x > 0 && x+kw > w {
					x, lines = 0, lines+1
				}
				x += kw + gapX(b.Layout.Gap)
			}
			return w, lines + 2*cellsY(b.Layout.Pad)
		}
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

// sidebarWidth is the width of the widget column of the side layout, 0 for the top band layout.
// Side is the default on a wide terminal (the game's output keeps the whole left side at full
// height, the widgets stack on the right: pictures first, the data below them); "top" forces the
// band over the output, "side" forces the column even on a narrow terminal.
func sidebarWidth(mode string, w, h int) int {
	switch mode {
	case "top":
		return 0
	case "side":
		if w < 80 || h < 16 {
			return 0
		}
	default:
		if w < 100 || h < 20 {
			return 0
		}
	}
	return max(40, min(56, w*4/10))
}

// pictureWidget says whether a widget is a picture (viewscreen, radar...) that leads the column.
func pictureWidget(w string) bool {
	switch w {
	case "viewscreen", "radar", "scope", "schematic", "sector-map":
		return true
	}
	return false
}

// pictureFirst returns the tree with its pictures moved to the front of a single column: the
// radar or viewscreen on top, the sensor and weapon data below it.
func pictureFirst(rest *pack.BoundNode) *pack.BoundNode {
	var pics []*pack.BoundNode
	var strip func(b *pack.BoundNode) *pack.BoundNode
	strip = func(b *pack.BoundNode) *pack.BoundNode {
		if !isContainer(b) {
			if pictureWidget(b.Widget) {
				pics = append(pics, b)
				return nil
			}
			return b
		}
		c := *b
		c.Children = nil
		for _, k := range b.Children {
			if s := strip(k); s != nil {
				c.Children = append(c.Children, s)
			}
		}
		if len(c.Children) == 0 {
			return nil
		}
		return &c
	}
	body := strip(rest)
	col := *rest
	col.Layout = pack.Layout{Dir: "col", Gap: rest.Layout.Gap}
	col.Children = nil
	for _, p := range pics {
		q := *p
		q.Layout.Weight, q.Layout.HasSize = 0, false
		col.Children = append(col.Children, &q)
	}
	if body != nil {
		col.Children = append(col.Children, body)
	}
	return &col
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
	return ih <= h*4/3
}

// smallControls reports whether every child is a button, toggle or short label (a row of them wraps).
func smallControls(kids []*pack.BoundNode) bool {
	for _, k := range kids {
		if isContainer(k) {
			return false
		}
		switch k.Widget {
		case "button", "toggle", "label":
		default:
			return false
		}
	}
	return true
}

// console is the theme's arrangement of a built-in screen: a band of pictures across the top (the
// viewscreen or schematic on the left, the radar on the right), under it the game's output on
// the left and a column of data and controls on the right. A screen without a main picture puts
// its radar at the head of the column; one without data gives the output the whole width.
func (a *arranger) console(rest *pack.BoundNode, in Rect) {
	var pics []*pack.BoundNode
	var strip func(b *pack.BoundNode) *pack.BoundNode
	strip = func(b *pack.BoundNode) *pack.BoundNode {
		if !isContainer(b) {
			if pictureWidget(b.Widget) {
				pics = append(pics, b)
				return nil
			}
			return b
		}
		c := *b
		c.Children = nil
		for _, k := range b.Children {
			if s := strip(k); s != nil {
				c.Children = append(c.Children, s)
			}
		}
		if len(c.Children) == 0 {
			return nil
		}
		return &c
	}
	body := strip(rest)
	var main, side []*pack.BoundNode
	for _, p := range pics {
		switch p.Widget {
		case "viewscreen", "schematic", "sector-map":
			main = append(main, p)
		default:
			side = append(side, p)
		}
	}
	bandH := 0
	if len(main) > 0 {
		bandH = min(max(11, in.H*36/100), 18)
		bandH = min(bandH, in.H-a.need)
		if bandH < 4 {
			bandH = 0
		}
	}
	y0 := in.Y + bandH
	dataW := 0
	if body != nil || (len(main) == 0 || bandH == 0) && len(side) > 0 {
		dataW = max(30, min(50, in.W*36/100))
		if in.W-dataW-1 < minTextCols {
			dataW = 0
		}
	}
	if bandH > 0 {
		radarW := 0
		if len(side) > 0 {
			radarW = min(bandH*2, in.W/3)
		}
		mainW := in.W - radarW
		if radarW > 0 {
			mainW--
		}
		x := in.X
		share := mainW / len(main)
		for _, p := range main {
			q := *p
			a.leaves = append(a.leaves, placed{&q, Rect{X: x, Y: in.Y, W: share, H: bandH}})
			x += share
		}
		if radarW > 0 {
			q := *side[0]
			a.leaves = append(a.leaves, placed{&q, Rect{X: in.X + in.W - radarW, Y: in.Y, W: radarW, H: bandH}})
			side = side[1:]
		}
	}
	col := Rect{X: in.X + in.W - dataW, Y: y0, W: dataW, H: in.H - bandH}
	if dataW > 0 {
		kids := []*pack.BoundNode{}
		for _, p := range side { // pictures with no place in the band lead the column
			q := *p
			q.Layout.Weight, q.Layout.HasSize = 0, false
			kids = append(kids, &q)
		}
		if bandH == 0 {
			for _, p := range main {
				q := *p
				q.Layout.Weight, q.Layout.HasSize = 0, false
				kids = append(kids, &q)
			}
		}
		if body != nil {
			kids = append(kids, body)
		}
		colNode := *rest
		colNode.Layout = pack.Layout{Dir: "col", Gap: rest.Layout.Gap}
		colNode.Children = kids
		a.sidebar = true
		a.arrange(&colNode, col, true)
		a.text = Rect{X: in.X, Y: y0, W: in.W - dataW - 1, H: in.H - bandH}
	} else {
		a.text = Rect{X: in.X, Y: y0, W: in.W, H: in.H - bandH}
	}
	a.hasTxt = true
}

// summaryLine is the expanded view's strip: the bars and gauges of the screen as "label value".
func (d *drawer) summaryLine(root *pack.BoundNode, r Rect) {
	th := d.env.Theme
	var parts []string
	root.Walk(func(b *pack.BoundNode) {
		if !b.Visible {
			return
		}
		switch b.Widget {
		case "bar", "gauge":
			v, m, frac := fracOf(b)
			lab := str(prop(b, "label"))
			if lab == "" {
				lab = "level"
			}
			_ = v
			_ = m
			parts = append(parts, fmt.Sprintf("%s %.0f%%", lab, frac*100))
		}
	})
	line := strings.Join(parts, "   ")
	if len(d.env.Tags) > 0 {
		line = strings.Join(d.env.Tags, " ") + "   " + line
	}
	if line == "" {
		line = d.env.Title
	}
	d.c.fill(r, ' ', th.blank())
	d.c.text(r.X+1, r.Y, line, th.attr(th.RGB("color.accent")), r.W-2)
}

// orderPictures orders the leading pictures of the right-hand column by the theme's arrangement for the
// screen (spec 19.8 fractions: left to right, then top to bottom), so each theme keeps its own emphasis
// (klingon puts the radar first, ferengi the viewscreen) while the side is the same for every theme.
func (a *arranger) orderPictures(col *pack.BoundNode, th Theme, screen string) {
	arr := jmap(jmap(cellDoc(th), "arrange"), screen)
	if arr == nil {
		return
	}
	counts := map[string]int{}
	type kv struct {
		b   *pack.BoundNode
		key float64
	}
	var pics []kv
	n := 0
	for _, k := range col.Children {
		if !pictureWidget(k.Widget) {
			break
		}
		n++
		key := 10.0
		if r := fracRect(arr, k.Widget+"#"+itoa(counts[k.Widget])); r != nil {
			key = r[0] + r[1]/10
		}
		counts[k.Widget]++
		pics = append(pics, kv{k, key})
	}
	for i := 1; i < len(pics); i++ {
		for j := i; j > 0 && pics[j].key < pics[j-1].key; j-- {
			pics[j], pics[j-1] = pics[j-1], pics[j]
		}
	}
	for i, p := range pics {
		col.Children[i] = p.b
	}
}
