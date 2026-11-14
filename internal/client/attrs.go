package client

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/itsnotyoutoday/gtf/internal/layout"
	"github.com/itsnotyoutoday/gtf/internal/ui"
	"github.com/itsnotyoutoday/gtfparser"
)

// tfAttrs is a parsed tf attribute list ("BuCred,Cbgyellow").
type tfAttrs struct {
	set       layout.Attr
	fgSet     bool
	bgSet     bool
	bold, ul  bool
	rev, dim  bool
	flash     bool
	none      bool
	exclusive bool
	gag       bool
	noHist    bool
	noLog     bool
	noActive  bool
}

var colorBase = map[string]uint8{"black": 0, "red": 1, "green": 2, "yellow": 3, "blue": 4, "magenta": 5, "cyan": 6, "white": 7}

func tfColor(name string) (layout.Color, bool) {
	name = strings.ToLower(name)
	if n, err := strconv.Atoi(name); err == nil && n >= 0 && n < 256 {
		if n < 16 {
			return layout.Color{Kind: layout.ANSI, N: uint8(n)}, true
		}
		return layout.Color{Kind: layout.Indexed, N: uint8(n)}, true
	}
	bright := false
	for _, p := range []string{"bright", "br"} {
		if strings.HasPrefix(name, p) {
			bright, name = true, strings.TrimPrefix(name, p)
			break
		}
	}
	if n, ok := colorBase[name]; ok {
		if bright {
			n += 8
		}
		return layout.Color{Kind: layout.ANSI, N: n}, true
	}
	switch name {
	case "orange":
		return layout.Color{Kind: layout.Indexed, N: 208}, true
	case "gray", "grey":
		return layout.Color{Kind: layout.ANSI, N: 8}, true
	}
	if c, err := layout.ParseColor(name); err == nil && c.Kind != layout.Default {
		return c, true
	}
	return layout.Color{}, false
}

// parseTfAttrs parses an attribute list; hilite/error/warning expand to the
// attributes in the given variables (hiliteattr, error_attr, warning_attr).
func (c *Client) parseTfAttrs(s string) tfAttrs {
	return c.parseTfAttrsDepth(s, 0)
}

func (c *Client) parseTfAttrsDepth(s string, depth int) tfAttrs {
	var a tfAttrs
	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch ch {
		case ',', ' ':
		case 'n':
			a.none = true
		case 'x':
			a.exclusive = true
		case 'g':
			a.gag = true
		case 'G':
			a.noHist = true
		case 'L':
			a.noLog = true
		case 'A':
			a.noActive = true
		case 'u':
			a.ul = true
		case 'r':
			a.rev = true
		case 'B':
			a.bold = true
		case 'd':
			a.dim = true
		case 'f':
			a.flash = true
		case 'b':
		case 'h', 'E', 'W':
			if depth < 2 {
				v := map[byte]string{'h': c.in.Var("hiliteattr"), 'E': c.in.Var("error_attr"), 'W': c.in.Var("warning_attr")}[ch]
				if v == "" && ch == 'h' {
					v = "B"
				}
				sub := c.parseTfAttrsDepth(v, depth+1)
				a.merge(sub)
			}
		case 'C':
			j := i + 1
			for j < len(s) && s[j] != ',' {
				j++
			}
			name := s[i+1 : j]
			i = j - 1
			bg := false
			if strings.HasPrefix(strings.ToLower(name), "bg") {
				bg, name = true, name[2:]
			}
			if col, ok := tfColor(name); ok {
				if bg {
					a.set.BG, a.bgSet = col, true
				} else {
					a.set.FG, a.fgSet = col, true
				}
			}
		}
	}
	return a
}

func (a *tfAttrs) merge(o tfAttrs) {
	a.bold = a.bold || o.bold
	a.ul = a.ul || o.ul
	a.rev = a.rev || o.rev
	a.dim = a.dim || o.dim
	a.flash = a.flash || o.flash
	if o.fgSet {
		a.set.FG, a.fgSet = o.set.FG, true
	}
	if o.bgSet {
		a.set.BG, a.bgSet = o.set.BG, true
	}
	a.gag = a.gag || o.gag
	a.noHist = a.noHist || o.noHist
	a.noLog = a.noLog || o.noLog
	a.noActive = a.noActive || o.noActive
	a.none = a.none || o.none
	a.exclusive = a.exclusive || o.exclusive
}

// onto combines a with an existing cell attribute.
func (a tfAttrs) onto(old layout.Attr) layout.Attr {
	if a.exclusive || a.none {
		old = layout.Attr{}
	}
	if a.bold {
		old.Bold = true
	}
	if a.ul {
		old.Underline = true
	}
	if a.rev {
		old.Reverse = true
	}
	if a.dim {
		old.Dim = true
	}
	if a.fgSet {
		old.FG = a.set.FG
	}
	if a.bgSet {
		old.BG = a.set.BG
	}
	return old
}

var partialRE = regexp.MustCompile(`P(\d+)([^P]*)`)

// applyMatches styles the cells of a received line per the matching macros:
// -a attributes apply to the whole line, -P<n> attributes to regexp group n.
func (c *Client) applyMatches(cells []layout.Cell, plain string, ms []gtfparser.Match) []layout.Cell {
	if len(ms) == 0 || c.in.Var("hilite") == "off" {
		return cells
	}
	// byte offset of each cell in plain
	offs := make([]int, len(cells))
	pos := 0
	for i, cell := range cells {
		offs[i] = pos
		if cell.R != 0 {
			pos += len(string(cell.R))
		}
	}
	out := append([]layout.Cell(nil), cells...)
	paint := func(a tfAttrs, from, to int) {
		for i := range out {
			if offs[i] >= from && offs[i] < to {
				out[i].A = a.onto(out[i].A)
			}
		}
	}
	for _, m := range ms {
		at := m.Macro.Attrs
		if at == "" {
			continue
		}
		if i := strings.Index(at, "P"); i >= 0 {
			if i > 0 {
				paint(c.parseTfAttrs(at[:i]), 0, len(plain))
			}
			for _, pm := range partialRE.FindAllStringSubmatch(at[i:], -1) {
				n, _ := strconv.Atoi(pm[1])
				if n < len(m.Groups) && m.Groups[n][0] >= 0 {
					paint(c.parseTfAttrs(pm[2]), m.Groups[n][0], m.Groups[n][1])
				} else if n == 0 {
					paint(c.parseTfAttrs(pm[2]), m.Span[0], m.Span[1])
				}
			}
			continue
		}
		paint(c.parseTfAttrs(at), 0, len(plain))
	}
	return out
}

// attrsCells makes cells for text with a tf attribute list (the /echo -a path).
func (c *Client) attrsCells(text, attrs string) []layout.Cell {
	var p ui.AnsiParser
	cells := p.Parse(text)
	if attrs == "" {
		return cells
	}
	a := c.parseTfAttrs(attrs)
	for i := range cells {
		cells[i].A = a.onto(cells[i].A)
	}
	return cells
}

// lineFlags reports the gag/nohistory/nolog/noactivity flags of the matches.
func (c *Client) lineFlags(ms []gtfparser.Match) (noHist, noLog, noActive bool) {
	for _, m := range ms {
		if m.Macro.Attrs == "" {
			continue
		}
		a := c.parseTfAttrs(strings.Split(m.Macro.Attrs, "P")[0])
		noHist = noHist || a.noHist
		noLog = noLog || a.noLog
		noActive = noActive || a.noActive
	}
	return
}
