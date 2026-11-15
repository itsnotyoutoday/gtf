// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package raster

import (
	"encoding/json"
	"image"
	"math"
	"sort"
)

// Buffer is a premultiplied RGBA pixel buffer (the layout of image.RGBA).
type Buffer struct {
	W, H int
	Pix  []uint8 // 4 bytes per pixel: premultiplied R, G, B, A
}

func newBuffer(w, h int) *Buffer { return &Buffer{W: w, H: h, Pix: make([]uint8, w*h*4)} }

// Image shares the buffer's memory as an *image.RGBA.
func (b *Buffer) Image() *image.RGBA {
	return &image.RGBA{Pix: b.Pix, Stride: b.W * 4, Rect: image.Rect(0, 0, b.W, b.H)}
}

// At returns the straight (non-premultiplied) colour at x, y.
func (b *Buffer) At(x, y int) Color {
	i := (y*b.W + x) * 4
	a := int(b.Pix[i+3])
	if a == 0 {
		return Color{}
	}
	if a == 255 {
		return Color{b.Pix[i], b.Pix[i+1], b.Pix[i+2], 255}
	}
	un := func(v uint8) uint8 { return uint8(min(255, (int(v)*255+a/2)/a)) }
	return Color{un(b.Pix[i]), un(b.Pix[i+1]), un(b.Pix[i+2]), uint8(a)}
}

// over composites a straight colour with source alpha sa (0..255) onto pixel i.
func (b *Buffer) over(i int, c Color, sa int) {
	if sa <= 0 {
		return
	}
	p := b.Pix[i : i+4 : i+4]
	if sa >= 255 {
		p[0], p[1], p[2], p[3] = c.R, c.G, c.B, 255
		return
	}
	inv := 255 - sa
	p[0] = uint8(mul255(int(c.R), sa) + mul255(int(p[0]), inv))
	p[1] = uint8(mul255(int(c.G), sa) + mul255(int(p[1]), inv))
	p[2] = uint8(mul255(int(c.B), sa) + mul255(int(p[2]), inv))
	p[3] = uint8(sa + mul255(int(p[3]), inv))
}

// Paint kinds.
const (
	PaintNone   = ""
	PaintSolid  = "solid"
	PaintLinear = "linear"
	PaintRadial = "radial"
)

// Stop is one gradient colour stop.
type Stop struct {
	Offset float64
	Color  Color
}

// UnmarshalJSON accepts [offset, "colour"] or {"offset":..,"color":..}.
func (s *Stop) UnmarshalJSON(b []byte) error {
	var arr []json.RawMessage
	if json.Unmarshal(b, &arr) == nil {
		if len(arr) != 2 {
			return invalidErr("a gradient stop is [offset, colour]")
		}
		if err := json.Unmarshal(arr[0], &s.Offset); err != nil {
			return invalidErr("bad stop offset")
		}
		return json.Unmarshal(arr[1], &s.Color)
	}
	var o struct {
		Offset float64 `json:"offset"`
		Color  Color   `json:"color"`
	}
	if err := json.Unmarshal(b, &o); err != nil {
		return invalidErr("bad gradient stop")
	}
	s.Offset, s.Color = o.Offset, o.Color
	return nil
}

// MarshalJSON writes the compact [offset, "colour"] form.
func (s Stop) MarshalJSON() ([]byte, error) { return json.Marshal([]any{s.Offset, s.Color}) }

// Paint says how to colour a fill or stroke: a solid colour or a gradient.
// Gradient geometry is in the shape's own coordinate system (after its
// transform), like SVG userSpaceOnUse.
//
// JSON: "#rrggbb[aa]" | "none" | {"type":"linear","x1":0,"y1":0,"x2":1,"y2":0,"stops":[[0,"#fff"],[1,"#000"]]}
// | {"type":"radial","cx":0,"cy":0,"r":10,"stops":[...]}; "spread" is
// "pad" (default), "reflect" or "repeat".
type Paint struct {
	Kind   string
	Color  Color // PaintSolid
	X1, Y1 float64
	X2, Y2 float64 // linear: gradient vector
	CX, CY float64
	R      float64 // radial
	Spread string
	Stops  []Stop
}

