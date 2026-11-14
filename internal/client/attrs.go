// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package client

import (
	"github.com/itsnotyoutoday/gtf/internal/layout"
	"github.com/itsnotyoutoday/gtf/internal/ui"
	"github.com/itsnotyoutoday/gtfparser"
)

// Attribute rendering. The engine owns tf's attribute model (gtfparser.Attr,
// AttrString, ParseAttrs); the client only maps it onto terminal cells.

// attrCtx is the engine's attribute context built from this interpreter's
// %hiliteattr, %error_attr, %warning_attr and %info_attr.
func (c *Client) attrCtx() gtfparser.AttrCtx {
	get := func(name string) gtfparser.Attr {
		if v := c.in.Var(name); v != "" {
			if a, err := c.in.ParseAttrs(v); err == nil {
				return a
			}
		}
		return 0
	}
	return gtfparser.AttrCtx{Hilite: get("hiliteattr"), Error: get("error_attr"), Warning: get("warning_attr"), Info: get("info_attr")}
}

func colorOf(n int) layout.Color {
	if n < 16 {
		return layout.Color{Kind: layout.ANSI, N: uint8(n)}
	}
	return layout.Color{Kind: layout.Indexed, N: uint8(n)}
}

// toLayout applies an engine attribute to a cell attribute.
func toLayout(a gtfparser.Attr, base layout.Attr) layout.Attr {
	if a&(gtfparser.AttrNone|gtfparser.AttrExclusive) != 0 {
		base = layout.Attr{}
	}
	if a&gtfparser.AttrUnderline != 0 {
		base.Underline = true
	}
	if a&gtfparser.AttrReverse != 0 {
		base.Reverse = true
	}
	if a&(gtfparser.AttrBold|gtfparser.AttrHilite) != 0 {
		base.Bold = true
	}
	if a&gtfparser.AttrItalic != 0 {
		base.Italic = true
	}
	if a.HasFg() {
		base.FG = colorOf(a.FgColor())
	}
	if a.HasBg() {
		base.BG = colorOf(a.BgColor())
	}
	return base
}

// attrCells builds terminal cells for an attributed string.
func (c *Client) attrCells(s gtfparser.AttrString) []layout.Cell {
	var out []layout.Cell
	for _, sp := range s.Spans(c.attrCtx()) {
		out = append(out, ui.PlainCells(sp.Text, toLayout(sp.Attr, layout.Attr{}))...)
	}
	return out
}

// attrsCells makes cells for text carrying a tf attribute list (/echo -a).
func (c *Client) attrsCells(text, attrs string) []layout.Cell {
	var p ui.AnsiParser
	cells := p.Parse(text)
	if attrs == "" {
		return cells
	}
	a, err := c.in.ParseAttrs(attrs)
	if err != nil {
		return cells
	}
	for i := range cells {
		cells[i].A = toLayout(a, cells[i].A)
	}
	return cells
}

// overlay applies a trigger result's attributes to the cells decoded from the
// line's ANSI (byte offsets in plain index the spans).
func (c *Client) overlay(cells []layout.Cell, plain string, res *gtfparser.LineResult) []layout.Cell {
	if res.Substituted {
		return c.attrCells(res.Line)
	}
	spans := res.Spans(c.in)
	if len(spans) == 0 || (len(spans) == 1 && spans[0].Attr&gtfparser.AttrDisplay == 0) {
		return cells
	}
	out := append([]layout.Cell(nil), cells...)
	off := make([]int, len(cells))
	pos := 0
	for i, cell := range cells {
		off[i] = pos
		if cell.R != 0 {
			pos += len(string(cell.R))
		}
	}
	at := 0
	for _, sp := range spans {
		end := at + len(sp.Text)
		if sp.Attr&gtfparser.AttrDisplay != 0 {
			for i := range out {
				if off[i] >= at && off[i] < end {
					out[i].A = toLayout(sp.Attr, out[i].A)
				}
			}
		}
		at = end
	}
	return out
}
