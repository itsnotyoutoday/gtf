// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package layout

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Resolver looks up a data binding. ref is "scheme:path" (gmcp:, msdp:,
// var:, expr:). The value is a decoded JSON-ish value (string, float64,
// bool, []any, map[string]any) or a Go number.
type Resolver interface {
	Resolve(ref string) (any, bool)
}

// ResolverFunc adapts a function.
type ResolverFunc func(string) (any, bool)

// Resolve implements Resolver.
func (f ResolverFunc) Resolve(ref string) (any, bool) { return f(ref) }

// Template is a parsed text template.
type Template struct {
	Src   string
	nodes []tnode
}

type tnode interface{}

type tLit string
type tRef struct {
	ref     string
	filters []string
}
type tBranch struct {
	cond string // "" = else
	body []tnode
}
type tIf struct{ branches []tBranch }
type tEach struct {
	ref  string
	body []tnode
}
type tStyle struct {
	apply func(*Attr)
	body  []tnode
}
type tWidget struct {
	name string
	args []string
}

// ParseTemplate parses template source.
func ParseTemplate(src string) (*Template, error) {
	p := &parser{s: src}
	nodes, term, err := p.block()
	if err != nil {
		return nil, err
	}
	if term != "" {
		return nil, fmt.Errorf("template: unexpected {%s}", term)
	}
	return &Template{Src: src, nodes: nodes}, nil
}

type parser struct {
	s string
	i int
}

// block parses until a closing/branching tag; it returns the terminator tag
// text ("" at end of input).
func (p *parser) block() ([]tnode, string, error) {
	var out []tnode
	var lit strings.Builder
	flush := func() {
		if lit.Len() > 0 {
			out = append(out, tLit(lit.String()))
			lit.Reset()
		}
	}
	for p.i < len(p.s) {
		c := p.s[p.i]
		switch {
		case c == '\\' && p.i+1 < len(p.s) && (p.s[p.i+1] == '{' || p.s[p.i+1] == '}' || p.s[p.i+1] == '\\'):
			lit.WriteByte(p.s[p.i+1])
			p.i += 2
		case c == '\\' && p.i+1 < len(p.s) && (p.s[p.i+1] == 'n' || p.s[p.i+1] == 't'):
			lit.WriteByte(map[byte]byte{'n': '\n', 't': '\t'}[p.s[p.i+1]])
			p.i += 2
		case c == '{':
			end := strings.IndexByte(p.s[p.i:], '}')
			if end < 0 {
				return nil, "", fmt.Errorf("template: unterminated {")
			}
			tag := strings.TrimSpace(p.s[p.i+1 : p.i+end])
			p.i += end + 1
			flush()
			name, rest := splitWord(tag)
			switch {
			case strings.HasPrefix(tag, "/"), name == "else", name == "elif":
				return out, tag, nil
			case name == "if":
				n, err := p.parseIf(rest)
				if err != nil {
					return nil, "", err
				}
				out = append(out, n)
			case name == "each":
				body, term, err := p.block()
				if err != nil {
					return nil, "", err
				}
				if term != "/each" {
					return nil, "", fmt.Errorf("template: {each} needs {/each}")
				}
				out = append(out, tEach{rest, body})
			case isStyle(name):
				ap, err := styleFunc(name, rest)
				if err != nil {
					return nil, "", err
				}
				body, term, err := p.block()
				if err != nil {
					return nil, "", err
				}
				if !strings.HasPrefix(term, "/") {
					return nil, "", fmt.Errorf("template: {%s} needs {/%s}", name, name)
				}
				out = append(out, tStyle{ap, body})
			case name == "bar" || name == "gauge" || name == "spark" || name == "hr" || name == "br" || name == "radar" || name == "scope" || name == "ansi" || name == "canvas":
				out = append(out, tWidget{name, splitArgs(rest)})
			default:
				ref, filt := splitFilters(tag)
				out = append(out, tRef{ref, filt})
			}
		default:
			lit.WriteByte(c)
			p.i++
		}
	}
	flush()
	return out, "", nil
}

func (p *parser) parseIf(cond string) (tnode, error) {
	n := tIf{}
	for {
		body, term, err := p.block()
		if err != nil {
			return nil, err
		}
		n.branches = append(n.branches, tBranch{cond, body})
		name, rest := splitWord(term)
		switch name {
		case "/if":
			return n, nil
		case "elif":
			cond = rest
		case "else":
			cond = ""
		default:
			return nil, fmt.Errorf("template: {if} needs {/if}")
		}
	}
}

func isStyle(n string) bool {
	switch n {
	case "color", "c", "bg", "bold", "dim", "italic", "underline", "reverse":
		return true
	}
	return false
}

func styleFunc(name, arg string) (func(*Attr), error) {
	switch name {
	case "bold":
		return func(a *Attr) { a.Bold = true }, nil
	case "dim":
		return func(a *Attr) { a.Dim = true }, nil
	case "italic":
		return func(a *Attr) { a.Italic = true }, nil
	case "underline":
		return func(a *Attr) { a.Underline = true }, nil
	case "reverse":
		return func(a *Attr) { a.Reverse = true }, nil
	}
	// color NAME [on BG]
	f := strings.Fields(arg)
	var fg, bg string
	if name == "bg" {
		if len(f) > 0 {
			bg = f[0]
		}
	} else {
		if len(f) > 0 {
			fg = f[0]
		}
		if len(f) > 2 && f[1] == "on" {
			bg = f[2]
		}
	}
	fc, err := ParseColor(fg)
	if err != nil {
		return nil, err
	}
	bc, err := ParseColor(bg)
	if err != nil {
		return nil, err
	}
	return func(a *Attr) {
		if fc.Kind != Default {
			a.FG = fc
		}
		if bc.Kind != Default {
			a.BG = bc
		}
	}, nil
}

func splitWord(s string) (w, rest string) {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, " \t"); i >= 0 {
		return s[:i], strings.TrimSpace(s[i+1:])
	}
	return s, ""
}