// Solid is a convenience constructor.
func Solid(c Color) *Paint { return &Paint{Kind: PaintSolid, Color: c} }

type paintJSON struct {
	Type   string  `json:"type"`
	X1     float64 `json:"x1"`
	Y1     float64 `json:"y1"`
	X2     float64 `json:"x2"`
	Y2     float64 `json:"y2"`
	CX     float64 `json:"cx"`
	CY     float64 `json:"cy"`
	R      float64 `json:"r"`
	Spread string  `json:"spread,omitempty"`
	Stops  []Stop  `json:"stops"`
}

// UnmarshalJSON reads the forms documented on Paint.
func (p *Paint) UnmarshalJSON(b []byte) error {
	var s string
	if json.Unmarshal(b, &s) == nil {
		if s == "none" || s == "" {
			*p = Paint{}
			return nil
		}
		c, err := ParseColor(s)
		if err != nil {
			return err
		}
		*p = Paint{Kind: PaintSolid, Color: c}
		return nil
	}
	var o paintJSON
	if err := json.Unmarshal(b, &o); err != nil {
		return invalidErr("bad paint: %v", err)
	}
	switch o.Type {
	case PaintLinear, PaintRadial:
	default:
		return invalidErr("unknown paint type %q", o.Type)
	}
	*p = Paint{Kind: o.Type, X1: o.X1, Y1: o.Y1, X2: o.X2, Y2: o.Y2, CX: o.CX, CY: o.CY, R: o.R, Spread: o.Spread, Stops: o.Stops}
	return nil
}

// MarshalJSON writes the documented forms.
func (p Paint) MarshalJSON() ([]byte, error) {
	switch p.Kind {
	case PaintSolid:
		return json.Marshal(p.Color)
	case PaintLinear, PaintRadial:
		return json.Marshal(paintJSON{Type: p.Kind, X1: p.X1, Y1: p.Y1, X2: p.X2, Y2: p.Y2, CX: p.CX, CY: p.CY, R: p.R, Spread: p.Spread, Stops: p.Stops})
	}
	return json.Marshal("none")
}

func (p *Paint) validate(l *Limits, what string) error {
	switch p.Kind {
	case PaintNone, PaintSolid:
		return nil
	case PaintLinear, PaintRadial:
	default:
		return invalidErr("%s: unknown paint kind %q", what, p.Kind)
	}
	if len(p.Stops) == 0 {
		return invalidErr("%s: gradient has no stops", what)
	}
	if len(p.Stops) > l.MaxStops {
		return limitErr("%s: %d gradient stops (max %d)", what, len(p.Stops), l.MaxStops)
	}
	for _, v := range []float64{p.X1, p.Y1, p.X2, p.Y2, p.CX, p.CY, p.R} {
		if err := checkNum(what+" gradient value", v, l.MaxCoord); err != nil {
			return err
		}
	}
	for _, s := range p.Stops {
		if !finite(s.Offset) {
			return invalidErr("%s: gradient stop offset is not finite", what)
		}
	}
	switch p.Spread {
	case "", "pad", "reflect", "repeat":
	default:
		return invalidErr("%s: unknown spread %q", what, p.Spread)
	}
	return nil
}

// painter evaluates a paint at device pixel centres.
type painter struct {
	solid  Color
	grad   bool
	lut    [256]Color
	inv    Affine // device -> gradient space
	radial bool
	// linear: t = ((p-p1).v) / |v|^2 ; radial: t = |p-c| / r
	x1, y1, vx, vy, vv float64
	cx, cy, r          float64
	spread             int // 0 pad 1 reflect 2 repeat
}

