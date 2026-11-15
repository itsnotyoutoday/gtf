// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package raster

import (
	"bytes"
	"encoding/json"
	"hash/fnv"
	"strings"
	"sync"
)

// Sprite is a vector picture: layers of shapes in a viewBox, plus named
// zones (regions that can be tinted at render time) and glow anchors
// (lights that can be switched on and off). It is the format-agnostic input
// of the rasteriser; the JSON form below is deliberately simple so it can be
// mapped from whatever the final CMCP sprite format turns out to be.
//
// JSON document:
//
//	{
//	  "viewBox": [0, 0, 100, 60],              // x y w h, default [0 0 100 100]
//	  "preserveAspect": "xMidYMid meet",       // SVG syntax: xMin|xMid|xMax, YMin|YMid|YMax, meet|slice, or "none"
//	  "layers": [{
//	    "id": "hull", "opacity": 1, "hidden": false,
//	    "transform": "translate(5 0) rotate(10)",   // SVG list; functions apply right to left
//	    "clip": { shape },                          // optional; the layer is cut to this shape
//	    "shapes": [ { shape }, ... ]
//	  }],
//	  "zones": [{ "id": "port-wing", "color": "#ff3020aa", "shapes": [ { shape } ] }],
//	  "glows": [{ "id": "engine", "x": 50, "y": 55, "r": 12, "color": "#6cf", "intensity": 1 }]
//	}
//
// A shape is one of (type: fields)
//
//	path      d "M0 0 L10 0 ... Z" (SVG path data: M L H V C S Q T A Z, absolute and relative)
//	polygon   points [x1,y1,x2,y2,...]      polyline  points [...]     line  x1 y1 x2 y2
//	rect      x y w h [rx ry]               ellipse   cx cy rx ry      circle  cx cy r
//	group     children [ { shape } ... ]    (opacity, transform and clip apply to the whole group)
//
// with the common fields
//
//	fill        paint, default solid black unless "stroke" is given; "none" for no fill
//	fillRule    "nonzero" (default) | "evenodd"
//	stroke      {"paint": paint, "width": 1, "cap": "butt|round|square", "join": "miter|round|bevel", "miterLimit": 4}
//	opacity     0..1 (default 1; set "hidden": true to skip a shape; applied per paint, wrap in a group for group opacity)
//	transform   "translate(x y) rotate(deg [cx cy]) scale(s | sx sy) matrix(a b c d e f)"
//	clip        a shape; only the part inside it is drawn. Its coordinates are those of the clipped element, transform included
//	zone        id of a zone this shape belongs to (see below); optional
//	id          free text
//
// A paint is "#rgb" | "#rrggbb" | "#rrggbbaa" | a few colour names | "none" |
// {"type":"linear","x1","y1","x2","y2","stops":[[0,"#fff"],[1,"#000"]],"spread":"pad|reflect|repeat"} |
// {"type":"radial","cx","cy","r","stops":[...]}; gradient coordinates are in the
// shape's own coordinates (after its transform).
//
// Zones: when Params.Zones[id] is greater than 0 the union of the zone's own
// shapes and of every shape tagged with "zone": id is tinted toward the zone
// colour by value*alpha, over the finished sprite and only where the sprite is
// drawn (a damage overlay). Glows: each glow is a radial additive light drawn on
// top; Params.Glows[id] sets its intensity (0 = off; absent = the glow's own
// intensity, default 1). Colours use straight 8-bit alpha.
//
// Treat a Sprite as immutable once it has been rendered or cached.
type Sprite struct {
	ViewBox        [4]float64 `json:"viewBox,omitempty"`
	PreserveAspect string     `json:"preserveAspect,omitempty"`
	Layers         []Layer    `json:"layers"`
	Zones          []Zone     `json:"zones,omitempty"`
	Glows          []Glow     `json:"glows,omitempty"`

	once sync.Once
	prep error
	hash uint64
	hOne sync.Once
	fit  fitSpec
}

// Layer is a drawing group with its own opacity, transform and clip.
type Layer struct {
	ID        string  `json:"id,omitempty"`
	Opacity   float64 `json:"opacity,omitempty"` // 0 means 1
	Hidden    bool    `json:"hidden,omitempty"`
	Transform string  `json:"transform,omitempty"`
	Clip      *Shape  `json:"clip,omitempty"`
	Shapes    []Shape `json:"shapes"`

	ctm Affine
}

// Shape types.
const (
	TypePath     = "path"
	TypePolygon  = "polygon"
	TypePolyline = "polyline"
	TypeLine     = "line"
	TypeRect     = "rect"
	TypeEllipse  = "ellipse"
	TypeCircle   = "circle"
	TypeGroup    = "group"
)

