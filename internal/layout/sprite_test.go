// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package layout

import "testing"

type spriteRes struct{ b *CellBlock }

func (r spriteRes) Resolve(ref string) (any, bool) {
	if ref == "sprite:s" {
		return r.b, true
	}
	return nil, false
}

func TestSpriteWidget(t *testing.T) {
	b := &CellBlock{W: 2, H: 2, Cells: []Cell{
		{R: '▀', A: Attr{FG: rgb(1, 2, 3)}}, {R: ' '},
		{R: '▄', A: Attr{FG: rgb(4, 5, 6)}}, {R: '█', A: Attr{FG: rgb(7, 8, 9)}},
	}}
	cells := evalTpl(t, "{sprite s}", spriteRes{b})
	// two rows of two cells with a line break between them
	if len(cells) != 5 || cells[2].R != '\n' || cells[0].A.FG != rgb(1, 2, 3) || cells[4].R != '█' {
		t.Errorf("cells: %+v", cells)
	}
	if got := evalTpl(t, "{sprite nope}", spriteRes{b}); len(got) != 0 {
		t.Errorf("unknown sprite should draw nothing, got %d cells", len(got))
	}
	// a malformed block (too few cells) draws nothing instead of panicking
	bad := &CellBlock{W: 4, H: 4, Cells: make([]Cell, 3)}
	if got := evalTpl(t, "{sprite s}", spriteRes{bad}); len(got) != 0 {
		t.Error("short block drawn")
	}
}

func TestCanvasBlitRGBA(t *testing.T) {
	c := NewCanvas(4, 2) // 4x4 px
	// opaque red, transparent, half-transparent premultiplied blue, opaque green
	pix := []uint8{255, 0, 0, 255, 0, 0, 0, 0, 0, 0, 128, 128, 0, 255, 0, 255}
	c.BlitRGBA(1, 1, 2, 2, pix[:16])
	if p := c.pix[1*c.W+1]; !p.Set || p.R != 255 {
		t.Errorf("red pixel: %+v", p)
	}
	if p := c.pix[1*c.W+2]; p.Set {
		t.Errorf("transparent pixel was painted: %+v", p)
	}
	if p := c.pix[2*c.W+1]; !p.Set || p.B < 120 || p.B > 130 {
		t.Errorf("half blue pixel (blended over black): %+v", p)
	}
	// out-of-range and short input are ignored
	c.BlitRGBA(-5, -5, 2, 2, pix)
	c.BlitRGBA(0, 0, 100, 100, pix)
}
