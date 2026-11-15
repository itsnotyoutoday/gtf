// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package cmcpui

import (
	"strings"

	"github.com/itsnotyoutoday/gtf/internal/layout"
)

// margins returns the rows the theme's frame takes at the top and the bottom of the screen area.
// The text pane and every widget live between them; the frame never enters that area.
// Margins exports the frame's top and bottom rows.
func (t Theme) Margins(h int) (top, bottom int) { return t.margins(h) }

func (t Theme) margins(h int) (top, bottom int) {
	if t.isCellTheme() {
		c := cellDoc(t)
		m := jmap(c, "margin")
		return int(jnum(m, "top", 1)), int(jnum(m, "bottom", 1))
	}
	switch t.Style {
	case "tng":
		if h < 28 {
			return 2, 1 // a small terminal gets a slim frame: the output keeps the room
		}
		return 4, 2
	}
	return 1, 0 // plain: one header row
}

// leftCols is the width of the console-switch column the frame carries on a wide terminal (a
// decoration: the screens follow manning and rooms, the column shows which console this is).
func (t Theme) leftCols(w int) int {
	if t.isCellTheme() {
		n := jmap(cellDoc(t), "nav")
		if n != nil && len(jlist(n, "items")) > 0 && w >= int(jnum(n, "min_cols", 100)) {
			return int(jnum(n, "width", 12))
		}
		return 0
	}
	if t.Style == "tng" && w >= 110 {
		return 13
	}
	return 0
}

var consoleNames = []string{"SENSORS", "ENGINEER", "TACTICAL", "NAVIGATE", "COMMS"}

// consoleTab maps the shown screen to the switch column's entry it belongs to.
func consoleTab(title string) int {
	u := strings.ToUpper(title)
	switch {
	case strings.Contains(u, "SENSOR"):
		return 0
	case strings.Contains(u, "POWER"), strings.Contains(u, "DAMAGE"), strings.Contains(u, "OPERATION"), strings.Contains(u, "ENGIN"):
		return 1
	case strings.Contains(u, "TACTIC"):
		return 2
	case strings.Contains(u, "HELM"), strings.Contains(u, "NAV"):
		return 3
	}
	return 4
}

// frameTop draws the top of the frame (or the plain header row).
func (d *drawer) frameTop() {
	switch {
	case d.env.Theme.Style == "tng":
		d.tngBands()
	case d.env.Theme.isCellTheme():
		d.cellFrame()
	default:
		d.header()
	}
}

// frameBottom draws the bottom strip once the focused widget's hint is known.
func (d *drawer) frameBottom() {}

// bandColor is the i-th colour of the theme's rainbow in the active palette set: the reference
// theme's own palette, red when a red alert switched the set.
func (d *drawer) bandColor(i int) layout.Color { return d.env.Theme.cycle(i) }

// edgeColor is the i-th band colour of the frame edge: the alert's palette in edge mode.
func (d *drawer) edgeColor(i int) layout.Color { return d.env.Theme.edgeTheme().cycle(i) }

// seg is one coloured segment of a band: its label and width in cells (0 = takes the rest).
type seg struct {
	text string
	w    int
	col  layout.Color
	fg   layout.Color
}