// Shape is one drawable. Only the fields of its Type are read.
type Shape struct {
	Type      string    `json:"type"`
	ID        string    `json:"id,omitempty"`
	D         string    `json:"d,omitempty"`
	Points    []float64 `json:"points,omitempty"`
	X         float64   `json:"x,omitempty"`
	Y         float64   `json:"y,omitempty"`
	W         float64   `json:"w,omitempty"`
	H         float64   `json:"h,omitempty"`
	RX        float64   `json:"rx,omitempty"`
	RY        float64   `json:"ry,omitempty"`
	CX        float64   `json:"cx,omitempty"`
	CY        float64   `json:"cy,omitempty"`
	R         float64   `json:"r,omitempty"`
	X1        float64   `json:"x1,omitempty"`
	Y1        float64   `json:"y1,omitempty"`
	X2        float64   `json:"x2,omitempty"`
	Y2        float64   `json:"y2,omitempty"`
	Fill      *Paint    `json:"fill,omitempty"`
	FillRule  string    `json:"fillRule,omitempty"`
	Stroke    *Stroke   `json:"stroke,omitempty"`
	Opacity   float64   `json:"opacity,omitempty"` // 0 means 1
	Hidden    bool      `json:"hidden,omitempty"`
	Transform string    `json:"transform,omitempty"`
	Clip      *Shape    `json:"clip,omitempty"`
	Zone      string    `json:"zone,omitempty"`
	Children  []Shape   `json:"children,omitempty"`

	path *Path  // parsed form of D, or the primitive converted to a path
	tf   Affine // parsed Transform
}

// Stroke describes an outline.
type Stroke struct {
	Paint      *Paint  `json:"paint,omitempty"` // default solid black
	Width      float64 `json:"width,omitempty"` // 0 means 1
	Cap        string  `json:"cap,omitempty"`
	Join       string  `json:"join,omitempty"`
	MiterLimit float64 `json:"miterLimit,omitempty"` // 0 means 4
}

// Zone is a named region with a tint colour.
type Zone struct {
	ID     string  `json:"id"`
	Color  Color   `json:"color,omitempty"` // default #ff3030b4
	Shapes []Shape `json:"shapes,omitempty"`
}

// Glow is a named light anchor.
type Glow struct {
	ID        string  `json:"id"`
	X         float64 `json:"x"`
	Y         float64 `json:"y"`
	R         float64 `json:"r"`
	Color     Color   `json:"color"`
	Intensity float64 `json:"intensity,omitempty"` // default intensity, 0 means 1
}

// Params are the per-render inputs that are not part of the art.
type Params struct {
	Zones       map[string]float64 // zone id -> tint amount 0..1
	Glows       map[string]float64 // glow id -> intensity 0..1 (0 = off)
	PixelAspect float64            // pixel height / width; 0 means 1 (square pixels)
	Samples     int                // vertical sub-scanlines per pixel: 4, 8, 16 or 32 (default 8)
}

// ParseJSON decodes and validates a JSON sprite. Unknown fields are ignored so
// documents can carry extra metadata. Errors wrap ErrInvalid or ErrLimit.
func ParseJSON(data []byte, lim *Limits) (*Sprite, error) {
	l := limitsOrDefault(lim)
	if len(data) > l.MaxJSONBytes {
		return nil, limitErr("document is %d bytes (max %d)", len(data), l.MaxJSONBytes)
	}
	if d := jsonDepth(data); d > l.MaxDepth*3+8 {
		return nil, limitErr("JSON nesting %d too deep", d)
	}
	var s Sprite
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&s); err != nil {
		return nil, invalidErr("bad JSON: %v", err)
	}
	if dec.More() {
		return nil, invalidErr("trailing data after the JSON document")
	}
	if err := s.Prepare(&l); err != nil {
		return nil, err
	}
	return &s, nil
}

// jsonDepth is the maximum bracket nesting, ignoring brackets inside strings.
func jsonDepth(b []byte) int {
	depth, max := 0, 0
	inStr, esc := false, false
	for _, c := range b {
		switch {
		case esc:
			esc = false
		case inStr:
			if c == '\\' {
				esc = true
			} else if c == '"' {
				inStr = false
			}
		case c == '"':
			inStr = true
		case c == '{' || c == '[':
			depth++
			if depth > max {
				max = depth
			}
		case c == '}' || c == ']':
			depth--
		}
	}
	return max
}

// Prepare validates the sprite against the limits and parses its paths and
// transforms. It runs once; Render calls it for you. Go-built sprites should
// not be modified after Prepare.
func (s *Sprite) Prepare(lim *Limits) error {
	s.once.Do(func() {
		l := limitsOrDefault(lim)
		s.prep = s.prepare(&l)
	})
	return s.prep
}

type prepState struct {
	l      *Limits
	shapes int
}

