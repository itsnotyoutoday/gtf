// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package cmcpui

import (
	"os"
	"strings"
	"testing"

	"github.com/itsnotyoutoday/cmcp/go/pack"
)

// The concourse is a world.place screen of core widgets only (spec 20.7): it must draw with no
// ship, bind $ctx.data, and be operable from the keyboard.

func concourseCtx() map[string]any {
	return map[string]any{"key": "place:concourse", "type": "world.place", "id": "concourse", "name": "Spacedock Concourse",
		"data": map[string]any{
			"title":   "Spacedock Concourse",
			"welcome": "Welcome to the concourse. Pick a ship and board it, or look around.",
			"test":    "Board a ship, man a console, leave again: this screen stays.",
			"ships": map[string]any{
				"ss-booty-king": map[string]any{"name": "SS Booty King", "class": "Freighter", "look": "ops"},
				"uss-kestrel":   map[string]any{"name": "USS Kestrel", "class": "Scout", "look": "tng"},
			}}}
}

func drawConcourse(t *testing.T, w, h int, st *State) (*Frame, *pack.Screen) {
	t.Helper()
	raw, err := os.ReadFile("testdata/fspace-concourse.pack.json")
	if err != nil {
		t.Fatal(err)
	}
	p, err := pack.Parse(raw, pack.Options{})
	if err != nil {
		t.Fatal(err)
	}
	scr := p.Screens["concourse"]
	if st == nil {
		st = NewState()
	}
	env := &pack.Env{UI: st.UI, Ctx: concourseCtx(), CanSend: func(string) bool { return true }}
	tree := scr.Bind(env, pack.Caps{Class: "cell"})
	panels, mn, _ := tree.FocusPlan(nil)
	st.SetupModel(Plan{Panels: panels, Mnemonics: mn}, "tab")
	return Draw(tree, w, h, Env{Theme: ThemeByName("tng"), Now: now, Title: scr.Title, NoAlert: true, NoNav: true, Mnemonics: mn}, st), scr
}

func TestConcourseDrawsWithoutShip(t *testing.T) {
	for _, sz := range [][2]int{{80, 24}, {120, 40}} {
		f, _ := drawConcourse(t, sz[0], sz[1], nil)
		txt := f.Grid.Text()
		for _, want := range []string{"Spacedock Concourse", "SS Booty King", "USS Kestrel", "MAP", "GUIDE", "HOME", "ABOUT"} {
			if !strings.Contains(txt, want) {
				t.Errorf("%dx%d: %q missing:\n%s", sz[0], sz[1], want, txt)
			}
		}
		if strings.Contains(txt, "ALERT") || !strings.Contains(txt, "SPACEDOCK CONCOURSE") {
			t.Errorf("%dx%d: frame must carry the place title and no alert segment:\n%s", sz[0], sz[1], txt)
		}
		if strings.Contains(txt, "SENSORS") || strings.Contains(txt, "ENGINEER") {
			t.Errorf("%dx%d: ship stations on a place:\n%s", sz[0], sz[1], txt)
		}
		if f.Compact {
			t.Errorf("%dx%d: compact", sz[0], sz[1])
		}
		for i, a := range f.Boxes {
			for j := i + 1; j < len(f.Boxes); j++ {
				if overlap(a, f.Boxes[j]) {
					t.Errorf("%dx%d: widgets %+v and %+v overlap", sz[0], sz[1], a, f.Boxes[j])
				}
			}
		}
		t.Logf("%dx%d\n%s", sz[0], sz[1], txt)
	}
}

// Keyboard: Tab reaches the buttons; a button's letter presses it and yields the click.
func TestConcourseKeyboard(t *testing.T) {
	st := NewState()
	f, _ := drawConcourse(t, 120, 40, st)
	if r := Dispatch(f, st, "tab", true, now); !st.Active || !r.Used {
		t.Fatalf("Tab did not enter focus mode: %+v", r)
	}
	var home *Item
	for i := range f.Items {
		if f.Items[i].Kind == "button" && str(f.Items[i].Node.Props["text"]) == "HOME" {
			home = &f.Items[i]
		}
	}
	if home == nil || home.Mnemonic == 0 {
		t.Fatal("HOME button has no mnemonic")
	}
	r := Dispatch(f, st, strings.ToLower(string(home.Mnemonic)), true, now)
	if len(r.Events) != 1 || r.Events[0].Name != "click" {
		t.Fatalf("HOME key: %+v", r)
	}
}
