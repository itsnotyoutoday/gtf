package layout

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Dock is the side of the main region a panel attaches to.
type Dock int

const (
	DockLeft Dock = iota
	DockRight
	DockTop
	DockBottom
)

func (d Dock) String() string { return [...]string{"left", "right", "top", "bottom"}[d] }

// ParseDock parses left|right|top|bottom.
func ParseDock(s string) (Dock, error) {
	switch strings.ToLower(s) {
	case "left", "l":
		return DockLeft, nil
	case "right", "r":
		return DockRight, nil
	case "top", "t":
		return DockTop, nil
	case "bottom", "b":
		return DockBottom, nil
	}
	return 0, fmt.Errorf("bad dock %q (left, right, top, bottom)", s)
}

// DimKind says how a Dim is resolved.
type DimKind int

const (
	Auto    DimKind = iota // fit the content
	Fixed                  // N cells
	Percent                // N percent of the terminal
	Flex                   // N/4 of the terminal (N defaults to 1)
)

// Dim is a size constraint.
type Dim struct {
	Kind DimKind
	N    int
}

// ParseDim parses "", "auto", "24", "30%", "*", "2*".
func ParseDim(s string) (Dim, error) {
	s = strings.TrimSpace(s)
	switch {
	case s == "" || s == "auto":
		return Dim{}, nil
	case strings.HasSuffix(s, "%"):
		n, err := strconv.Atoi(s[:len(s)-1])
		if err != nil || n < 0 || n > 100 {
			return Dim{}, fmt.Errorf("bad size %q", s)
		}
		return Dim{Percent, n}, nil
	case strings.HasSuffix(s, "*"):
		n := 1
		if len(s) > 1 {
			var err error
			if n, err = strconv.Atoi(s[:len(s)-1]); err != nil {
				return Dim{}, fmt.Errorf("bad size %q", s)
			}
		}
		return Dim{Flex, n}, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return Dim{}, fmt.Errorf("bad size %q", s)
	}
	return Dim{Fixed, n}, nil
}

func (d Dim) String() string {
	switch d.Kind {
	case Fixed:
		return strconv.Itoa(d.N)
	case Percent:
		return strconv.Itoa(d.N) + "%"
	case Flex:
		if d.N == 1 {
			return "*"
		}
		return strconv.Itoa(d.N) + "*"
	}
	return "auto"
}

// Border styles.
var borders = map[string][6]rune{
	"single":  {'┌', '─', '┐', '│', '└', '┘'},
	"rounded": {'╭', '─', '╮', '│', '╰', '╯'},
	"double":  {'╔', '═', '╗', '║', '╚', '╝'},
	"ascii":   {'+', '-', '+', '|', '+', '+'},
}

// BorderStyles lists the valid border names.
func BorderStyles() []string { return []string{"single", "rounded", "double", "ascii"} }

// Panel is a docked rectangle showing a template.
type Panel struct {
	Name     string
	Dock     Dock
	W, H     Dim
	Title    string
	Border   string // "" = none, else a key of borders
	Body     string
	Hidden   bool
	MinWidth int  // hide when the terminal is narrower than this
	Wrap     bool // wrap long lines instead of clipping
	Fg, Bg   string
	Order    int

	tpl   *Template
	deps  []string
	dirty bool
	grid  *Grid
	gw    int
	gh    int
	lines int // content line count at last evaluation
	cols  int // widest content line at last evaluation
}

// Rect is a screen rectangle (0-based).
type Rect struct{ X, Y, W, H int }

// Placed is a panel with its computed rectangle.
type Placed struct {
	P *Panel
	Rect
}

// Frame is the result of laying out a terminal.
type Frame struct {
	W, H   int
	Main   Rect
	Panels []Placed
}

// Layout is a named set of panels.
type Layout struct {
	Name    string
	MinMain int // minimum width of the main region (default 30)
	panels  []*Panel
	hist    map[string][]float64
	histMax int
	seq     int
}

// New makes an empty layout.
func New(name string) *Layout {
	return &Layout{Name: name, MinMain: 30, hist: map[string][]float64{}, histMax: 120}
}

// Panels returns the panels in declaration order.
func (l *Layout) Panels() []*Panel { return append([]*Panel(nil), l.panels...) }

// Get returns a panel by name.
func (l *Layout) Get(name string) *Panel {
	for _, p := range l.panels {
		if p.Name == name {
			return p
		}
	}
	return nil
}

