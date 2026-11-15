// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package raster

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func wantLimit(t *testing.T, name string, err error) {
	t.Helper()
	if !errors.Is(err, ErrLimit) {
		t.Errorf("%s: want ErrLimit, got %v", name, err)
	}
}

func wantInvalid(t *testing.T, name string, err error) {
	t.Helper()
	if !errors.Is(err, ErrInvalid) {
		t.Errorf("%s: want ErrInvalid, got %v", name, err)
	}
}

// timed fails the test if f takes too long: hostile input must never hang.
func timed(t *testing.T, name string, max time.Duration, f func()) {
	t.Helper()
	if raceDetector {
		max *= 15 // the race detector slows everything down by an order of magnitude
	}
	start := time.Now()
	f()
	if d := time.Since(start); d > max {
		t.Errorf("%s took %v (max %v)", name, d, max)
	}
}

func TestHostileSizes(t *testing.T) {
	s := loadSprite(t, "planet")
	_, err := s.Render(1<<30, 1<<30, nil, nil)
	wantLimit(t, "huge render", err)
	_, err = s.Render(4096, 4096, nil, nil)
	wantLimit(t, "16M pixels", err)
	_, err = s.Render(0, 5, nil, nil)
	wantInvalid(t, "zero width", err)
	_, err = s.Render(-3, 5, nil, nil)
	wantInvalid(t, "negative width", err)
	_, err = RenderCells(s, 1<<20, 1<<20, nil, Options{}, nil)
	wantLimit(t, "huge cells", err)
	_, err = RenderCells(s, 300, 300, nil, Options{}, nil)
	wantLimit(t, "90k cells over the cell limit", err)
	_, err = RenderCells(s, 0, 4, nil, Options{}, nil)
	wantInvalid(t, "zero cells", err)
	_, err = s.Render(10, 10, &Params{PixelAspect: math.NaN()}, nil)
	wantInvalid(t, "NaN aspect", err)
	_, err = s.Render(10, 10, &Params{PixelAspect: 1e9}, nil)
	wantInvalid(t, "huge aspect", err)
	_, err = s.Render(10, 10, &Params{Zones: map[string]float64{"x": math.NaN()}}, nil)
	wantInvalid(t, "NaN zone", err)
	_, err = s.Render(10, 10, &Params{Glows: map[string]float64{"x": math.Inf(1)}}, nil)
	wantInvalid(t, "Inf glow", err)
	// the largest render the defaults allow still finishes quickly
	timed(t, "1M pixel render", 3*time.Second, func() {
		if _, err := s.Render(1024, 1024, nil, nil); err != nil {
			t.Error(err)
		}
	})
}