// tngBands draws the LCARS frame: a rounded elbow and coloured segments across the top carrying
// the title, the ship, the mode and the key hint, and a thin band along the bottom. The bands are
// pixels (two per cell) with rounded ends; the labels are text on the pixels' colour.
func (d *drawer) tngBands() {
	th := d.env.Theme
	g := d.f.Grid
	w := g.W
	topRows, botRows := th.margins(g.H)
	bg := th.RGB("color.bg")
	// ---- top band: 4 rows = 8 pixels
	cv := layout.NewCanvas(w, topRows)
	hh := topRows * 2
	tr := (topRows - 1) / 2 // the cell row the labels sit on
	// the elbow: a big rounded block at the left that turns into the top band
	elbowW := 12
	if topRows < 4 {
		elbowW = 6
	}
	lw := th.leftCols(g.W)
	if lw > 0 {
		elbowW = lw
	}
	if lw == 0 {
		cv.RoundRect(0, 0, elbowW, hh, float64(hh), "tl", d.edgeColor(0), 1)
		cv.Rect(elbowW-3, 0, 3, hh, d.edgeColor(0), 1, true)
	}
	// segments
	title := strings.ToUpper(d.env.Title)
	if title == "" {
		title = "CONSOLE"
	}
	ship := strings.ToUpper(d.env.Ship)
	alert := strings.ToUpper(orDefault(d.env.Alert, "green")) + " ALERT"
	keys := "TAB: SCREEN KEYS"
	if d.st.Active {
		keys = strings.ToUpper(d.f.FocusName)
	}
	if d.env.Consent != "" {
		keys = d.env.Consent
	}
	if d.env.Notice != "" {
		keys = strings.ToUpper(d.env.Notice)
	}
	segs := []seg{{text: title, col: d.bandColor(3)}}
	if ship != "" {
		segs = append(segs, seg{text: ship, col: d.bandColor(1)})
	}
	for _, t := range d.env.Tags {
		segs = append(segs, seg{text: strings.ToUpper(t), col: d.bandColor(4)})
	}
	segs = append(segs, seg{text: alert, col: d.edgeColor(2)})
	right := seg{text: keys, col: d.bandColor(0)}
	x := elbowW + 2
	rightW := runeLen(right.text) + 4
	for i, s := range segs {
		sw := runeLen(s.text) + 4
		if x+sw+2 > w-rightW-2 {
			break
		}
		cv.Rect(x, 0, sw, hh-2, s.col, 1, true) // segments are plain bars: the band's lower edge is the elbow's
		cv.Text(x+2, tr, s.text, layout.Attr{FG: dark, BG: s.col, Bold: true})
		segs[i].w = sw
		x += sw + 2
	}
	// a long gradient-less filler bar to the right segment
	fillEnd := w - rightW - 2
	if fillEnd > x {
		cv.Rect(x, 0, fillEnd-x, hh-2, d.bandColor(4), 0.9, true)
	}
	cv.RoundRect(w-rightW, 0, rightW, hh-2, float64(hh-2)/2, "tr br", right.col, 1)
	cv.Text(w-rightW+2, tr, keys, layout.Attr{FG: dark, BG: right.col, Bold: true})
	d.blitRows(cv.Block(bg), 0, 0, topRows)
	if lw > 0 {
		// the elbow: a large corner with a long, anti-aliased curve, one piece with the column below it
		er := topRows + 4
		ce := layout.NewCanvas(lw, er)
		ce.RoundRect(0, 0, lw, er*2, 22, "tl", d.edgeColor(0), 1)
		ce.Rect(lw-4, 0, 4, hh-2, d.edgeColor(0), 1, true)
		ce.RoundRect(lw-6, hh-2, 6, 3, 0, "", bg, 1) // a notch under the band, where the first segment starts
		d.blitRows(ce.Block(bg), 0, 0, er)
	}
	d.tngColumn(topRows, botRows)
	// ---- bottom band: 2 rows, a segment strip with the focused widget's hint
	cb := layout.NewCanvas(w, botRows)
	bh := botRows * 2
	cb.RoundRect(0, 0, elbowW, bh, float64(bh)/2, "bl", d.edgeColor(0), 1)
	// the status strip: hull, shields, core, lock and alert as coloured segments
	x = elbowW + 2
	status := d.env.Status
	if d.st.Active {
		// while the screen has the keyboard the strip says which keys work
		status = []string{strings.ToUpper(d.f.Header())}
	}
	for i, t := range status {
		sw := runeLen(t) + 4
		if x+sw+10 > w {
			sw = w - x - 10
			if sw < 8 {
				break
			}
			t = string([]rune(t)[:sw-4])
		}
		col := d.bandColor(i + 1)
		cb.Rect(x, 0, sw, bh-1, col, 1, true)
		cb.Text(x+2, 0, t, layout.Attr{FG: dark, BG: col, Bold: true})
		x += sw + 2
	}
	if w-x-10 > 0 {
		cb.Rect(x, 0, w-x-10, bh-1, d.bandColor(4), 0.8, true)
	}
	cb.RoundRect(w-8, 0, 8, bh-1, float64(bh-1)/2, "br", d.edgeColor(2), 1)
	d.blitRows(cb.Block(bg), 0, g.H-botRows, botRows)
}