func (s *Sprite) prepare(l *Limits) error {
	if s.ViewBox == ([4]float64{}) {
		s.ViewBox = [4]float64{0, 0, 100, 100}
	}
	for _, v := range s.ViewBox {
		if err := checkNum("viewBox", v, l.MaxCoord); err != nil {
			return err
		}
	}
	if s.ViewBox[2] <= 0 || s.ViewBox[3] <= 0 {
		return invalidErr("viewBox width and height must be positive")
	}
	f, err := parseFit(s.PreserveAspect)
	if err != nil {
		return err
	}
	s.fit = f
	if len(s.Layers) > l.MaxLayers {
		return limitErr("%d layers (max %d)", len(s.Layers), l.MaxLayers)
	}
	if len(s.Zones)+len(s.Glows) > l.MaxZones {
		return limitErr("%d zones and glows (max %d)", len(s.Zones)+len(s.Glows), l.MaxZones)
	}
	st := &prepState{l: l}
	for i := range s.Layers {
		ly := &s.Layers[i]
		if err := checkOpacity("layer opacity", ly.Opacity); err != nil {
			return err
		}
		if ly.ctm, err = ParseTransform(ly.Transform, l.MaxCoord); err != nil {
			return err
		}
		if ly.Clip != nil {
			if err := st.shape(ly.Clip, 1, true); err != nil {
				return err
			}
		}
		for j := range ly.Shapes {
			if err := st.shape(&ly.Shapes[j], 1, false); err != nil {
				return err
			}
		}
	}
	for i := range s.Zones {
		z := &s.Zones[i]
		if z.ID == "" {
			return invalidErr("zone without an id")
		}
		for j := range z.Shapes {
			if err := st.shape(&z.Shapes[j], 1, true); err != nil {
				return err
			}
		}
	}
	for i := range s.Glows {
		g := &s.Glows[i]
		if g.ID == "" {
			return invalidErr("glow without an id")
		}
		for _, v := range []float64{g.X, g.Y, g.R, g.Intensity} {
			if err := checkNum("glow value", v, l.MaxCoord); err != nil {
				return err
			}
		}
		if g.R < 0 {
			return invalidErr("glow radius is negative")
		}
	}
	return nil
}

func checkOpacity(what string, v float64) error {
	if !finite(v) || v < 0 || v > 1 {
		return invalidErr("%s must be within 0..1", what)
	}
	return nil
}

// shape validates one shape (recursively) and parses its geometry. geomOnly
// shapes (clips, zone regions) need no paint.
func (st *prepState) shape(sh *Shape, depth int, geomOnly bool) error {
	l := st.l
	if depth > l.MaxDepth {
		return limitErr("shapes nested deeper than %d", l.MaxDepth)
	}
	if st.shapes++; st.shapes > l.MaxShapes {
		return limitErr("more than %d shapes", l.MaxShapes)
	}
	if err := checkOpacity("opacity", sh.Opacity); err != nil {
		return err
	}
	var err error
	if sh.tf, err = ParseTransform(sh.Transform, l.MaxCoord); err != nil {
		return err
	}
	for _, v := range []float64{sh.X, sh.Y, sh.W, sh.H, sh.RX, sh.RY, sh.CX, sh.CY, sh.R, sh.X1, sh.Y1, sh.X2, sh.Y2} {
		if err := checkNum(sh.Type+" value", v, l.MaxCoord); err != nil {
			return err
		}
	}
	if sh.Fill != nil {
		if err := sh.Fill.validate(l, "fill"); err != nil {
			return err
		}
	}
	switch sh.FillRule {
	case "", NonZero, EvenOdd:
	default:
		return invalidErr("unknown fillRule %q", sh.FillRule)
	}
	if sh.Stroke != nil {
		sk := sh.Stroke
		if err := checkNum("stroke width", sk.Width, l.MaxCoord); err != nil {
			return err
		}
		if err := checkNum("miterLimit", sk.MiterLimit, l.MaxCoord); err != nil {
			return err
		}
		if sk.Width < 0 {
			return invalidErr("negative stroke width")
		}
		switch sk.Cap {
		case "", CapButt, CapRound, CapSquare:
		default:
			return invalidErr("unknown stroke cap %q", sk.Cap)
		}
		switch sk.Join {
		case "", JoinMiter, JoinRound, JoinBevel:
		default:
			return invalidErr("unknown stroke join %q", sk.Join)
		}
		if sk.Paint != nil {
			if err := sk.Paint.validate(l, "stroke"); err != nil {
				return err
			}
		}
	}
	if sh.Clip != nil {
		if err := st.shape(sh.Clip, depth+1, true); err != nil {
			return err
		}
	}
	np := newPath(*l)
	switch sh.Type {
	case TypeGroup:
		for i := range sh.Children {
			if err := st.shape(&sh.Children[i], depth+1, geomOnly); err != nil {
				return err
			}
		}
		return nil
	case TypePath:
		p, err := ParsePath(sh.D, l)
		if err != nil {
			return err
		}
		sh.path = p
		return nil
	case TypePolygon, TypePolyline:
		if len(sh.Points)%2 != 0 || len(sh.Points) < 4 {
			return invalidErr("%s needs at least two x,y pairs", sh.Type)
		}
		if len(sh.Points)/2 > l.MaxPoints {
			return limitErr("%s has %d points (max %d)", sh.Type, len(sh.Points)/2, l.MaxPoints)
		}
		for _, v := range sh.Points {
			if err := checkNum("point", v, l.MaxCoord); err != nil {
				return err
			}
		}
		np.MoveTo(sh.Points[0], sh.Points[1])
		for i := 2; i < len(sh.Points); i += 2 {
			np.LineTo(sh.Points[i], sh.Points[i+1])
		}
		if sh.Type == TypePolygon {
			np.Close()
		}
	case TypeLine:
		np.MoveTo(sh.X1, sh.Y1).LineTo(sh.X2, sh.Y2)
	case TypeRect:
		if sh.W < 0 || sh.H < 0 || sh.RX < 0 || sh.RY < 0 {
			return invalidErr("rect with a negative size")
		}
		rx, ry := sh.RX, sh.RY
		if rx == 0 {
			rx = ry
		}
		if ry == 0 {
			ry = rx
		}
		rectPath(sh.X, sh.Y, sh.W, sh.H, rx, ry, np)
	case TypeEllipse:
		if sh.RX < 0 || sh.RY < 0 {
			return invalidErr("ellipse with a negative radius")
		}
		ellipsePath(sh.CX, sh.CY, sh.RX, sh.RY, np)
	case TypeCircle:
		if sh.R < 0 {
			return invalidErr("circle with a negative radius")
		}
		ellipsePath(sh.CX, sh.CY, sh.R, sh.R, np)
	default:
		return invalidErr("unknown shape type %q", sh.Type)
	}
	if np.err != nil {
		return np.err
	}
	sh.path = np
	return nil
}