// newPainter prepares a paint; ctm maps the paint's space to device pixels.
// ok is false when nothing would be drawn (degenerate gradient).
func newPainter(p *Paint, ctm Affine) (painter, bool) {
	var pt painter
	switch p.Kind {
	case PaintSolid:
		pt.solid = p.Color
		return pt, p.Color.A > 0
	case PaintLinear, PaintRadial:
	default:
		return pt, false
	}
	inv, ok := ctm.Invert()
	if !ok {
		return pt, false
	}
	pt.inv = inv
	pt.grad = true
	stops := append([]Stop(nil), p.Stops...)
	sort.SliceStable(stops, func(i, j int) bool { return stops[i].Offset < stops[j].Offset })
	for i := range pt.lut {
		t := float64(i) / 255
		pt.lut[i] = stopColor(stops, t)
	}
	switch p.Spread {
	case "reflect":
		pt.spread = 1
	case "repeat":
		pt.spread = 2
	}
	if p.Kind == PaintLinear {
		pt.x1, pt.y1 = p.X1, p.Y1
		pt.vx, pt.vy = p.X2-p.X1, p.Y2-p.Y1
		pt.vv = mul(pt.vx, pt.vx) + mul(pt.vy, pt.vy)
		if pt.vv == 0 {
			// SVG: a zero-length vector paints the last stop
			pt.solid = stops[len(stops)-1].Color
			pt.grad = false
			return pt, pt.solid.A > 0
		}
	} else {
		pt.radial = true
		pt.cx, pt.cy, pt.r = p.CX, p.CY, p.R
		if pt.r <= 0 {
			pt.solid = stops[len(stops)-1].Color
			pt.grad = false
			return pt, pt.solid.A > 0
		}
	}
	return pt, true
}

func stopColor(stops []Stop, t float64) Color {
	if t <= stops[0].Offset {
		return stops[0].Color
	}
	last := stops[len(stops)-1]
	if t >= last.Offset {
		return last.Color
	}
	for i := 1; i < len(stops); i++ {
		if t <= stops[i].Offset {
			a, b := stops[i-1], stops[i]
			d := b.Offset - a.Offset
			if d <= 0 {
				return b.Color
			}
			f := (t - a.Offset) / d
			// interpolate in premultiplied space so transparent stops do not tint
			mix := func(x, y uint8, ax, ay uint8) float64 {
				return mul(mul(float64(x), float64(ax)), 1-f) + mul(mul(float64(y), float64(ay)), f)
			}
			al := mul(float64(a.Color.A), 1-f) + mul(float64(b.Color.A), f)
			if al <= 0 {
				return Color{}
			}
			ch := func(x, y uint8) uint8 {
				v := mix(x, y, a.Color.A, b.Color.A) / al
				return uint8(math.Min(255, math.Max(0, v+0.5)))
			}
			return Color{ch(a.Color.R, b.Color.R), ch(a.Color.G, b.Color.G), ch(a.Color.B, b.Color.B), uint8(al + 0.5)}
		}
	}
	return last.Color
}

// at returns the colour at device pixel (x, y).
func (p *painter) at(x, y int) Color {
	if !p.grad {
		return p.solid
	}
	ux, uy := p.inv.Apply(float64(x)+0.5, float64(y)+0.5)
	var t float64
	if p.radial {
		t = hyp(ux-p.cx, uy-p.cy) / p.r
	} else {
		t = (mul(ux-p.x1, p.vx) + mul(uy-p.y1, p.vy)) / p.vv
	}
	switch p.spread {
	case 1:
		t = math.Mod(math.Abs(t), 2)
		if t > 1 {
			t = 2 - t
		}
	case 2:
		t -= math.Floor(t)
	default:
		if t < 0 {
			t = 0
		} else if t > 1 {
			t = 1
		}
	}
	if !(t >= 0 && t <= 1) {
		t = 0 // NaN from an overflowing reflect/repeat
	}
	return p.lut[int(mul(t, 255)+0.5)]
}