func orDefault(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

// blitRows copies a block's rows into the grid at x, y.
func (d *drawer) blitRows(b *layout.CellBlock, x, y, rows int) {
	for j := 0; j < min(b.H, rows); j++ {
		for i := 0; i < b.W; i++ {
			d.f.Grid.Set(x+i, y+j, b.Cells[j*b.W+i])
		}
	}
}

// tngColumn draws the console-switch column down the left side: a large elbow at the top, one
// pill per console (the shown one lit), a soft block below, curving into the bottom band.
func (d *drawer) tngColumn(topRows, botRows int) {
	g := d.f.Grid
	lw := d.env.Theme.leftCols(g.W)
	if lw == 0 {
		return
	}
	th := d.env.Theme
	bg := th.RGB("color.bg")
	start := topRows + 4 // the elbow takes the rows above
	rows := g.H - start - botRows
	cv := layout.NewCanvas(lw, rows)
	ph := cv.H
	y := 0
	ents := d.navEntries()
	if len(ents) == 0 {
		for i, n := range consoleNames {
			ents = append(ents, NavEntry{Title: n, Available: true, Active: i == consoleTab(d.env.Title)})
		}
	}
	for i, e := range navWindow(ents, ph/2-3) {
		col := d.bandColor(i)
		fg := dark
		if !e.Available {
			col, fg = layout.Lerp(col, rgbC(0, 0, 0), 0.7), rgbC(150, 150, 160)
		}
		if e.Active {
			col = layout.Lerp(col, rgbC(255, 255, 255), 0.35)
		}
		if e.Cursor {
			col, fg = rgbC(255, 255, 255), rgbC(0, 0, 0) // the menu's keyboard cursor: a white pill
		}
		n := strings.ToUpper(e.Title)
		if len([]rune(n)) > lw-3 {
			n = string([]rune(n)[:lw-4]) + "…"
		}
		cv.RoundRect(0, y*2, lw-1, 2, 1, "tr br", col, 1)
		cv.Text(max(lw-2-len([]rune(n)), 0), y, n, layout.Attr{FG: fg, BG: col, Bold: true})
		if e.Active {
			cv.Rect(lw-1, y*2, 1, 2, rgbC(255, 255, 255), 1, true)
		}
		if e.Cursor {
			cv.Text(0, y, "▶", layout.Attr{FG: fg, BG: col, Bold: true})
		} else if e.Active {
			cv.Text(0, y, "●", layout.Attr{FG: fg, BG: col, Bold: true})
		}
		y += 2
	}
	y++
	// the soft blocks under the pills: stacked, rounded, each its own colour, curving into the bottom band
	rest := ph - y*2
	if rest > 6 {
		seg := max(rest/3, 4)
		yy := y * 2
		for k := 0; yy < ph; k++ {
			h2 := min(seg-1, ph-yy)
			round := "none" // an unmatched corner list rounds nothing (an empty one rounds all four)
			if yy+seg >= ph {
				round = "bl"
				h2 = ph - yy
			}
			cv.RoundRect(0, yy, lw-1, h2, 8, round, d.bandColor(4+k), 0.9)
			yy += seg
		}
	}
	d.blitRows(cv.Block(bg), 0, start, rows)
}

// textBorder draws the thin frame around the game's output panel, with its label in the top
// edge; it lives in the ring of cells around the text pane's rectangle.
func (d *drawer) textBorder(o Rect) {
	th := d.env.Theme
	col := d.edgeColor(0)
	bg := th.blank().BG
	a := layout.Attr{FG: layout.Lerp(col, rgbC(0, 0, 0), 0.25), BG: bg}
	d.c.clip = Rect{W: d.f.Grid.W, H: d.f.Grid.H}
	g := d.f.Grid
	// the border's character set: the theme's own (cell.port.border: round, heavy, double, ornate ...)
	tl, tr, bl, br, hz, vt := '╭', '╮', '╰', '╯', '─', '│'
	pc := jmap(cellDoc(th), "port")
	if r := th.edgeTheme().role(jstr(pc, "role")); jstr(pc, "role") != "" {
		a.FG = layout.Lerp(r, rgbC(0, 0, 0), 0.1)
		col = r
	}
	switch jstr(pc, "border") {
	case "heavy":
		tl, tr, bl, br, hz, vt = '┏', '┓', '┗', '┛', '━', '┃'
	case "double":
		tl, tr, bl, br, hz, vt = '╔', '╗', '╚', '╝', '═', '║'
	case "ornate":
		tl, tr, bl, br, hz, vt = '◆', '◆', '◆', '◆', '═', '║'
	case "square":
		tl, tr, bl, br, hz, vt = '┌', '┐', '└', '┘', '─', '│'
	}
	g.Set(o.X, o.Y, layout.Cell{R: tl, A: a})
	g.Set(o.X+o.W-1, o.Y, layout.Cell{R: tr, A: a})
	g.Set(o.X, o.Y+o.H-1, layout.Cell{R: bl, A: a})
	g.Set(o.X+o.W-1, o.Y+o.H-1, layout.Cell{R: br, A: a})
	for x := o.X + 1; x < o.X+o.W-1; x++ {
		hr := hz
		if jstr(pc, "border") == "ornate" && x%4 == o.X%4+2 {
			hr = '◇'
		}
		g.Set(x, o.Y, layout.Cell{R: hr, A: a})
		g.Set(x, o.Y+o.H-1, layout.Cell{R: hr, A: a})
	}
	for y := o.Y + 1; y < o.Y+o.H-1; y++ {
		g.Set(o.X, y, layout.Cell{R: vt, A: a})
		g.Set(o.X+o.W-1, y, layout.Cell{R: vt, A: a})
	}
	lab := " GAME OUTPUT "
	if d.env.Expanded {
		lab = " LOG "
	}
	for i, r := range []rune(lab) {
		g.Set(o.X+2+i, o.Y, layout.Cell{R: r, A: layout.Attr{FG: dark, BG: col, Bold: true}})
	}
}
