// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package ui

import (
	"strings"
	"testing"

	"github.com/itsnotyoutoday/gtf/internal/layout"
	"github.com/itsnotyoutoday/gtf/internal/ui/vt"
)

func newUI(w, h int) (*UI, *vt.Screen) {
	s := vt.New(w, h)
	return New(s), s
}

func addText(u *UI, s string) {
	var p AnsiParser
	u.AddLine(u.Scr, p.Parse(s))
}

func statusLine(w int, text string) [][]layout.Cell {
	line := []rune(strings.Repeat("_", w))
	copy(line, []rune(text))
	return [][]layout.Cell{PlainCells(string(line), layout.Attr{})}
}

func TestBasicLayoutMatchesTf(t *testing.T) {
	u, scr := newUI(80, 24)
	u.Wrap.Size = 79 // tf sets wrapsize to columns-1
	addText(u, "TinyFugue version development")
	addText(u, "Copyright (C) 1993, 1994, 1995, 1996, 1997, 1998, 1999, 2002, 2003, 2004, 2005, 2006-2007 Ken Keys (kenkeys@users.sourceforge.net)")
	addText(u, "hello")
	u.SetStatus(statusLine(80, "_________(no world)"))
	u.In.Insert("/echo hello")
	u.Render()
	l := scr.Lines()
	// output bottom-aligned above the status line, wrapped with a 4 space indent
	if l[16] != "TinyFugue version development" || l[17] != "Copyright (C) 1993, 1994, 1995, 1996, 1997, 1998, 1999, 2002, 2003, 2004," || l[18] != "    2005, 2006-2007 Ken Keys (kenkeys@users.sourceforge.net)" || l[19] != "hello" {
		t.Fatalf("\n%s", scr.Dump())
	}
}

func TestInputWindowScrollsLikeTf(t *testing.T) {
	u, scr := newUI(80, 24)
	u.SetStatus(statusLine(80, "status"))
	for _, s := range []string{"/echo one", "/echo two", "/echo three"} {
		u.In.Insert(s)
		u.Submit()
	}
	u.In.Insert("xyz")
	u.Render()
	l := scr.Lines()
	if l[21] != "/echo three" || l[22] != "xyz" || scr.Y != 23-1 || scr.X != 3 {
		// ISize 3: two old lines then the live line
		if !(l[21] == "/echo two" && l[22] == "/echo three" && l[23] == "xyz") {
			t.Fatalf("\n%s", scr.Dump())
		}
	}
}

func TestMorePager(t *testing.T) {
	u, scr := newUI(40, 10) // output rows = 10-1-3 = 6
	u.SetStatus(statusLine(40, "st"))
	for i := 1; i <= 10; i++ {
		addText(u, "line "+string(rune('0'+i%10)))
	}
	addText(u, "line X")
	u.Render()
	if !u.Scr.Paused || u.Scr.Held() != 5 {
		t.Fatalf("paused=%v held=%d\n%s", u.Scr.Paused, u.Scr.Held(), scr.Dump())
	}
	if scr.Row(5) != "line 6" {
		t.Fatalf("\n%s", scr.Dump())
	}
	u.Scr.Scroll(u.Winlines(), u.Winlines())
	u.Render()
	if u.Scr.Paused || scr.Row(5) != "line X" {
		t.Fatalf("\n%s", scr.Dump())
	}
}

func TestPanelDocksAndRestores(t *testing.T) {
	u, scr := newUI(80, 24)
	u.Layout = layout.New("t")
	u.Resolver = layout.ResolverFunc(func(string) (any, bool) { return 42.0, true })
	u.Layout.Set(&layout.Panel{Name: "side", Dock: layout.DockLeft, W: layout.Dim{Kind: layout.Fixed, N: 20}, Border: "single", Title: "Vitals", Body: "HP {var:hp}"})
	addText(u, "a long line of text that must wrap inside the narrowed main region because of the sidebar")
	u.SetStatus(statusLine(80, "st"))
	u.Render()
	if !strings.HasPrefix(scr.Row(0), "┌─ Vitals") || !strings.HasPrefix(scr.Row(1), "│HP 42") {
		t.Fatalf("\n%s", scr.Dump())
	}
	if !strings.HasPrefix(scr.Row(19), "└") || strings.Contains(scr.Row(20), "│") {
		t.Fatalf("panel must span the output rows only\n%s", scr.Dump())
	}
	found := false
	for y := 0; y < 24; y++ {
		if strings.Contains(scr.Row(y), "a long line of text") && strings.Index(scr.Row(y), "a long") >= 20 {
			found = true
		}
	}
	if !found {
		t.Fatalf("\n%s", scr.Dump())
	}
	u.Layout.Remove("side")
	u.Redraw()
	u.Render()
	if strings.Contains(scr.Text(), "│") {
		t.Fatalf("\n%s", scr.Dump())
	}
}

func TestResizeRewraps(t *testing.T) {
	u, scr := newUI(40, 12)
	addText(u, strings.Repeat("word ", 20))
	u.SetStatus(statusLine(40, "s"))
	u.Render()
	scr.Resize(80, 12)
	u.Resize()
	u.Render()
	n := 0
	for _, r := range scr.Lines() {
		if strings.HasPrefix(r, "word") || strings.HasPrefix(r, "    word") {
			n++
		}
	}
	if n != 2 {
		t.Fatalf("\n%s", scr.Dump())
	}
}

func TestAnsiColors(t *testing.T) {
	u, scr := newUI(40, 10)
	addText(u, "\x1b[31mred\x1b[0m plain \x1b[1;38;5;208mbold-orange\x1b[0m")
	u.SetStatus(statusLine(40, "s"))
	u.Render()
	row := 5
	if scr.Row(row) != "red plain bold-orange" {
		t.Fatalf("\n%s", scr.Dump())
	}
	if scr.CellAt(0, row).Attr != "31" || scr.CellAt(4, row).Attr != "" || !strings.Contains(scr.CellAt(11, row).Attr, "fg=208") {
		t.Fatalf("%q %q %q", scr.CellAt(0, row).Attr, scr.CellAt(4, row).Attr, scr.CellAt(11, row).Attr)
	}
}

func TestNonVisual(t *testing.T) {
	u, scr := newUI(40, 6)
	u.SetVisual(false)
	addText(u, "one")
	addText(u, "two")
	u.In.Insert("typed")
	u.Render()
	if !strings.Contains(scr.Text(), "one") || !strings.Contains(scr.Text(), "typed") {
		t.Fatalf("\n%s", scr.Dump())
	}
}
