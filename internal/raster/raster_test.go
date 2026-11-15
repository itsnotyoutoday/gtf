// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package raster

import (
	"bytes"
	"errors"
	"math"
	"testing"
)

func mustSprite(t testing.TB, js string) *Sprite {
	t.Helper()
	s, err := ParseJSON([]byte(js), nil)
	if err != nil {
		t.Fatalf("%v\n%s", err, js)
	}
	return s
}

func mustRender(t testing.TB, js string, w, h int) *Buffer {
	t.Helper()
	b, err := mustSprite(t, js).Render(w, h, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func alphaSum(b *Buffer) float64 {
	sum := 0.0
	for i := 3; i < len(b.Pix); i += 4 {
		sum += float64(b.Pix[i]) / 255
	}
	return sum
}

func near(t *testing.T, what string, got, want, tol float64) {
	t.Helper()
	if math.Abs(got-want) > tol {
		t.Errorf("%s = %g, want %g +- %g", what, got, want, tol)
	}
}

func TestParsePath(t *testing.T) {
	ok := map[string]int{
		"M0 0L10 0 10 10z":               4,
		"M0,0 l10,0 0,10 z":              4,
		"M10-5.5.5 20":                   2, // compact numbers: 10, -5.5 | .5, 20 (an implicit lineto)
		"M0 0 H10 V10 h-10 v-10":         5,
		"M0 0 C1 1 2 2 3 3 S5 5 6 6":     3,
		"M0 0 Q5 5 10 0 T20 0":           3,
		"M0 0 A5 5 0 0 1 10 0":           3, // half circle: two quarter cubics
		"M0 0 a5 5 0 1010 0":             3, // flags without separators
		"M1e1 2E+1 L-1.5e-1 0":           2,
		"M0 0 L5 5 Z M10 10 L20 20 Z":    6,
		"  M 0 0 \n L 1 1 ":              2,
		"M0 0 L10 0 L10 10 L0 10 Z L5 5": 6,
		"M0 0 A0 5 0 0 1 10 0":           2, // zero radius: a straight line
		"M0 0 A1 1 0 0 1 10 0":           3, // radius too small: scaled up
		"M0 0 L1 1 m5 5 l1 1":            4,
	}
	for d, n := range ok {
		p, err := ParsePath(d, nil)
		if err != nil {
			t.Errorf("%q: %v", d, err)
			continue
		}
		if p.Len() != n {
			t.Errorf("%q: %d commands, want %d", d, p.Len(), n)
		}
	}
	for _, d := range []string{"", "L10 10", "M", "M0", "M0 0 L", "M0 0 X5", "M0 0 A1 1 0 2 0 5 5", "M1e999 0", "M0 0 L abc", "M0 0 Z 5"} {
		if _, err := ParsePath(d, nil); !errors.Is(err, ErrInvalid) && !errors.Is(err, ErrLimit) {
			t.Errorf("%q: want an error, got %v", d, err)
		}
	}
}

func TestArcGeometry(t *testing.T) {
	p, err := ParsePath("M0 0 A5 5 0 0 1 10 0", nil)
	if err != nil {
		t.Fatal(err)
	}
	var bud budget
	bud.left = 1 << 20
	sp, err := flatten(p, 0.001, &bud)
	if err != nil {
		t.Fatal(err)
	}
	// every flattened point is on the circle centred (5, 0) radius 5, and the arc bulges to y<0 (sweep=1, y down => clockwise from left to right goes up)
	for _, v := range sp[0].pts {
		near(t, "radius", math.Hypot(v.x-5, v.y), 5, 0.01)
	}
	last := sp[0].pts[len(sp[0].pts)-1]
	near(t, "end x", last.x, 10, 1e-9)
	near(t, "end y", last.y, 0, 1e-9)
	minY := 0.0
	for _, v := range sp[0].pts {
		minY = math.Min(minY, v.y)
	}
	near(t, "bulge", minY, -5, 0.02)
}

func TestTransformParse(t *testing.T) {
	m, err := ParseTransform("translate(10 20) scale(2)", 1e6)
	if err != nil {
		t.Fatal(err)
	}
	x, y := m.Apply(1, 1)
	near(t, "x", x, 12, 1e-9)
	near(t, "y", y, 22, 1e-9)
	m, _ = ParseTransform("rotate(90 5 5)", 1e6)
	x, y = m.Apply(10, 5)
	near(t, "rot x", x, 5, 1e-9)
	near(t, "rot y", y, 10, 1e-9)
	for _, bad := range []string{"translate(", "foo(1)", "scale()", "rotate(1 2)", "translate(NaN)", "translate(1e30)", "matrix(1 2 3)"} {
		if _, err := ParseTransform(bad, 1e6); err == nil {
			t.Errorf("%q should fail", bad)
		}
	}
}

func TestRectCoverage(t *testing.T) {
	// viewBox equals pixels: a rect from 2 to 6.5 horizontally, 1 to 3 vertically
	b := mustRender(t, `{"viewBox":[0,0,10,5],"layers":[{"shapes":[{"type":"rect","x":2,"y":1,"w":4.5,"h":2,"fill":"#ff0000"}]}]}`, 10, 5)
	for y := 0; y < 5; y++ {
		for x := 0; x < 10; x++ {
			c := b.At(x, y)
			switch {
			case x >= 2 && x <= 5 && y >= 1 && y <= 2:
				if c != (Color{255, 0, 0, 255}) {
					t.Errorf("(%d,%d) = %v, want solid red", x, y, c)
				}
			case x == 6 && y >= 1 && y <= 2:
				if c.A < 126 || c.A > 129 || c.R != 255 {
					t.Errorf("(%d,%d) = %v, want half coverage", x, y, c)
				}
			default:
				if c.A != 0 {
					t.Errorf("(%d,%d) = %v, want empty", x, y, c)
				}
			}
		}
	}
}

func TestCircleAndEllipseArea(t *testing.T) {
	b := mustRender(t, `{"viewBox":[0,0,100,100],"layers":[{"shapes":[{"type":"circle","cx":50,"cy":50,"r":40,"fill":"#fff"}]}]}`, 100, 100)
	near(t, "circle area", alphaSum(b), math.Pi*40*40, math.Pi*1600*0.005)
	b = mustRender(t, `{"viewBox":[0,0,100,100],"layers":[{"shapes":[{"type":"ellipse","cx":50,"cy":50,"rx":30,"ry":15,"fill":"#fff"}]}]}`, 100, 100)
	near(t, "ellipse area", alphaSum(b), math.Pi*30*15, math.Pi*450*0.006)
	// at 5x the pixel size the area scales by 25
	b = mustRender(t, `{"viewBox":[0,0,100,100],"layers":[{"shapes":[{"type":"circle","cx":50,"cy":50,"r":40,"fill":"#fff"}]}]}`, 500, 500)
	near(t, "scaled circle area", alphaSum(b), math.Pi*40*40*25, math.Pi*1600*25*0.005)
}

func TestFillRules(t *testing.T) {
	sq := `"d":"M10 10 H90 V90 H10 Z M30 30 H70 V70 H30 Z"`
	nz := mustRender(t, `{"viewBox":[0,0,100,100],"layers":[{"shapes":[{"type":"path",`+sq+`,"fill":"#fff"}]}]}`, 100, 100)
	eo := mustRender(t, `{"viewBox":[0,0,100,100],"layers":[{"shapes":[{"type":"path",`+sq+`,"fillRule":"evenodd","fill":"#fff"}]}]}`, 100, 100)
	// both squares wind the same way: non-zero fills the hole, even-odd leaves it
	near(t, "nonzero", alphaSum(nz), 80*80, 1)
	near(t, "evenodd", alphaSum(eo), 80*80-40*40, 1)
	if eo.At(50, 50).A != 0 || nz.At(50, 50).A != 255 {
		t.Error("hole not as expected")
	}
}

func TestStrokes(t *testing.T) {
	line := func(extra string) float64 {
		b := mustRender(t, `{"viewBox":[0,0,100,40],"layers":[{"shapes":[{"type":"line","x1":20,"y1":20,"x2":80,"y2":20,"stroke":{"paint":"#fff","width":6`+extra+`}}]}]}`, 100, 40)
		return alphaSum(b)
	}
	near(t, "butt", line(""), 60*6, 2)
	near(t, "square", line(`,"cap":"square"`), 66*6, 2)
	near(t, "round", line(`,"cap":"round"`), 60*6+math.Pi*9, 2.5)
	// joins on an L shape: legs 40 long, width 6; bevel cuts the outer corner, miter fills it
	elbow := func(join string) float64 {
		b := mustRender(t, `{"viewBox":[0,0,100,100],"layers":[{"shapes":[{"type":"polyline","points":[20,20,60,20,60,60],"fill":"none","stroke":{"paint":"#fff","width":6,"join":"`+join+`"}}]}]}`, 100, 100)
		return alphaSum(b)
	}
	miter, bevel, round := elbow("miter"), elbow("bevel"), elbow("round")
	near(t, "miter", miter, 40*6+40*6-9+9, 2.5) // two 40x6 legs, minus the 3x3 overlap, plus the 3x3 mitred corner
	if !(miter > round && round > bevel) {
		t.Errorf("join areas should order miter %g > round %g > bevel %g", miter, round, bevel)
	}
	// a miter limit below the corner's ratio falls back to a bevel
	lim := mustRender(t, `{"viewBox":[0,0,100,100],"layers":[{"shapes":[{"type":"polyline","points":[20,20,60,20,60,60],"fill":"none","stroke":{"paint":"#fff","width":6,"join":"miter","miterLimit":1.2}}]}]}`, 100, 100)
	near(t, "miter limit", alphaSum(lim), bevel, 1)
}

func TestGradients(t *testing.T) {
	b := mustRender(t, `{"viewBox":[0,0,100,10],"layers":[{"shapes":[{"type":"rect","x":0,"y":0,"w":100,"h":10,
		"fill":{"type":"linear","x1":0,"y1":0,"x2":100,"y2":0,"stops":[[0,"#000000"],[1,"#ff0000"]]}}]}]}`, 100, 10)
	for _, c := range []struct {
		x int
		r float64
	}{{0, 1.3}, {49, 127}, {99, 253.7}} {
		near(t, "linear red", float64(b.At(c.x, 5).R), c.r, 3)
	}
	r := mustRender(t, `{"viewBox":[0,0,100,100],"layers":[{"shapes":[{"type":"rect","x":0,"y":0,"w":100,"h":100,
		"fill":{"type":"radial","cx":50,"cy":50,"r":50,"stops":[[0,"#ffffff"],[1,"#000000"]]}}]}]}`, 100, 100)
	near(t, "radial centre", float64(r.At(50, 50).R), 255, 6)
	near(t, "radial mid", float64(r.At(75, 50).R), 127, 6)
	near(t, "radial outside is last stop", float64(r.At(2, 2).R), 0, 1)
	// stops given as objects, unsorted, and reflect spread
	o := mustRender(t, `{"viewBox":[0,0,100,10],"layers":[{"shapes":[{"type":"rect","x":0,"y":0,"w":100,"h":10,
		"fill":{"type":"linear","x1":0,"y1":0,"x2":50,"y2":0,"spread":"reflect","stops":[{"offset":1,"color":"#ff0000"},{"offset":0,"color":"#000000"}]}}]}]}`, 100, 10)
	near(t, "reflect at 25", float64(o.At(25, 5).R), 127, 4)
	near(t, "reflect at 75", float64(o.At(75, 5).R), 127, 4)
	near(t, "reflect at 50", float64(o.At(50, 5).R), 255, 6)
}

func TestOpacityLayersGroupsClip(t *testing.T) {
	// two overlapping half-opaque squares: shape opacity composes each; a group composes the union
	js := func(body string) *Buffer {
		return mustRender(t, `{"viewBox":[0,0,20,10],"layers":[{"shapes":[`+body+`]}]}`, 20, 10)
	}
	sq := func(x float64, extra string) string {
		return `{"type":"rect","x":` + ftoa(x) + `,"y":0,"w":10,"h":10,"fill":"#ffffff"` + extra + `}`
	}
	per := js(sq(0, `,"opacity":0.5`) + "," + sq(5, `,"opacity":0.5`))
	grp := js(`{"type":"group","opacity":0.5,"children":[` + sq(0, "") + "," + sq(5, "") + `]}`)
	near(t, "per-shape overlap alpha", float64(per.At(7, 5).A), 191, 2) // 1-(.5*.5)
	near(t, "group overlap alpha", float64(grp.At(7, 5).A), 128, 2)
	near(t, "single", float64(per.At(2, 5).A), 128, 2)
	// layer opacity
	l := mustRender(t, `{"viewBox":[0,0,20,10],"layers":[{"opacity":0.25,"shapes":[`+sq(0, "")+`]}]}`, 20, 10)
	near(t, "layer opacity", float64(l.At(2, 5).A), 64, 2)
	// clip: only the part of the big rect inside the circle remains
	c := mustRender(t, `{"viewBox":[0,0,100,100],"layers":[{"shapes":[{"type":"rect","x":0,"y":0,"w":100,"h":100,"fill":"#fff",
		"clip":{"type":"circle","cx":50,"cy":50,"r":30}}]}]}`, 100, 100)
	near(t, "clipped area", alphaSum(c), math.Pi*900, 12)
	// layer clip with a transform: clip coordinates follow the layer
	c = mustRender(t, `{"viewBox":[0,0,100,100],"layers":[{"transform":"translate(20 0)","clip":{"type":"rect","x":0,"y":0,"w":10,"h":100},"shapes":[{"type":"rect","x":0,"y":0,"w":100,"h":100,"fill":"#fff"}]}]}`, 100, 100)
	if c.At(25, 50).A != 255 || c.At(15, 50).A != 0 || c.At(35, 50).A != 0 {
		t.Error("layer clip did not follow the layer transform")
	}
	// hidden things are skipped
	h := mustRender(t, `{"viewBox":[0,0,20,10],"layers":[{"hidden":true,"shapes":[`+sq(0, "")+`]},{"shapes":[`+sq(0, `,"hidden":true`)+`]}]}`, 20, 10)
	if alphaSum(h) != 0 {
		t.Error("hidden content drawn")
	}
}

func ftoa(f float64) string { return formatFloat(f) }

func TestTransforms(t *testing.T) {
	// a 10x2 bar rotated 90 degrees about the middle of the target becomes 2x10
	b := mustRender(t, `{"viewBox":[0,0,20,20],"layers":[{"shapes":[{"type":"rect","x":5,"y":9,"w":10,"h":2,"fill":"#fff","transform":"rotate(90 10 10)"}]}]}`, 20, 20)
	if b.At(10, 5).A != 255 || b.At(9, 14).A != 255 || b.At(5, 10).A != 0 {
		t.Error("rotated bar is not vertical")
	}
	near(t, "area", alphaSum(b), 20, 0.5)
	// non-uniform scale keeps strokes correct in user space: width 2 horizontally becomes 6 wide when x is scaled by 3
	b = mustRender(t, `{"viewBox":[0,0,60,20],"layers":[{"shapes":[{"type":"line","x1":5,"y1":2,"x2":5,"y2":18,"transform":"scale(3 1)","stroke":{"paint":"#fff","width":2}}]}]}`, 60, 20)
	near(t, "scaled stroke area", alphaSum(b), 6*16, 1)
}

func TestViewBoxFit(t *testing.T) {
	// a 2:1 viewBox in a 40x40 target
	mk := func(pa string) *Buffer {
		return mustRender(t, `{"viewBox":[0,0,20,10],"preserveAspect":"`+pa+`","layers":[{"shapes":[{"type":"rect","x":0,"y":0,"w":20,"h":10,"fill":"#fff"}]}]}`, 40, 40)
	}
	meet := mk("xMidYMid meet")
	if meet.At(20, 5).A != 0 || meet.At(20, 15).A != 255 || meet.At(20, 25).A != 255 || meet.At(20, 35).A != 0 {
		t.Error("meet should letterbox vertically and centre")
	}
	near(t, "meet area", alphaSum(meet), 40*20, 1)
	top := mk("xMidYMin meet")
	if top.At(20, 2).A != 255 || top.At(20, 22).A != 0 {
		t.Error("YMin should align to the top")
	}
	slice := mk("xMidYMid slice")
	near(t, "slice fills", alphaSum(slice), 40*40, 1)
	none := mk("none")
	near(t, "none stretches", alphaSum(none), 40*40, 1)
	// non-square pixels (aspect 2: pixels twice as tall as wide): a circle stays round in physical space
	s := mustSprite(t, `{"viewBox":[0,0,100,100],"layers":[{"shapes":[{"type":"circle","cx":50,"cy":50,"r":40,"fill":"#fff"}]}]}`)
	b, err := s.Render(40, 20, &Params{PixelAspect: 2}, nil)
	if err != nil {
		t.Fatal(err)
	}
	near(t, "circle area in pixels", alphaSum(b), math.Pi*16*16/2, 8) // radius 16 physical units, a pixel is 2 units
}

func TestZonesAndGlows(t *testing.T) {
	s := mustSprite(t, `{"viewBox":[0,0,100,100],
		"layers":[{"shapes":[{"type":"rect","x":10,"y":10,"w":80,"h":80,"fill":"#3366ff"},
		   {"type":"rect","x":10,"y":10,"w":30,"h":80,"fill":"none","zone":"left"}]}],
		"zones":[{"id":"left","color":"#ff0000ff"},{"id":"top","color":"#00ff00ff","shapes":[{"type":"rect","x":0,"y":0,"w":100,"h":30}]}],
		"glows":[{"id":"lamp","x":80,"y":80,"r":10,"color":"#ffffff"}]}`)
	base, _ := s.Render(100, 100, nil, nil)
	if c := base.At(20, 50); c != (Color{0x33, 0x66, 0xff, 255}) {
		t.Fatalf("base colour %v", c)
	}
	// the glow is on by default; params switch it off
	if base.At(80, 80) == (Color{0x33, 0x66, 0xff, 255}) {
		t.Error("default glow not drawn")
	}
	off, _ := s.Render(100, 100, &Params{Glows: map[string]float64{"lamp": 0}}, nil)
	if off.At(80, 80) != (Color{0x33, 0x66, 0xff, 255}) {
		t.Error("glow did not switch off")
	}
	z0, _ := s.Render(100, 100, &Params{Zones: map[string]float64{"left": 0}, Glows: map[string]float64{"lamp": 0}}, nil)
	if !bytes.Equal(z0.Pix, off.Pix) {
		t.Error("zone at 0 must not change the picture")
	}
	half, _ := s.Render(100, 100, &Params{Zones: map[string]float64{"left": 0.5}, Glows: map[string]float64{"lamp": 0}}, nil)
	c := half.At(20, 50)
	near(t, "half tint R", float64(c.R), 0x33*0.5+255*0.5, 2)
	near(t, "half tint B", float64(c.B), 255*0.5, 2)
	if half.At(60, 50) != (Color{0x33, 0x66, 0xff, 255}) {
		t.Error("tint leaked outside the zone")
	}
	full, _ := s.Render(100, 100, &Params{Zones: map[string]float64{"left": 1, "top": 1}, Glows: map[string]float64{"lamp": 0}}, nil)
	if full.At(20, 50) != (Color{255, 0, 0, 255}) {
		t.Errorf("full tint = %v", full.At(20, 50))
	}
	// zone region outside the drawn sprite stays transparent (source-atop)
	if full.At(5, 5).A != 0 {
		t.Error("zone tint painted outside the sprite")
	}
	if full.At(50, 20) != (Color{0, 255, 0, 255}) {
		t.Errorf("explicit zone shape tint = %v", full.At(50, 20))
	}
}

func TestDeterministic(t *testing.T) {
	for _, n := range []string{"saucer", "warbird", "planet"} {
		s := loadSprite(t, n)
		a, _ := s.Render(97, 61, nil, nil)
		b, _ := loadSprite(t, n).Render(97, 61, nil, nil)
		if !bytes.Equal(a.Pix, b.Pix) {
			t.Errorf("%s: two renders differ", n)
		}
	}
}

func TestSamplesImproveEdges(t *testing.T) {
	s := mustSprite(t, `{"viewBox":[0,0,100,100],"layers":[{"shapes":[{"type":"polygon","points":[0,0,100,13,0,26],"fill":"#fff"}]}]}`)
	var errs []float64
	for _, n := range []int{4, 32} {
		b, err := s.Render(100, 100, &Params{Samples: n}, nil)
		if err != nil {
			t.Fatal(err)
		}
		errs = append(errs, math.Abs(alphaSum(b)-1300))
	}
	if errs[1] > errs[0]+0.001 {
		t.Errorf("more samples got worse: %v", errs)
	}
	if _, err := s.Render(10, 10, &Params{Samples: 5}, nil); err == nil {
		t.Error("odd sample count should be rejected")
	}
}

func TestSpriteJSONRoundTrip(t *testing.T) {
	s := loadSprite(t, "warbird")
	b, err := jsonMarshal(s)
	if err != nil {
		t.Fatal(err)
	}
	s2, err := ParseJSON(b, nil)
	if err != nil {
		t.Fatal(err)
	}
	if s.Hash() != s2.Hash() {
		t.Error("hash changed through a JSON round trip")
	}
	x, _ := s.Render(50, 50, nil, nil)
	y, _ := s2.Render(50, 50, nil, nil)
	if !bytes.Equal(x.Pix, y.Pix) {
		t.Error("render changed through a JSON round trip")
	}
}