// Hash identifies the sprite's content (FNV-1a of its JSON form); the cell
// cache keys on it.
func (s *Sprite) Hash() uint64 {
	s.hOne.Do(func() {
		b, _ := json.Marshal(s)
		h := fnv.New64a()
		h.Write(b)
		s.hash = h.Sum64()
	})
	return s.hash
}

// ---- viewBox fitting ----

type fitSpec struct {
	none   bool
	ax, ay float64 // 0, .5, 1
	slice  bool
}

func parseFit(s string) (fitSpec, error) {
	f := fitSpec{ax: .5, ay: .5}
	s = strings.TrimSpace(s)
	if s == "" {
		return f, nil
	}
	if s == "none" {
		f.none = true
		return f, nil
	}
	parts := strings.Fields(s)
	if len(parts) < 1 || len(parts) > 2 {
		return f, invalidErr("bad preserveAspect %q", s)
	}
	al := map[string][2]float64{
		"xMinYMin": {0, 0}, "xMidYMin": {.5, 0}, "xMaxYMin": {1, 0},
		"xMinYMid": {0, .5}, "xMidYMid": {.5, .5}, "xMaxYMid": {1, .5},
		"xMinYMax": {0, 1}, "xMidYMax": {.5, 1}, "xMaxYMax": {1, 1},
	}
	a, ok := al[parts[0]]
	if !ok {
		return f, invalidErr("bad preserveAspect alignment %q", parts[0])
	}
	f.ax, f.ay = a[0], a[1]
	if len(parts) == 2 {
		switch parts[1] {
		case "meet":
		case "slice":
			f.slice = true
		default:
			return f, invalidErr("bad preserveAspect %q", parts[1])
		}
	}
	return f, nil
}

// rootTransform maps viewBox coordinates onto a w x h pixel target whose
// pixels have the given aspect (height/width).
func (s *Sprite) rootTransform(w, h int, aspect float64) Affine {
	vx, vy, vw, vh := s.ViewBox[0], s.ViewBox[1], s.ViewBox[2], s.ViewBox[3]
	tw, th := float64(w), float64(h)*aspect // target in square physical units
	sx, sy := tw/vw, th/vh
	switch {
	case s.fit.none:
	default:
		k := sx
		if (sy < sx) != s.fit.slice {
			k = sy
		}
		sx, sy = k, k
	}
	ox := mul(tw-mul(vw, sx), s.fit.ax)
	oy := mul(th-mul(vh, sy), s.fit.ay)
	if s.fit.none {
		ox, oy = 0, 0
	}
	// physical -> device: y / aspect
	m := Affine{sx, 0, 0, sy / aspect, ox - mul(vx, sx), (oy - mul(vy, sy)) / aspect}
	return m
}