// Set adds or replaces a panel (keeping its position when replacing).
func (l *Layout) Set(p *Panel) error {
	if p.Name == "" {
		return fmt.Errorf("panel needs a name")
	}
	if p.Border != "" {
		if _, ok := borders[p.Border]; !ok {
			return fmt.Errorf("bad border %q (%s)", p.Border, strings.Join(BorderStyles(), ", "))
		}
	}
	t, err := ParseTemplate(p.Body)
	if err != nil {
		return err
	}
	p.tpl, p.dirty, p.grid = t, true, nil
	for i, q := range l.panels {
		if q.Name == p.Name {
			l.panels[i] = p
			return nil
		}
	}
	l.seq++
	p.Order = l.seq
	l.panels = append(l.panels, p)
	return nil
}

// Remove deletes a panel; false if absent.
func (l *Layout) Remove(name string) bool {
	for i, q := range l.panels {
		if q.Name == name {
			l.panels = append(l.panels[:i], l.panels[i+1:]...)
			return true
		}
	}
	return false
}

// Touch marks a panel for redraw (after changing its fields).
func (l *Layout) Touch(name string) {
	if p := l.Get(name); p != nil {
		p.dirty, p.grid = true, nil
	}
}

// Deps lists every binding the panels read at their last render.
func (l *Layout) Deps() []string {
	seen := map[string]bool{}
	for _, p := range l.panels {
		for _, d := range p.deps {
			seen[d] = true
		}
	}
	out := make([]string, 0, len(seen))
	for d := range seen {
		out = append(out, d)
	}
	sort.Strings(out)
	return out
}

func related(dep, key string) bool {
	return dep == key || strings.HasPrefix(dep, key+".") || strings.HasPrefix(key, dep+".")
}

// Changed records that binding key changed: the panels that depend on it
// become dirty and their names are returned.
func (l *Layout) Changed(key string) []string {
	var out []string
	for _, p := range l.panels {
		hit := p.grid == nil
		for _, d := range p.deps {
			if related(d, key) {
				hit = true
				break
			}
		}
		if hit {
			p.dirty = true
			out = append(out, p.Name)
		}
	}
	return out
}

// Observe appends a numeric sample to a binding's history (for sparklines).
func (l *Layout) Observe(key string, v float64) {
	h := append(l.hist[key], v)
	if len(h) > l.histMax {
		h = h[len(h)-l.histMax:]
	}
	l.hist[key] = h
}

// Dirty reports whether the panel needs rendering.
func (l *Layout) Dirty(name string) bool {
	p := l.Get(name)
	return p != nil && (p.dirty || p.grid == nil)
}

func (l *Layout) border(p *Panel) int {
	if p.Border != "" {
		return 1
	}
	return 0
}

// measure evaluates the body to learn its natural size.
func (l *Layout) measure(p *Panel, r Resolver, width int) {
	cells := l.eval(p, r, width)
	lines := splitLines(cells)
	p.lines, p.cols = len(lines), 0
	for _, ln := range lines {
		if len(ln) > p.cols {
			p.cols = len(ln)
		}
	}
}

func (l *Layout) eval(p *Panel, r Resolver, width int) []Cell {
	c := &Ctx{R: r, Width: width, Hist: func(k string) []float64 { return l.hist[k] }}
	cells := p.tpl.Eval(c)
	p.deps = c.Deps()
	return cells
}

func splitLines(cells []Cell) [][]Cell {
	var out [][]Cell
	var cur []Cell
	for _, c := range cells {
		if c.R == '\n' {
			out = append(out, cur)
			cur = nil
			continue
		}
		cur = append(cur, c)
	}
	return append(out, cur)
}

func (d Dim) resolve(total int) int {
	switch d.Kind {
	case Fixed:
		return d.N
	case Percent:
		return total * d.N / 100
	case Flex:
		return total * d.N / 4
	}
	return -1
}