// splitArgs splits on blanks honouring "double" and 'single' quotes.
func splitArgs(s string) []string {
	var out []string
	var cur strings.Builder
	var q byte
	has := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case q != 0:
			if c == q {
				q = 0
			} else {
				cur.WriteByte(c)
			}
		case c == '"' || c == '\'':
			q, has = c, true
		case c == ' ' || c == '\t':
			if cur.Len() > 0 || has {
				out = append(out, cur.String())
				cur.Reset()
				has = false
			}
		default:
			cur.WriteByte(c)
		}
	}
	if cur.Len() > 0 || has {
		out = append(out, cur.String())
	}
	return out
}

func splitFilters(tag string) (string, []string) {
	parts := strings.Split(tag, "|")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts[0], parts[1:]
}

// Ctx is the evaluation context.
type Ctx struct {
	R     Resolver
	Width int                        // available width (for hr)
	Hist  func(key string) []float64 // value history for sparklines
	deps  map[string]bool
	attr  Attr
	out   []Cell
	scope []scopeItem
}

type scopeItem struct {
	val any
	key any
}

// Deps returns the bindings read during the evaluation.
func (c *Ctx) Deps() []string {
	out := make([]string, 0, len(c.deps))
	for d := range c.deps {
		out = append(out, d)
	}
	sort.Strings(out)
	return out
}

// Eval renders the template to styled cells; '\n' cells separate lines.
func (t *Template) Eval(c *Ctx) []Cell {
	c.deps = map[string]bool{}
	c.out = nil
	c.attr = Attr{}
	c.scope = nil
	c.run(t.nodes)
	return c.out
}

func (c *Ctx) emit(s string) {
	for _, r := range s {
		c.out = append(c.out, Cell{R: r, A: c.attr})
	}
}

func (c *Ctx) emitWith(s string, a Attr) {
	for _, r := range s {
		c.out = append(c.out, Cell{R: r, A: a})
	}
}

func (c *Ctx) run(nodes []tnode) {
	for _, n := range nodes {
		switch n := n.(type) {
		case tLit:
			c.emit(string(n))
		case tRef:
			v, _ := c.lookup(n.ref)
			c.emit(applyFilters(v, n.filters))
		case tIf:
			for _, b := range n.branches {
				if b.cond == "" || c.cond(b.cond) {
					c.run(b.body)
					break
				}
			}
		case tEach:
			v, _ := c.lookup(n.ref)
			c.each(v, n.body)
		case tStyle:
			old := c.attr
			n.apply(&c.attr)
			c.run(n.body)
			c.attr = old
		case tWidget:
			c.widget(n)
		}
	}
}

