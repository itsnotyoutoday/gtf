package layout

import (
	"strings"
	"testing"
)

func rgb(r, g, b uint8) Color { return Color{Kind: RGB, R: r, G: g, B: b} }

// canvasText renders {canvas c} through a template and returns the cells.
type canvasRes struct{ c *Canvas }

func (r canvasRes) Resolve(ref string) (any, bool) {
	if ref == "canvas:c" {
		return r.c, true
	}
	return nil, false
}

func evalTpl(t *testing.T, tpl string, r Resolver) []Cell {
	t.Helper()
	p, err := ParseTemplate(tpl)
	if err != nil {
		t.Fatal(err)
	}
	return p.Eval(&Ctx{R: r, Width: 20})
}

func TestCanvasHalfBlocks(t *testing.T) {
	c := NewCanvas(3, 1)             // 3x2 pixels
	c.Blend(0, 0, rgb(255, 0, 0), 1) // top only
	c.Blend(1, 1, rgb(0, 255, 0), 1) // bottom only
	c.Blend(2, 0, rgb(0, 0, 255), 1) // both, different
	c.Blend(2, 1, rgb(9, 9, 9), 1)
	cells := evalTpl(t, "{canvas c}", canvasRes{c})
	if len(cells) != 3 {
		t.Fatalf("want 3 cells, got %d", len(cells))
	}
	if cells[0].R != '▀' || cells[0].A.FG != rgb(255, 0, 0) {
		t.Errorf("top-only cell: %+v", cells[0])
	}
	if cells[1].R != '▄' || cells[1].A.FG != rgb(0, 255, 0) {
		t.Errorf("bottom-only cell: %+v", cells[1])
	}
	if cells[2].R != '▀' || cells[2].A.FG != rgb(0, 0, 255) || cells[2].A.BG != rgb(9, 9, 9) {
		t.Errorf("two-colour cell: %+v", cells[2])
	}
}

func TestCanvasPrimitives(t *testing.T) {
	c := NewCanvas(20, 10) // 20x20 px
	c.Rect(1, 1, 4, 4, rgb(1, 2, 3), 1, true)
	if p := c.pix[2*c.W+2]; !p.Set || p.R != 1 {
		t.Error("fill rect did not paint")
	}
	c.Disc(10, 10, 4, rgb(9, 9, 9), 1)
	if p := c.pix[10*c.W+10]; !p.Set {
		t.Error("disc centre not painted")
	}
	if p := c.pix[10*c.W+17]; p.Set {
		t.Error("disc painted outside its radius")
	}
	c.Poly([]float64{12, 2, 18, 2, 15, 8}, rgb(5, 5, 5), 1)
	if p := c.pix[4*c.W+15]; !p.Set {
		t.Error("polygon interior not painted")
	}
	c.Ellipse(5, 15, 4, 2, rgb(7, 7, 7), 1)
	if p := c.pix[15*c.W+5]; !p.Set {
		t.Error("ellipse centre not painted")
	}
	if p := c.pix[15*c.W+12]; p.Set {
		t.Error("ellipse painted outside")
	}
	// an arc from 0 to 90 degrees (up to right) leaves the lower-left empty
	d := NewCanvas(20, 10)
	d.Arc(10, 10, 3, 8, 0, 90, rgb(1, 1, 1), 1)
	if !d.pix[5*d.W+13].Set {
		t.Error("arc missing in its quadrant")
	}
	if d.pix[14*d.W+6].Set {
		t.Error("arc painted outside its quadrant")
	}
}

func TestCanvasAlphaAndAdd(t *testing.T) {
	c := NewCanvas(2, 1)
	c.Blend(0, 0, rgb(200, 200, 200), 1)
	c.Blend(0, 0, rgb(0, 0, 0), 0.5)
	if p := c.pix[0]; p.R != 100 {
		t.Errorf("blend: got %d, want 100", p.R)
	}
	c.Add(1, 0, rgb(100, 100, 100), 1)
	c.Add(1, 0, rgb(200, 200, 200), 1)
	if p := c.pix[1]; p.R != 255 {
		t.Errorf("add should saturate at 255, got %d", p.R)
	}
}

func TestCanvasStarsDeterministic(t *testing.T) {
	a, b := NewCanvas(30, 10), NewCanvas(30, 10)
	a.Stars(0, 0, 30, 20, 40, 7, 0)
	b.Stars(0, 0, 30, 20, 40, 7, 0)
	for i := range a.pix {
		if a.pix[i] != b.pix[i] {
			t.Fatal("same seed gave a different sky")
		}
	}
	n := 0
	for _, p := range a.pix {
		if p.Set {
			n++
		}
	}
	if n < 20 {
		t.Errorf("only %d stars", n)
	}
}

func TestAnsiWidget(t *testing.T) {
	r := mapRes{"var:s": "\x1b[38;2;255;0;0mR\x1b[0mx\\e[48;5;21;1mB"}
	cells := evalTpl(t, "{ansi s}", r)
	if len(cells) != 3 {
		t.Fatalf("got %d cells", len(cells))
	}
	if cells[0].A.FG != rgb(255, 0, 0) {
		t.Errorf("truecolor fg: %+v", cells[0].A)
	}
	if cells[1].A.FG.Kind != Default {
		t.Errorf("reset: %+v", cells[1].A)
	}
	if cells[2].A.BG.Kind != Indexed || cells[2].A.BG.N != 21 || !cells[2].A.Bold {
		t.Errorf("256 colour bg and bold via the \\e form: %+v", cells[2].A)
	}
}

func TestRadarWidget(t *testing.T) {
	r := mapRes{"var:ct": "1:0:50:H;2:90:50:F", "var:sw": 10.0}
	cells := evalTpl(t, "{radar contacts=ct sweep=sw width=21 height=11}", r)
	text := ""
	for _, c := range cells {
		text += string(c.R)
	}
	if strings.Count(text, "\n") != 10 {
		t.Errorf("want 11 rows, got %d newlines", strings.Count(text, "\n"))
	}
	if !strings.Contains(text, "●") {
		t.Error("contacts not drawn")
	}
	braille := false
	for _, c := range cells {
		if c.R >= 0x2800 && c.R <= 0x28ff {
			braille = true
		}
	}
	if !braille {
		t.Error("no braille dots drawn")
	}
}

func TestScopeAndGradientBar(t *testing.T) {
	l := New("t")
	_ = l
	hist := []float64{1, 5, 2, 9, 4, 8}
	p, _ := ParseTemplate("{scope v width=6 height=2 color=#00ff00}")
	cells := p.Eval(&Ctx{R: mapRes{"var:v": 8.0}, Hist: func(string) []float64 { return hist }})
	if strings.Count(cellsText(cells), "\n") != 1 {
		t.Error("scope should be two rows")
	}
	bar := evalTpl(t, `{bar "var:v" 10 width=4 gradient=#000000,#ff0000}`, mapRes{"var:v": 10.0})
	if len(bar) != 4 || bar[0].A.FG.R >= bar[3].A.FG.R {
		t.Errorf("gradient should ramp up: %+v", bar)
	}
}

func cellsText(c []Cell) string {
	var b strings.Builder
	for _, x := range c {
		b.WriteRune(x.R)
	}
	return b.String()
}
