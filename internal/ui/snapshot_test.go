// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package ui

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/itsnotyoutoday/gtf/internal/layout"
)

func TestScreenStateRoundTrip(t *testing.T) {
	scr := NewScreen("w", 100)
	red := layout.Attr{FG: layout.Color{Kind: layout.ANSI, N: 1}, Bold: true}
	scr.rewrap(20, WrapConfig{Wrap: true, Space: 2})
	for i := 0; i < 30; i++ {
		cells := PlainCells("line number that is long enough to wrap around", layout.Attr{})
		for j := 5; j < 10; j++ {
			cells[j].A = red
		}
		scr.Add(cells, 5)
	}
	scr.Add(PlainCells("wide: 中文", layout.Attr{}), 5)
	scr.Unseen = 3
	st := scr.State()
	js, err := json.Marshal(st)
	if err != nil {
		t.Fatal(err)
	}
	var st2 ScreenState
	if err := json.Unmarshal(js, &st2); err != nil {
		t.Fatal(err)
	}
	got, err := RestoreScreen(st2, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.SB.Len() != scr.SB.Len() || got.vis != scr.vis || got.Paused != scr.Paused || got.Unseen != 3 || got.outcount != scr.outcount {
		t.Fatalf("scalars differ: %d/%d vis %d/%d", got.SB.Len(), scr.SB.Len(), got.vis, scr.vis)
	}
	for i := 0; i < scr.SB.Len(); i++ {
		if !reflect.DeepEqual(got.SB.Phys(i), scr.SB.Phys(i)) {
			t.Fatalf("physical line %d differs", i)
		}
	}
	if !got.Paused {
		t.Fatal("expected the pager to be engaged")
	}
	// the restored screen keeps paging like the original
	scr.Scroll(2, 5)
	got.Scroll(2, 5)
	if got.vis != scr.vis {
		t.Fatalf("scroll: %d vs %d", got.vis, scr.vis)
	}
}

func TestRestoreScreenRefusesGarbage(t *testing.T) {
	for name, st := range map[string]ScreenState{
		"too new":  {V: ScreenStateVersion + 1, Attrs: []layout.Attr{{}}},
		"no attrs": {V: 1},
		"bad run":  {V: 1, Attrs: []layout.Attr{{}}, Lines: []LineState{{T: "ab", R: []int{5, 0}}}},
		"bad idx":  {V: 1, Attrs: []layout.Attr{{}}, Lines: []LineState{{T: "ab", R: []int{2, 9}}}},
	} {
		if _, err := RestoreScreen(st, nil); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}