func (c *Ctx) each(v any, body []tnode) {
	switch x := v.(type) {
	case []any:
		for i, e := range x {
			c.scope = append(c.scope, scopeItem{e, float64(i + 1)})
			c.run(body)
			c.scope = c.scope[:len(c.scope)-1]
		}
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			c.scope = append(c.scope, scopeItem{x[k], k})
			c.run(body)
			c.scope = c.scope[:len(c.scope)-1]
		}
	}
}

var schemes = map[string]bool{"canvas": true, "gmcp": true, "msdp": true, "var": true, "expr": true, "mssp": true}

// lookup resolves a reference: scope refs (".", ".name", "#"), scheme refs,
// numeric/quoted literals, or a bare identifier meaning var:ident.
func (c *Ctx) lookup(ref string) (any, bool) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, false
	}
	if ref == "#" {
		if len(c.scope) > 0 {
			return c.scope[len(c.scope)-1].key, true
		}
		return nil, false
	}
	if ref[0] == '.' {
		if len(c.scope) == 0 {
			return nil, false
		}
		v := c.scope[len(c.scope)-1].val
		if ref == "." {
			return v, true
		}
		return Path(v, ref[1:])
	}
	if f, err := strconv.ParseFloat(ref, 64); err == nil {
		return f, true
	}
	if ref[0] == '"' && len(ref) >= 2 && ref[len(ref)-1] == '"' {
		return ref[1 : len(ref)-1], true
	}
	if i := strings.IndexByte(ref, ':'); i > 0 && schemes[ref[:i]] {
		c.deps[ref] = true
		if c.R == nil {
			return nil, false
		}
		return c.R.Resolve(ref)
	}
	k := "var:" + ref
	c.deps[k] = true
	if c.R == nil {
		return nil, false
	}
	return c.R.Resolve(k)
}

// Path walks a dotted path ("a.b.0") through maps and arrays (arrays are
// indexed from 0).
func Path(v any, path string) (any, bool) {
	if path == "" {
		return v, true
	}
	for _, seg := range strings.Split(path, ".") {
		switch x := v.(type) {
		case map[string]any:
			nv, ok := x[seg]
			if !ok {
				return nil, false
			}
			v = nv
		case []any:
			i, err := strconv.Atoi(seg)
			if err != nil || i < 0 || i >= len(x) {
				return nil, false
			}
			v = x[i]
		default:
			return nil, false
		}
	}
	return v, true
}

// cond evaluates "[not] a [op b] [and|or ...]".
func (c *Ctx) cond(s string) bool {
	toks := splitArgs(s)
	res := false
	join := ""
	for i := 0; i < len(toks); {
		neg := false
		if toks[i] == "not" || toks[i] == "!" {
			neg = true
			i++
		}
		if i >= len(toks) {
			break
		}
		a := toks[i]
		i++
		var r bool
		if i+1 < len(toks) && isOp(toks[i]) {
			r = c.compare(a, toks[i], toks[i+1])
			i += 2
		} else {
			v, _ := c.lookup(a)
			r = truthy(v)
		}
		if neg {
			r = !r
		}
		switch join {
		case "and":
			res = res && r
		case "or":
			res = res || r
		default:
			res = r
		}
		join = ""
		if i < len(toks) && (toks[i] == "and" || toks[i] == "or" || toks[i] == "&&" || toks[i] == "||") {
			join = map[string]string{"&&": "and", "||": "or"}[toks[i]]
			if join == "" {
				join = toks[i]
			}
			i++
		}
	}
	return res
}

func isOp(s string) bool {
	switch s {
	case "==", "!=", "<", "<=", ">", ">=", "=~":
		return true
	}
	return false
}

func (c *Ctx) compare(a, op, b string) bool {
	av, _ := c.lookup(a)
	bv, bok := c.lookup(b)
	if !bok || toString(bv) == "" {
		// a bare word on the right is a literal ("aa == red"), unless it names a
		// variable that holds a value ("hp < maxhp")
		bv = b
	}
	af, aok := toFloat(av)
	bf, bok2 := toFloat(bv)
	if aok && bok2 && op != "=~" {
		switch op {
		case "==":
			return af == bf
		case "!=":
			return af != bf
		case "<":
			return af < bf
		case "<=":
			return af <= bf
		case ">":
			return af > bf
		case ">=":
			return af >= bf
		}
	}
	as, bs := toString(av), toString(bv)
	switch op {
	case "==":
		return as == bs
	case "!=":
		return as != bs
	case "<":
		return as < bs
	case "<=":
		return as <= bs
	case ">":
		return as > bs
	case ">=":
		return as >= bs
	case "=~":
		return strings.Contains(as, bs)
	}
	return false
}

func truthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case float64:
		return x != 0
	case int:
		return x != 0
	case int64:
		return x != 0
	case string:
		if f, err := strconv.ParseFloat(x, 64); err == nil {
			return f != 0
		}
		return x != "" && x != "off" && x != "false"
	case []any:
		return len(x) > 0
	case map[string]any:
		return len(x) > 0
	}
	return true
}

func toFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case bool:
		if x {
			return 1, true
		}
		return 0, true
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(x), 64)
		return f, err == nil
	}
	return 0, false
}

// ToString formats a value for display.
func toString(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case float64:
		if x == math.Trunc(x) && math.Abs(x) < 1e15 {
			return strconv.FormatInt(int64(x), 10)
		}
		return strconv.FormatFloat(x, 'g', -1, 64)
	case bool:
		if x {
			return "1"
		}
		return "0"
	case []any:
		p := make([]string, len(x))
		for i, e := range x {
			p[i] = toString(e)
		}
		return strings.Join(p, ", ")
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		p := make([]string, len(keys))
		for i, k := range keys {
			p[i] = k + "=" + toString(x[k])
		}
		return strings.Join(p, " ")
	}
	return fmt.Sprint(v)
}

// ToString is the exported formatter (used by clients exposing values to scripts).
func ToString(v any) string { return toString(v) }

func applyFilters(v any, filters []string) string {
	s := toString(v)
	cur := any(v)
	for _, f := range filters {
		name, arg, _ := strings.Cut(f, ":")
		name = strings.TrimSpace(name)
		arg = strings.TrimSpace(arg)
		switch {
		case strings.HasPrefix(f, "%"):
			s = formatf(f, cur)
		case name == "upper":
			s = strings.ToUpper(s)
		case name == "lower":
			s = strings.ToLower(s)
		case name == "title":
			if s != "" {
				s = strings.ToUpper(s[:1]) + s[1:]
			}
		case name == "trim":
			s = strings.TrimSpace(s)
		case name == "int":
			if x, ok := toFloat(cur); ok {
				s = strconv.FormatInt(int64(x), 10)
			}
		case name == "round":
			if x, ok := toFloat(cur); ok {
				s = strconv.FormatInt(int64(math.Round(x)), 10)
			}
		case name == "k":
			if x, ok := toFloat(cur); ok {
				s = humanize(x)
			}
		case name == "default":
			if s == "" {
				s = arg
			}
		case name == "pad", name == "ljust":
			n, _ := strconv.Atoi(arg)
			for utf8.RuneCountInString(s) < n {
				s += " "
			}
		case name == "rpad", name == "rjust":
			n, _ := strconv.Atoi(arg)
			for utf8.RuneCountInString(s) < n {
				s = " " + s
			}
		case name == "trunc":
			n, _ := strconv.Atoi(arg)
			if r := []rune(s); len(r) > n && n >= 0 {
				s = string(r[:n])
			}
		}
		cur = s
	}
	return s
}

func humanize(x float64) string {
	switch ax := math.Abs(x); {
	case ax >= 1e9:
		return trimf(x/1e9) + "G"
	case ax >= 1e6:
		return trimf(x/1e6) + "M"
	case ax >= 1e3:
		return trimf(x/1e3) + "k"
	}
	return trimf(x)
}

func trimf(x float64) string {
	s := strconv.FormatFloat(x, 'f', 1, 64)
	return strings.TrimSuffix(s, ".0")
}

func formatf(f string, v any) string {
	verb := f[len(f)-1]
	switch verb {
	case 'd', 'x', 'X', 'o', 'b', 'c':
		if x, ok := toFloat(v); ok {
			return fmt.Sprintf(f, int64(x))
		}
	case 'f', 'e', 'g', 'E', 'G':
		if x, ok := toFloat(v); ok {
			return fmt.Sprintf(f, x)
		}
	case 's':
		return fmt.Sprintf(f, toString(v))
	}
	return toString(v)
}