// Compute lays the visible panels out around the main region of a w x h
// terminal. Panels are placed in declaration order, outermost first. A panel
// is skipped (hidden) when the terminal is narrower than its MinWidth or when
// it would leave the main region narrower than MinMain (or shorter than 3).
func (l *Layout) Compute(w, h int, r Resolver) Frame {
	f := Frame{W: w, H: h, Main: Rect{0, 0, w, h}}
	rem := f.Main
	for _, p := range l.panels {
		if p.Hidden || w < p.MinWidth {
			continue
		}
		bd := l.border(p)
		switch p.Dock {
		case DockLeft, DockRight:
			pw := p.W.resolve(w)
			if pw < 0 {
				l.measure(p, r, rem.W)
				pw = p.cols + 2*bd
				if pw > w*40/100 {
					pw = w * 40 / 100
				}
			}
			if pw < 2*bd+1 || rem.W-pw < l.MinMain {
				continue
			}
			x := rem.X
			if p.Dock == DockRight {
				x = rem.X + rem.W - pw
			} else {
				rem.X += pw
			}
			rem.W -= pw
			f.Panels = append(f.Panels, Placed{p, Rect{x, rem.Y, pw, rem.H}})
		default:
			ph := p.H.resolve(h)
			if ph < 0 {
				l.measure(p, r, rem.W-2*bd)
				ph = p.lines + 2*bd
				if ph > h/3 {
					ph = h / 3
				}
			}
			if ph < 2*bd+1 || rem.H-ph < 3 {
				continue
			}
			y := rem.Y
			if p.Dock == DockBottom {
				y = rem.Y + rem.H - ph
			} else {
				rem.Y += ph
			}
			rem.H -= ph
			f.Panels = append(f.Panels, Placed{p, Rect{rem.X, y, rem.W, ph}})
		}
	}
	// side panels span only the rows left after top/bottom ones placed
	// later; fix up vertical extents so they all share the final main band
	// only when placed before top/bottom (outermost first keeps full height).
	f.Main = rem
	return f
}

// Render draws p into a grid of the given size (cached until it turns dirty
// or the size changes).
func (l *Layout) Render(p *Panel, w, h int, r Resolver) *Grid {
	if p.grid != nil && !p.dirty && p.gw == w && p.gh == h {
		return p.grid
	}
	bd := l.border(p)
	iw, ih := w-2*bd, h-2*bd
	g := NewGrid(w, h)
	var base Attr
	base.FG, _ = ParseColor(p.Fg)
	base.BG, _ = ParseColor(p.Bg)
	if base.BG.Kind != Default || base.FG.Kind != Default {
		for i := range g.Cells {
			g.Cells[i].A = base
		}
	}
	if iw > 0 && ih > 0 {
		cells := l.eval(p, r, iw)
		lines := splitLines(cells)
		if p.Wrap {
			lines = wrapLines(lines, iw)
		}
		p.lines = len(lines)
		for y := 0; y < ih && y < len(lines); y++ {
			for x := 0; x < iw && x < len(lines[y]); x++ {
				c := lines[y][x]
				if c.A.BG.Kind == Default {
					c.A.BG = base.BG
				}
				if c.A.FG.Kind == Default {
					c.A.FG = base.FG
				}
				g.Set(x+bd, y+bd, c)
			}
		}
	}
	if bd > 0 {
		drawBorder(g, borders[p.Border], p.Title, base)
	}
	p.grid, p.gw, p.gh, p.dirty = g, w, h, false
	return g
}

func wrapLines(lines [][]Cell, w int) [][]Cell {
	var out [][]Cell
	for _, ln := range lines {
		if len(ln) <= w {
			out = append(out, ln)
			continue
		}
		for len(ln) > w {
			cut := w
			for i := w; i > w/2; i-- {
				if ln[i-1].R == ' ' {
					cut = i
					break
				}
			}
			out = append(out, ln[:cut])
			ln = ln[cut:]
		}
		out = append(out, ln)
	}
	return out
}

func drawBorder(g *Grid, b [6]rune, title string, a Attr) {
	w, h := g.W, g.H
	if w < 2 || h < 2 {
		return
	}
	put := func(x, y int, r rune) { g.Set(x, y, Cell{R: r, A: a}) }
	put(0, 0, b[0])
	put(w-1, 0, b[2])
	put(0, h-1, b[4])
	put(w-1, h-1, b[5])
	for x := 1; x < w-1; x++ {
		put(x, 0, b[1])
		put(x, h-1, b[1])
	}
	for y := 1; y < h-1; y++ {
		put(0, y, b[3])
		put(w-1, y, b[3])
	}
	if title != "" && w > 6 {
		t := " " + title + " "
		if utf8.RuneCountInString(t) > w-4 {
			t = string([]rune(t)[:w-4])
		}
		ta := a
		ta.Bold = true
		for i, r := range []rune(t) {
			g.Set(2+i, 0, Cell{R: r, A: ta})
		}
	}
}