func TestHostileJSON(t *testing.T) {
	deepArr := strings.Repeat("[", 200000) + strings.Repeat("]", 200000)
	deepGroups := func(n int) string {
		return `{"layers":[{"shapes":[` + strings.Repeat(`{"type":"group","children":[`, n) + `{"type":"circle","r":1}` + strings.Repeat(`]}`, n) + `]}]}`
	}
	bad := map[string]string{
		"empty":           ``,
		"not json":        `hello`,
		"truncated":       `{"layers":[{"shapes":[`,
		"trailing":        `{"layers":[]} {}`,
		"wrong type":      `{"layers":"x"}`,
		"viewBox":         `{"viewBox":[0,0,0,10],"layers":[]}`,
		"viewBox type":    `{"viewBox":"a","layers":[]}`,
		"huge number":     `{"viewBox":[0,0,1e999,10],"layers":[]}`,
		"big coord":       `{"layers":[{"shapes":[{"type":"circle","r":1e300}]}]}`,
		"unknown type":    `{"layers":[{"shapes":[{"type":"banana"}]}]}`,
		"bad colour":      `{"layers":[{"shapes":[{"type":"circle","r":1,"fill":"#12"}]}]}`,
		"bad colour 2":    `{"layers":[{"shapes":[{"type":"circle","r":1,"fill":"#gggggg"}]}]}`,
		"bad paint":       `{"layers":[{"shapes":[{"type":"circle","r":1,"fill":{"type":"conic"}}]}]}`,
		"no stops":        `{"layers":[{"shapes":[{"type":"circle","r":1,"fill":{"type":"linear","stops":[]}}]}]}`,
		"bad stop":        `{"layers":[{"shapes":[{"type":"circle","r":1,"fill":{"type":"linear","stops":[[1]]}}]}]}`,
		"bad transform":   `{"layers":[{"transform":"rotate(","shapes":[]}]}`,
		"bad path":        `{"layers":[{"shapes":[{"type":"path","d":"M0 0 L"}]}]}`,
		"odd points":      `{"layers":[{"shapes":[{"type":"polygon","points":[1,2,3]}]}]}`,
		"neg radius":      `{"layers":[{"shapes":[{"type":"circle","r":-1}]}]}`,
		"neg width":       `{"layers":[{"shapes":[{"type":"line","stroke":{"width":-2}}]}]}`,
		"bad cap":         `{"layers":[{"shapes":[{"type":"line","stroke":{"cap":"x"}}]}]}`,
		"bad opacity":     `{"layers":[{"opacity":2,"shapes":[]}]}`,
		"bad fillRule":    `{"layers":[{"shapes":[{"type":"circle","r":1,"fillRule":"x"}]}]}`,
		"bad aspect":      `{"preserveAspect":"banana","layers":[]}`,
		"zone no id":      `{"layers":[],"zones":[{}]}`,
		"glow neg radius": `{"layers":[],"glows":[{"id":"a","r":-1}]}`,
	}
	for name, js := range bad {
		_, err := ParseJSON([]byte(js), nil)
		if !errors.Is(err, ErrInvalid) && !errors.Is(err, ErrLimit) {
			t.Errorf("%s: want a typed error, got %v", name, err)
		}
	}
	timed(t, "deep brackets", time.Second, func() {
		_, err := ParseJSON([]byte(deepArr), nil)
		wantLimit(t, "200k brackets", err)
	})
	if _, err := ParseJSON([]byte(deepGroups(10)), nil); err != nil {
		t.Errorf("10 nested groups should be fine: %v", err)
	}
	timed(t, "deep groups", time.Second, func() {
		_, err := ParseJSON([]byte(deepGroups(40)), nil)
		wantLimit(t, "40 nested groups", err)
		_, err = ParseJSON([]byte(deepGroups(5000)), nil)
		wantLimit(t, "5000 nested groups", err)
	})
	_, err := ParseJSON(make([]byte, 2<<20), nil)
	wantLimit(t, "2MB document", err)
	// too many shapes
	many := `{"layers":[{"shapes":[` + strings.Repeat(`{"type":"circle","r":1},`, 5000) + `{"type":"circle","r":1}]}]}`
	_, err = ParseJSON([]byte(many), nil)
	wantLimit(t, "5001 shapes", err)
	_, err = ParseJSON([]byte(`{"layers":[`+strings.Repeat(`{"shapes":[]},`, 70)+`{"shapes":[]}]}`), nil)
	wantLimit(t, "71 layers", err)
	stops := strings.Repeat(`[0,"#fff"],`, 40) + `[1,"#000"]`
	_, err = ParseJSON([]byte(`{"layers":[{"shapes":[{"type":"circle","r":1,"fill":{"type":"linear","stops":[`+stops+`]}}]}]}`), nil)
	wantLimit(t, "41 stops", err)
}

func TestHostilePaths(t *testing.T) {
	_, err := ParsePath("M0 0"+strings.Repeat(" L1 1", 80000), nil)
	wantLimit(t, "400KB of path", err)
	_, err = ParsePath("M0 0"+strings.Repeat("L1 1", 50001), nil)
	wantLimit(t, "50001 segments", err)
	if p, err := ParsePath("M0 0"+strings.Repeat("L1 1", 49990), nil); err != nil || p.Len() != 49991 {
		t.Errorf("just under the limit: %v", err)
	}
	for _, d := range []string{"M1e400 0", "M0 0 L1e7 0", "M0 0 A1e9 1 0 0 0 1 1", "M NaN 0", "M0 0 L Infinity 0", "M-1e-999 0 L5 5"} {
		_, err := ParsePath(d, nil)
		if d == "M-1e-999 0 L5 5" {
			if err != nil {
				t.Errorf("%q: an underflowing number is just zero: %v", d, err)
			}
			continue
		}
		if err == nil {
			t.Errorf("%q should fail", d)
		}
	}
	// garbage bytes never panic
	for i := 0; i < 2000; i++ {
		b := make([]byte, 1+i%40)
		x := uint32(i)*2654435761 + 1
		for j := range b {
			x ^= x << 13
			x ^= x >> 17
			x ^= x << 5
			const alpha = "MmLlHhVvCcSsQqTtAaZz0123456789.-+eE ,\n#x"
			b[j] = alpha[int(x)%len(alpha)]
		}
		ParsePath(string(b), nil)
	}
	// the numeric API refuses non-finite input too
	p := NewPath().MoveTo(0, 0).LineTo(math.NaN(), 1)
	if p.Err() == nil {
		t.Error("NaN LineTo accepted")
	}
	p = NewPath().MoveTo(0, 0).ArcTo(math.Inf(1), 1, 0, false, false, 1, 1)
	if p.Err() == nil {
		t.Error("Inf arc accepted")
	}
}

