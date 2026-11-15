// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package cmcpui

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/itsnotyoutoday/cmcp/go/pack"
)

var updateGolden = flag.Bool("update", false, "rewrite golden files")

func loadDocs(t testing.TB) map[string]any {
	docs := map[string]any{}
	files, _ := filepath.Glob("../client/testdata/cmcp/*--full.json")
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var m struct {
			Package string `json:"package"`
			Payload struct {
				D any `json:"d"`
			} `json:"payload"`
		}
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatal(err)
		}
		docs[m.Package] = m.Payload.D
	}
	return docs
}

func loadPack(t testing.TB) *pack.Pack {
	raw, err := os.ReadFile("testdata/fspace-crew.pack.json")
	if err != nil {
		t.Fatal(err)
	}
	p, err := pack.Parse(raw, pack.Options{})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

var now = time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)

func draw(t testing.TB, scr *pack.Screen, docs map[string]any, w, h int, st *State, th string) *Frame {
	if st == nil {
		st = NewState()
	}
	env := &pack.Env{Docs: docs, UI: st.UI, CanSend: func(string) bool { return true }}
	tree := scr.Bind(env, pack.Caps{Class: "cell"})
	panels, mn, _ := tree.FocusPlan(nil)
	st.SetupModel(Plan{Panels: panels, Mnemonics: mn}, "tab")
	return Draw(tree, w, h, Env{Theme: ThemeByName(th), Now: now, Header: scr.Title, Mnemonics: mn}, st)
}

func golden(t *testing.T, name, got string) {
	t.Helper()
	p := filepath.Join("testdata", "golden", name+".txt")
	if *updateGolden {
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(got), 0o644)
		return
	}
	want, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("no golden %s (go test -run %s -args -update)", p, t.Name())
	}
	if string(want) != got {
		t.Errorf("%s differs:\n--- got\n%s\n--- want\n%s", name, got, want)
	}
}

// Every screen of the crew pack draws on the example data with its text pane in place.
func TestCrewPackScreensDraw(t *testing.T) {
	p := loadPack(t)
	docs := loadDocs(t)
	for _, id := range p.ScreenIDs {
		f := draw(t, p.Screens[id], docs, 120, 40, nil, "tng")
		if f.Compact || f.Text.H < minTextRows {
			t.Errorf("%s: text pane %+v compact=%v", id, f.Text, f.Compact)
		}
		// nothing drawn inside the text pane
		for y := f.Text.Y; y < f.Text.Y+f.Text.H; y++ {
			for x := f.Text.X; x < f.Text.X+f.Text.W; x++ {
				if c := f.Grid.At(x, y); c.R != ' ' {
					t.Fatalf("%s: widget drawn over the text pane at %d,%d: %q", id, x, y, c.R)
				}
			}
		}
	}
}

func TestTransporterGolden(t *testing.T) {
	p := loadPack(t)
	f := draw(t, p.Screens["transporter"], loadDocs(t), 120, 40, nil, "plain")
	golden(t, "transporter", f.Grid.Text())
	if !strings.Contains(f.Grid.Text(), "Lt. Ok") || !strings.Contains(f.Grid.Text(), "ENERGIZE DOWN") {
		t.Fatalf("transporter content missing:\n%s", f.Grid.Text())
	}
}

