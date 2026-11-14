package gtfparser

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Macro is a /def'd macro: optionally a trigger, hook or key binding.
type Macro struct {
	ID       int
	Name     string
	Body     string
	Trigger  string
	Hook     string
	Bind     string
	Matching string // simple, glob, regexp
	Shots    int
	Priority int
	Fall     bool // -F fall-through
	Gag      bool
	Cond     string // -E
	Attrs    string
	World    string
	re       *regexp.Regexp
}

// rawCmds take their argument text unexpanded (their bodies are code).
var rawCmds = map[string]bool{"def": true, "trig": true, "hilite": true, "gag": true, "hook": true, "bind": true, "alias": false}

// splitOptWord pulls the next /def option word: -ttext, -t'quoted text',
// -h"event pattern".
func splitOptWord(s string) (word, rest string) {
	i := 0
	for i < len(s) && s[i] != ' ' && s[i] != '\t' {
		if (s[i] == '"' || s[i] == '\'' || s[i] == '`') && i > 1 {
			q := s[i]
			i++
			for i < len(s) && s[i] != q {
				if s[i] == '\\' {
					i++
				}
				i++
			}
		}
		i++
	}
	if i > len(s) {
		i = len(s)
	}
	return s[:i], strings.TrimLeft(s[i:], " \t")
}

func unquoteOpt(v string) string {
	if len(v) >= 2 && (v[0] == '"' || v[0] == '\'' || v[0] == '`') && v[len(v)-1] == v[0] {
		inner := v[1 : len(v)-1]
		var b strings.Builder
		for i := 0; i < len(inner); i++ {
			if inner[i] == '\\' && i+1 < len(inner) {
				i++
			}
			b.WriteByte(inner[i])
		}
		return b.String()
	}
	return v
}

func (in *Interp) parseDef(args string) (*Macro, error) {
	m := &Macro{Matching: in.globals["matching"].String()}
	rest := strings.TrimLeft(args, " \t")
	for strings.HasPrefix(rest, "-") && len(rest) > 1 {
		var w string
		w, rest = splitOptWord(rest)
		opt, val := w[1], w[2:]
		switch opt {
		case 'i': // invisible: ignored
		case 'm':
			m.Matching = val
		case 'n':
			m.Shots, _ = strconv.Atoi(val)
		case 'p':
			m.Priority, _ = strconv.Atoi(val)
		case 'E':
			m.Cond = unquoteOpt(val)
		case 't':
			m.Trigger = unquoteOpt(val)
		case 'h':
			m.Hook = unquoteOpt(val)
		case 'b':
			m.Bind = unquoteOpt(val)
		case 'a', 'f', 'T', 'w', 'c', 'P', 'F', 'g', 'B', 'k', 'q':
			switch opt {
			case 'F':
				m.Fall = true
			case 'g':
				m.Gag = true
			case 'a':
				m.Attrs += val
			case 'w':
				m.World = val
			case 'P':
				m.Attrs += "P" + val
				m.Matching = "regexp"
			case 'T': // like -t but different matching style
				m.Trigger = unquoteOpt(val)
			}
		default:
			return nil, fmt.Errorf("/def: invalid option '%c'", opt)
		}
	}
	if m.Matching == "" {
		m.Matching = "glob"
	}
	// [name] [= body]
	name := ""
	if !strings.HasPrefix(rest, "=") {
		i := 0
		for i < len(rest) && rest[i] != ' ' && rest[i] != '\t' && rest[i] != '=' {
			i++
		}
		name, rest = rest[:i], strings.TrimLeft(rest[i:], " \t")
	}
	if strings.HasPrefix(rest, "=") {
		m.Body = strings.TrimLeft(rest[1:], " \t")
	}
	m.Name = name
	return m, nil
}

// Define installs a macro, replacing one of the same name.
func (in *Interp) Define(m *Macro) {
	if m.Name != "" {
		if old, ok := in.macros[m.Name]; ok {
			in.removeFromList(old)
		}
		in.macros[m.Name] = m
	}
	in.nextID++
	m.ID = in.nextID
	if m.Matching == "regexp" && m.Trigger != "" {
		m.re, _ = regexp.Compile(m.Trigger)
	}
	in.list = append(in.list, m)
}

func (in *Interp) removeFromList(m *Macro) {
	for i, x := range in.list {
		if x == m {
			in.list = append(in.list[:i], in.list[i+1:]...)
			return
		}
	}
}

func (in *Interp) Undef(name string) bool {
	m, ok := in.macros[name]
	if !ok {
		return false
	}
	delete(in.macros, name)
	in.removeFromList(m)
	return true
}

// Macros lists defined macros by name.
func (in *Interp) Macros() []*Macro {
	var out []*Macro
	for _, m := range in.macros {
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (m *Macro) matches(line string) (ok bool, regs []string, left, right string) {
	switch m.Matching {
	case "regexp":
		if m.re == nil {
			return false, nil, "", ""
		}
		loc := m.re.FindStringSubmatchIndex(line)
		if loc == nil {
			return false, nil, "", ""
		}
		for i := 0; i+1 < len(loc); i += 2 {
			if loc[i] < 0 {
				regs = append(regs, "")
			} else {
				regs = append(regs, line[loc[i]:loc[i+1]])
			}
		}
		return true, regs, line[:loc[0]], line[loc[1]:]
	case "simple":
		return line == m.Trigger, nil, "", ""
	}
	return globMatch(m.Trigger, line), nil, "", ""
}

// ProcessLine runs the triggers matching a line received from a world. It
// reports whether the line was gagged.
func (in *Interp) ProcessLine(line string) (gag bool) {
	var hits []*Macro
	for _, m := range in.list {
		if m.Trigger == "" {
			continue
		}
		if m.Cond != "" && !in.condition(m.Cond) {
			continue
		}
		hits = append(hits, m)
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].Priority > hits[j].Priority })
	topPri := 0
	first := true
	for _, m := range hits {
		if !first && m.Priority < topPri {
			break
		}
		ok, regs, l, r := m.matches(line)
		if !ok {
			continue
		}
		if first {
			first, topPri = false, m.Priority
		}
		if m.Gag {
			gag = true
		}
		in.runTrigger(m, line, regs, l, r)
		if m.Shots > 0 {
			m.Shots--
			if m.Shots == 0 {
				in.Undef(m.Name)
				in.removeFromList(m)
			}
		}
		if !m.Fall {
			break
		}
	}
	return gag
}

func (in *Interp) runTrigger(m *Macro, line string, regs []string, l, r string) {
	in.depth++
	f := in.pushFrame(m.Name, line)
	if regs != nil {
		f.regs, f.pl, f.pr = regs, l, r
	}
	oldExp := in.noExp
	in.noExp = false
	in.execText(m.Body)
	in.noExp = oldExp
	in.ctl = ctlNone
	in.popFrame()
	in.depth--
}

// Hook fires macros hooked on an event.
func (in *Interp) Hook(event, args string) {
	for _, m := range append([]*Macro(nil), in.list...) {
		if m.Hook == "" {
			continue
		}
		ev, pat := firstWord(m.Hook)
		match := false
		for _, e := range strings.Split(ev, "|") {
			if strings.EqualFold(e, event) {
				match = true
			}
		}
		if !match || (pat != "" && !globMatch(pat, args)) {
			continue
		}
		in.depth++
		in.pushFrame(m.Name, args)
		in.execText(m.Body)
		in.ctl = ctlNone
		in.popFrame()
		in.depth--
	}
}