// Go-built sprites can carry values JSON cannot (NaN, Inf).
func TestNonFiniteGoSprites(t *testing.T) {
	mk := func(sh Shape) *Sprite { return &Sprite{Layers: []Layer{{Shapes: []Shape{sh}}}} }
	for name, sh := range map[string]Shape{
		"nan radius":   {Type: TypeCircle, R: math.NaN()},
		"inf rect":     {Type: TypeRect, W: math.Inf(1), H: 1},
		"nan point":    {Type: TypePolygon, Points: []float64{0, 0, 1, math.NaN(), 2, 2}},
		"nan width":    {Type: TypeLine, X2: 1, Stroke: &Stroke{Width: math.NaN()}},
		"nan opacity":  {Type: TypeCircle, R: 1, Opacity: math.NaN()},
		"nan gradient": {Type: TypeCircle, R: 1, Fill: &Paint{Kind: PaintLinear, X2: math.NaN(), Stops: []Stop{{0, Color{}}}}},
		"nan stop":     {Type: TypeCircle, R: 1, Fill: &Paint{Kind: PaintRadial, R: 1, Stops: []Stop{{math.NaN(), Color{}}}}},
	} {
		_, err := mk(sh).Render(8, 8, nil, nil)
		if !errors.Is(err, ErrInvalid) && !errors.Is(err, ErrLimit) {
			t.Errorf("%s: want a typed error, got %v", name, err)
		}
	}
	s := &Sprite{ViewBox: [4]float64{0, 0, math.NaN(), 1}}
	if _, err := s.Render(8, 8, nil, nil); err == nil {
		t.Error("NaN viewBox accepted")
	}
}

// Documents inside the syntax limits that are still nasty to rasterise must
// stop at the work budget (or just work), quickly.
func TestHostileWork(t *testing.T) {
	cases := map[string]string{}
	// 4000 full-canvas shapes with gradient and stroke at the biggest size
	var b strings.Builder
	b.WriteString(`{"viewBox":[0,0,100,100],"layers":[{"shapes":[`)
	for i := 0; i < 4000; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(`{"type":"rect","x":0,"y":0,"w":100,"h":100,"fill":{"type":"radial","cx":50,"cy":50,"r":70,"stops":[[0,"#fff"],[1,"#000"]]},"stroke":{"width":3,"join":"round"}}`)
	}
	b.WriteString(`]}]}`)
	cases["4000 full-screen gradient shapes"] = b.String()
	// a polygon with 50k points zig-zagging across the canvas
	var pts []string
	for i := 0; i < 49000; i++ {
		x := 0
		if i%2 == 1 {
			x = 100
		}
		pts = append(pts, fmt.Sprintf("%d,%d", x, i%100))
	}
	cases["zigzag polygon"] = `{"layers":[{"shapes":[{"type":"polygon","points":[` + strings.Join(pts, ",") + `],"stroke":{"width":2,"join":"round","cap":"round"}}]}]}`
	cases["huge coordinates"] = `{"viewBox":[0,0,1,1],"layers":[{"shapes":[{"type":"path","d":"M-1000000 -1000000 L1000000 1000000 L-1000000 1000000 Z","stroke":{"width":999999}}]}]}`
	cases["giant stroke width"] = `{"layers":[{"shapes":[{"type":"line","x1":0,"y1":0,"x2":100,"y2":100,"stroke":{"width":1000000,"cap":"round"}}]}]}`
	cases["giant scale"] = `{"layers":[{"transform":"scale(1000000)","shapes":[{"type":"circle","cx":1,"cy":1,"r":1000000,"stroke":{"width":1000}}]}]}`
	cases["tiny scale"] = `{"layers":[{"transform":"scale(0.0000001)","shapes":[{"type":"circle","cx":50,"cy":50,"r":40}]}]}`
	cases["singular transform"] = `{"layers":[{"transform":"matrix(0 0 0 0 5 5)","shapes":[{"type":"circle","r":40,"cx":50,"cy":50,"stroke":{"width":5},"fill":{"type":"linear","x2":1,"stops":[[0,"#fff"]]}}]}]}`
	cases["zero length gradient"] = `{"layers":[{"shapes":[{"type":"rect","w":100,"h":100,"fill":{"type":"linear","stops":[[0,"#fff"],[1,"#000"]]}},{"type":"rect","w":100,"h":100,"fill":{"type":"radial","stops":[[0,"#fff"]]}}]}]}`
	cases["reflect overflow"] = `{"layers":[{"shapes":[{"type":"rect","w":100,"h":100,"fill":{"type":"linear","x2":1e-300,"spread":"reflect","stops":[[0,"#fff"],[1,"#000"]]}}]}]}`
	cases["many curves"] = `{"layers":[{"shapes":[{"type":"path","d":"M0 0` + strings.Repeat("C100000 0 0 100000 100 100", 3000) + `","stroke":{"width":2}}]}]}`
	cases["many nested clips"] = `{"layers":[{"shapes":[{"type":"rect","w":100,"h":100,"clip":{"type":"group","children":[` + strings.Repeat(`{"type":"circle","cx":50,"cy":50,"r":40,"clip":{"type":"circle","cx":50,"cy":50,"r":45}},`, 300) + `{"type":"circle","cx":50,"cy":50,"r":40}]}}]}]}`
	cases["opacity groups"] = `{"layers":[{"shapes":[` + strings.Repeat(`{"type":"group","opacity":0.5,"children":[{"type":"circle","cx":50,"cy":50,"r":50}]},`, 1500) + `{"type":"circle","cx":50,"cy":50,"r":50}]}]}`
	for name, js := range cases {
		s, err := ParseJSON([]byte(js), nil)
		if err != nil {
			continue // rejected up front is fine
		}
		timed(t, name, 3*time.Second, func() {
			for _, size := range [][2]int{{64, 40}, {1000, 1000}} {
				_, err := s.Render(size[0], size[1], nil, nil)
				if err != nil && !errors.Is(err, ErrLimit) && !errors.Is(err, ErrInvalid) {
					t.Errorf("%s: untyped error %v", name, err)
				}
			}
		})
	}
	// and the budget itself trips on a document that is syntactically fine
	s, err := ParseJSON([]byte(cases["4000 full-screen gradient shapes"]), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Render(1000, 1000, nil, nil)
	wantLimit(t, "budget", err)
	// a tighter budget stops sooner
	small := Limits{MaxOps: 1000}
	_, err = loadSprite(t, "saucer").Render(200, 140, nil, &small)
	wantLimit(t, "tiny budget", err)
}