// The energize control has hold_ms: a terminal has no key release, so its mnemonic (or Enter) is
// pressed three times within two seconds; the shared focus model counts, the frame activates.
func TestHoldButtonNeedsThreePresses(t *testing.T) {
	p := loadPack(t)
	st := NewState()
	docs := loadDocs(t)
	f := draw(t, p.Screens["transporter"], docs, 120, 40, st, "plain")
	var mn rune
	for _, it := range f.Items {
		if it.Kind == "button" && str(it.Node.Props["text"]) == "ENERGIZE DOWN" {
			mn = it.Mnemonic
		}
	}
	if mn == 0 {
		t.Fatal("no mnemonic for the energize button")
	}
	if r := Dispatch(f, st, "tab", true, now); !st.Active || !r.Used {
		t.Fatalf("Tab did not enter focus mode: %+v", r)
	}
	key := strings.ToLower(string(mn))
	for i := 1; i <= 2; i++ {
		if r := Dispatch(f, st, key, true, now.Add(time.Duration(i)*100*time.Millisecond)); len(r.Events) != 0 || r.Hold == "" {
			t.Fatalf("press %d: %+v", i, r)
		}
	}
	if r := Dispatch(f, st, key, true, now.Add(time.Second)); len(r.Events) != 1 || r.Events[0].Name != "click" {
		t.Fatalf("third press: %+v", r)
	}
	// too slow: the count starts over
	for i := 0; i < 2; i++ {
		Dispatch(f, st, key, true, now.Add(10*time.Second+time.Duration(i)*time.Second))
	}
	if r := Dispatch(f, st, key, true, now.Add(13*time.Second)); len(r.Events) != 0 {
		t.Fatalf("a slow third press fired: %+v", r)
	}
}

// ASCII-only mode: no character outside ASCII reaches the grid.
func TestASCIIMode(t *testing.T) {
	p := loadPack(t)
	st := NewState()
	docs := loadDocs(t)
	env := &pack.Env{Docs: docs, UI: st.UI}
	tree := p.Screens["transporter"].Bind(env, pack.Caps{Class: "cell"})
	f := Draw(tree, 120, 40, Env{Theme: ThemeByName("plain"), Now: now, ASCII: true}, st)
	for _, c := range f.Grid.Cells {
		if c.R >= 0x80 {
			t.Fatalf("non-ASCII %q in ASCII mode", c.R)
		}
	}
}

func overlap(a, b Rect) bool {
	return a.X < b.X+b.W && b.X < a.X+a.W && a.Y < b.Y+b.H && b.Y < a.Y+a.H
}

// Widget-on-widget geometry: at every size no widget overlaps another, the text pane (and its
// border), or the frame's strips; all stay inside the output area; the pane keeps its floor.
func TestNoOverlapAtSizes(t *testing.T) {
	docs := loadDocs(t)
	for _, th := range []string{"tng", "plain"} {
		for _, sz := range [][2]int{{80, 20}, {120, 36}, {200, 46}} {
			for _, id := range pack.BuiltinScreenIDs() {
				scr := pack.Builtin(id)
				st := NewState()
				env := &pack.Env{Docs: docs, UI: st.UI, CanSend: func(string) bool { return true }}
				tree := scr.Bind(env, pack.Caps{Class: "cell"})
				panels, mn, _ := tree.FocusPlan(nil)
				st.SetupModel(Plan{Panels: panels, Mnemonics: mn}, "tab")
				f := Draw(tree, sz[0], sz[1], Env{Theme: ThemeByName(th), Now: now, Builtin: true, Title: scr.Title, Mnemonics: mn}, st)
				name := fmt.Sprintf("%s %dx%d %s", th, sz[0], sz[1], id)
				if f.Compact {
					continue
				}
				top, bottom := ThemeByName(th).margins(sz[1])
				left := ThemeByName(th).leftCols(sz[0])
				for i, a := range f.Boxes {
					if a.X < left || a.Y < top || a.X+a.W > sz[0] || a.Y+a.H > sz[1]-bottom {
						t.Errorf("%s: widget %d %+v outside the content area", name, i, a)
					}
					if overlap(a, f.Text) {
						t.Errorf("%s: widget %d %+v overlaps the text pane %+v", name, i, a, f.Text)
					}
					for j := i + 1; j < len(f.Boxes); j++ {
						if overlap(a, f.Boxes[j]) {
							t.Errorf("%s: widgets %d %+v and %d %+v overlap", name, i, a, j, f.Boxes[j])
						}
					}
				}
				if f.Text.H < 8 && sz[1] >= 20 {
					t.Errorf("%s: text pane %+v under the floor", name, f.Text)
				}
			}
		}
	}
}
