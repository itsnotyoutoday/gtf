// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package layout

// EXPERIMENTAL: the sprite entry points ({sprite NAME} and Canvas.BlitRGBA)
// are the hook for the vector sprite rasteriser (the core's raster package). Their
// shape may change when the CMCP sprite format is final.

// CellBlock is a ready-made grid of styled cells, row-major, that a panel can
// show with {sprite NAME}; the client resolves "sprite:NAME" to a *CellBlock.
type CellBlock struct {
	W, H  int
	Cells []Cell
}

// spriteWidget renders {sprite NAME}: the block's rows, one per line.
func (c *Ctx) spriteWidget(name string) {
	v, ok := c.lookup("sprite:" + name)
	if !ok {
		return
	}
	b, ok := v.(*CellBlock)
	if !ok || b == nil || b.W <= 0 || len(b.Cells) < b.W*b.H {
		return
	}
	for y := 0; y < b.H; y++ {
		c.out = append(c.out, b.Cells[y*b.W:(y+1)*b.W]...)
		if y < b.H-1 {
			c.emit("\n")
		}
	}
}

// BlitRGBA paints a w x h block of premultiplied RGBA pixels (4 bytes each,
// the layout of image.RGBA) with its top-left corner at pixel (x, y).
// Transparent pixels leave the canvas as it was; partly transparent ones blend.
func (c *Canvas) BlitRGBA(x, y, w, h int, pix []uint8) {
	if w <= 0 || h <= 0 || len(pix) < w*h*4 {
		return
	}
	for j := 0; j < h; j++ {
		for i := 0; i < w; i++ {
			p := pix[(j*w+i)*4 : (j*w+i)*4+4]
			a := int(p[3])
			if a == 0 {
				continue
			}
			un := func(v uint8) uint8 { return uint8(min(255, (int(v)*255+a/2)/a)) }
			c.Blend(x+i, y+j, Color{Kind: RGB, R: un(p[0]), G: un(p[1]), B: un(p[2])}, float64(a)/255)
		}
	}
}

// PixelAt returns the colour of canvas pixel (x, y) and whether it is set.
func (c *Canvas) PixelAt(x, y int) (Color, bool) {
	if x < 0 || y < 0 || x >= c.W || y >= c.H {
		return Color{}, false
	}
	p := c.pix[y*c.W+x]
	return Color{Kind: RGB, R: p.R, G: p.G, B: p.B}, p.Set
}