func TestNoPanicOnRandomMutations(t *testing.T) {
	base, err := os.ReadFile(filepath.Join("testdata", "saucer.json"))
	if err != nil {
		t.Fatal(err)
	}
	x := uint32(12345)
	rnd := func(n int) int {
		x ^= x << 13
		x ^= x >> 17
		x ^= x << 5
		return int(x % uint32(n))
	}
	for i := 0; i < 400; i++ {
		b := append([]byte(nil), base...)
		for k := 0; k < 1+rnd(6); k++ {
			switch rnd(3) {
			case 0:
				b[rnd(len(b))] = byte(rnd(256))
			case 1:
				at := rnd(len(b))
				b = append(b[:at], b[min(len(b), at+1+rnd(20)):]...)
			case 2:
				at := rnd(len(b))
				b = append(b[:at], append([]byte(fmt.Sprint(rnd(1000000)-500000)), b[at:]...)...)
			}
		}
		s, err := ParseJSON(b, nil)
		if err != nil {
			continue
		}
		timed(t, "mutated sprite", 2*time.Second, func() {
			if _, err := RenderCells(s, 20, 10, nil, Options{Mode: Sextant}, nil); err != nil && !errors.Is(err, ErrLimit) && !errors.Is(err, ErrInvalid) {
				t.Errorf("untyped error: %v", err)
			}
		})
	}
}

func FuzzParseAndRender(f *testing.F) {
	for _, n := range []string{"saucer", "warbird", "planet"} {
		b, _ := os.ReadFile(filepath.Join("testdata", n+".json"))
		f.Add(b)
	}
	f.Add([]byte(`{"layers":[{"shapes":[{"type":"path","d":"M0 0 A5 5 0 1 1 10 10 Q1 2 3 4 T5 6 Z"}]}]}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		s, err := ParseJSON(data, nil)
		if err != nil {
			return
		}
		lim := DefaultLimits()
		lim.MaxOps = 2 << 20 // keep each fuzz execution short
		c, err := RenderCells(s, 16, 8, &Params{Zones: map[string]float64{"hull": 0.5}}, Options{Mode: Quadrant}, &lim)
		if err == nil && (c.W != 16 || c.H != 8) {
			t.Fatalf("bad grid %dx%d", c.W, c.H)
		}
	})
}

func FuzzParsePath(f *testing.F) {
	for _, s := range []string{"M0 0 L10 10 Z", "M10-5.5.5 20", "M0 0 a5 5 0 1010 0", "M0 0 C1 1 2 2 3 3 S4 4 5 5", ""} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, d string) {
		p, err := ParsePath(d, nil)
		if err != nil {
			return
		}
		var bud budget
		bud.left = 1 << 24
		flatten(p, 0.05, &bud)
	})
}
