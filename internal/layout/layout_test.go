// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package layout

import (
	"strings"
	"testing"
)

type mapRes map[string]any

func (m mapRes) Resolve(ref string) (any, bool) {
	if v, ok := m[ref]; ok {
		return v, true
	}
	// gmcp:Pkg.path lookups into stored packages
	if i := strings.IndexByte(ref, ':'); i > 0 {
		scheme, path := ref[:i], ref[i+1:]
		for k, v := range m {
			if strings.HasPrefix(k, scheme+":") && strings.HasPrefix(path, k[i+1:]+".") {
				if r, ok := Path(v, path[len(k)-i:]); ok {
					return r, true
				}
			}
		}
	}
	return nil, false
}

func render(t *testing.T, src string, r Resolver, w int) (string, []string) {
	t.Helper()
	tpl, err := ParseTemplate(src)
	if err != nil {
		t.Fatal(err)
	}
	c := &Ctx{R: r, Width: w}
	cells := tpl.Eval(c)
	var b strings.Builder
	for _, c := range cells {
		b.WriteRune(c.R)
	}
	return b.String(), c.Deps()
}

func TestInterpolationFiltersAndDeps(t *testing.T) {
	r := mapRes{"gmcp:Char.Vitals": map[string]any{"hp": 45.0, "maxhp": 60.0, "name": "bob"}, "var:level": "7", "msdp:HEALTH": "12"}
	got, deps := render(t, "{gmcp:Char.Vitals.name|upper} L{level|%03d} hp {gmcp:Char.Vitals.hp}/{gmcp:Char.Vitals.maxhp} m{msdp:HEALTH|rpad:4}|", r, 0)
	if got != "BOB L007 hp 45/60 m  12|" {
		t.Fatalf("got %q", got)
	}
	if strings.Join(deps, ",") != "gmcp:Char.Vitals.hp,gmcp:Char.Vitals.maxhp,gmcp:Char.Vitals.name,msdp:HEALTH,var:level" {
		t.Fatalf("deps %v", deps)
	}
}

func TestConditionalsAndEach(t *testing.T) {
	r := mapRes{"var:hp": 10.0, "gmcp:Room.Info": map[string]any{"exits": map[string]any{"n": "a", "s": "b"}, "who": []any{"x", "y"}}}
	got, _ := render(t, "{if hp < 20}LOW{elif hp < 50}MID{else}OK{/if} {if not hp > 20 and hp > 5}yes{/if}", r, 0)
	if got != "LOW yes" {
		t.Fatalf("%q", got)
	}
	got, _ = render(t, "{each gmcp:Room.Info.exits}{#}={.} {/each}|{each gmcp:Room.Info.who}[{#}:{.}]{/each}", r, 0)
	if got != "n=a s=b |[1:x][2:y]" {
		t.Fatalf("%q", got)
	}
}

func TestBarWidgetAndColor(t *testing.T) {
	r := mapRes{"var:hp": 50.0, "var:maxhp": 100.0}
	tpl, _ := ParseTemplate("{color red}{bar hp maxhp width=10 chars=ascii color=red}{/color}")
	cells := tpl.Eval(&Ctx{R: r})
	var s string
	for _, c := range cells {
		s += string(c.R)
	}
	if s != "#####-----" {
		t.Fatalf("%q", s)
	}
	if cells[0].A.FG.Kind != ANSI || cells[0].A.FG.N != 1 {
		t.Fatalf("%+v", cells[0].A)
	}
	got, _ := render(t, "{gauge hp maxhp width=8 chars=ascii label=HP}", r, 0)
	if got != "HP [####----] 50/100" {
		t.Fatalf("%q", got)
	}
	got, _ = render(t, "{bar 25 100 width=4}", r, 0)
	if got != "█░░░" {
		t.Fatalf("%q", got)
	}
}

func TestEscapesAndErrors(t *testing.T) {
	got, _ := render(t, `a\{b\}\n{hr width=3}`, mapRes{}, 0)
	if got != "a{b}\n───" {
		t.Fatalf("%q", got)
	}
	for _, bad := range []string{"{if x}", "{each x}", "{var:x", "{color nosuchcolor}z{/color}"} {
		if _, err := ParseTemplate(bad); err == nil {
			t.Fatalf("%q should fail", bad)
		}
	}
}

func TestSpark(t *testing.T) {
	l := New("t")
	for _, v := range []float64{1, 2, 3, 4} {
		l.Observe("var:x", v)
	}
	tpl, _ := ParseTemplate("{spark var:x width=4}")
	cells := tpl.Eval(&Ctx{R: mapRes{"var:x": 4.0}, Hist: func(k string) []float64 { return l.hist[k] }})
	if string(cells[0].R) != "▁" || string(cells[3].R) != "█" || len(cells) != 4 {
		t.Fatalf("%v", cells)
	}
}

func TestEngineDockingAndDegrade(t *testing.T) {
	l := New("t")
	if err := l.Set(&Panel{Name: "side", Dock: DockLeft, W: Dim{Fixed, 20}, Border: "single", Title: "Stat", Body: "HP {var:hp}"}); err != nil {
		t.Fatal(err)
	}
	l.Set(&Panel{Name: "top", Dock: DockTop, H: Dim{Fixed, 1}, Body: "room"})
	l.Set(&Panel{Name: "bot", Dock: DockBottom, Body: "a\nb"})
	r := mapRes{"var:hp": 9.0}
	f := l.Compute(80, 24, r)
	if len(f.Panels) != 3 || f.Main != (Rect{20, 1, 60, 21}) {
		t.Fatalf("%+v", f)
	}
	if f.Panels[0].Rect != (Rect{0, 0, 20, 24}) {
		t.Fatalf("side %+v", f.Panels[0].Rect)
	}
	g := l.Render(l.Get("side"), 20, 24, r)
	if !strings.HasPrefix(g.Row(0), "┌─ Stat ─") || !strings.HasPrefix(g.Row(1), "│HP 9") {
		t.Fatalf("%q\n%q", g.Row(0), g.Row(1))
	}
	// narrow terminal: side panel dropped to keep MinMain
	f = l.Compute(40, 24, r)
	for _, p := range f.Panels {
		if p.P.Name == "side" {
			t.Fatal("side panel should be hidden on a narrow terminal")
		}
	}
	// dirty tracking
	if l.Dirty("side") {
		t.Fatal("rendered panel should be clean")
	}
	l.Changed("var:hp")
	if !l.Dirty("side") {
		t.Fatal("change of a dependency must dirty the panel")
	}
	if n := l.Changed("var:other"); len(n) != 0 && n[0] == "side" {
		t.Fatal("unrelated change dirtied side")
	}
}

func TestCondBareWordIsLiteral(t *testing.T) {
	r := mapRes{"var:aa": "red", "var:hp": 5.0, "var:maxhp": 10.0}
	c := &Ctx{R: r, deps: map[string]bool{}}
	for _, tc := range []struct {
		cond string
		want bool
	}{
		{"aa == red", true}, {`aa == "red"`, true}, {"aa != red", false}, {"aa == blue", false},
		{"hp < maxhp", true}, {"hp > maxhp", false},
	} {
		if got := c.cond(tc.cond); got != tc.want {
			t.Errorf("cond(%q) = %v, want %v", tc.cond, got, tc.want)
		}
	}
}
