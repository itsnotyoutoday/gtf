// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package client

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/itsnotyoutoday/cmcp/go/msg"
	"github.com/itsnotyoutoday/cmcp/go/session"
	"github.com/itsnotyoutoday/gtfparser"
)

func concourseFixture(t *testing.T, w, h int) *screenFixture {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "cmcp", "fspace-concourse.pack.json"))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	ref := msg.PackRef{ID: "fspace-concourse", Kind: "layout", Version: "1.0.0", SHA256: hex.EncodeToString(sum[:]), Size: len(raw), Inline: true}
	layoutAnnounce = []msg.PackRef{ref}
	t.Cleanup(func() { layoutAnnounce = nil })
	return newScreenFixtureOpt(t, w, h, t.TempDir(), map[string][]byte{ref.SHA256: raw})
}

func (f *screenFixture) pushConcourse() {
	data, _ := json.Marshal(map[string]any{
		"title":   "Spacedock Concourse",
		"welcome": "Welcome to the concourse. Pick a ship and board it, or look around.",
		"test":    "Board a ship, man a console, leave again: this screen stays.",
		"ships": map[string]any{
			"ss-booty-king": map[string]any{"name": "SS Booty King", "class": "Freighter", "look": "ops"},
			"uss-kestrel":   map[string]any{"name": "USS Kestrel", "class": "Scout", "look": "tng"},
		}})
	f.sendContext(msg.ContextV2{Op: "push", Reason: "entered", Context: &msg.ContextEntryV2{
		Key: "place:concourse", Ctx: 900, Type: "world.place", ID: "concourse", Name: "Spacedock Concourse", Data: data,
		Commands: []string{"world.home", "world.map", "world.guide", "world.about", "world.view", "ship.board"}}})
}

func (f *screenFixture) layoutNow() (id string, fb bool, ship bool) {
	f.c.Do(func(*gtfparser.Interp) {
		u := f.c.fg.cm.UI()
		id, fb, ship = u.Screen.Resolved, u.Screen.Fallback, u.HasOwner("ship")
	})
	return
}

// Spec 20.7, 20.12 item 7: the concourse is a world.place context. It draws its pack layout with no
// ship (a frame: the place's title, the layout's widgets, the textport), has no alert segment or ship
// stations, keeps the screen while a ship is boarded and left, and its buttons send real verbs that
// are echoed.
func TestCmcpConcourseNoShipFrame(t *testing.T) {
	for _, sz := range [][2]int{{80, 24}, {120, 40}} {
		f := concourseFixture(t, sz[0], sz[1])
		f.pushConcourse()
		cmEventually(t, "concourse layout", func() bool { id, fb, _ := f.layoutNow(); return id == "fspace-concourse/concourse" && !fb })
		f.render()
		if _, _, ship := f.layoutNow(); ship {
			t.Fatal("the test must run with no ship")
		}
		f.contain(t, "SPACEDOCK CONCOURSE", "SHIPS", "MAP", "GUIDE", "HOME", "ABOUT")
		if sz[1] >= 40 {
			f.contain(t, "SS Booty King", "USS Kestrel")
		}
		f.lack(t, "ALERT", "ENGINEER", "SENSORS", "SS BOOTY KING")
		var r struct{ W, H int }
		f.c.Do(func(*gtfparser.Interp) { m := f.c.ui.MainRect(); r.W, r.H = m.W, m.H })
		if r.H < 4 || r.W < 30 {
			t.Fatalf("%dx%d: the textport has %+v", sz[0], sz[1], r)
		}
	}
}

func TestCmcpConcourseKeyboardAndEcho(t *testing.T) {
	f := concourseFixture(t, 120, 40)
	f.pushConcourse()
	cmEventually(t, "concourse layout", func() bool { id, fb, _ := f.layoutNow(); return id == "fspace-concourse/concourse" && !fb })
	f.render()
	f.keys(kTab) // enter the screen
	f.keys("h")  // HOME's letter
	cmEventually(t, "home sent", func() bool { return len(f.game.commands()) == 1 })
	if got := f.game.commands()[0]; !strings.HasPrefix(got, "world.home") {
		t.Fatalf("ran %q", got)
	}
	cmEventually(t, "typed form echoed", func() bool { f.render(); return strings.Contains(f.text(), "cmcp> home") })
	f.keys("a")
	cmEventually(t, "about sent", func() bool { return len(f.game.commands()) == 2 })
	if got := f.game.commands()[1]; !strings.HasPrefix(got, "world.about") {
		t.Fatalf("ran %q", got)
	}
	// BOARD appears once a ship is selected in the table; it sends ship.board with the row's key
	f.lack(t, "BOARD ◗")
	f.keys("\x1b", kTab, kTab) // Esc, then Tab into the menu and on to the table
	f.keys(kDown, kEnter)
	f.render()
	f.contain(t, "BOARD ◗")
	f.keys("b") // BOARD's letter
	cmEventually(t, "board sent", func() bool { return len(f.game.commands()) == 3 })
	if got := f.game.commands()[2]; !strings.HasPrefix(got, "ship.board") || !strings.Contains(got, `"ship":"`) {
		t.Fatalf("ran %q", got)
	}
}

// The place survives boarding and leaving: a ship's console wins while aboard, the place shows again when the
// console is unmanned and when the ship scope loses its owner; the world scope is never touched (spec 20.4.1 rule 2).
func TestCmcpConcourseSurvivesBoardingAndLeaving(t *testing.T) {
	f := concourseFixture(t, 120, 40)
	f.pushConcourse()
	cmEventually(t, "concourse layout", func() bool { id, fb, _ := f.layoutNow(); return id == "fspace-concourse/concourse" && !fb })
	f.console("std.operations", "", nil) // boards: Cobra.Scope(ship, owner) + a console context
	if id, _, ship := f.layoutNow(); id != "std.operations" || !ship {
		t.Fatalf("aboard: %q ship=%v", id, ship)
	}
	f.render()
	f.contain(t, "OPERATIONS", "GREEN ALERT")
	f.unman()
	cmEventually(t, "place again", func() bool { id, fb, _ := f.layoutNow(); return id == "fspace-concourse/concourse" && !fb })
	f.d.gm().with(func(s *session.Server) { s.SetScope("ship", nil) }) // left every ship: owner null
	cmEventually(t, "ship scope empty", func() bool { _, _, ship := f.layoutNow(); return !ship })
	f.render()
	f.contain(t, "SPACEDOCK CONCOURSE", "SS Booty King")
	f.lack(t, "ALERT")
	exec(f.c, "/cmcp status")
	f.render()
	f.contain(t, "scope ship: nobody", "scope world:", "context * place:concourse world.place")
	time.Sleep(10 * time.Millisecond)
}

func worldOnlyDoc(format int) map[string]any {
	return map[string]any{
		"format": format, "id": "town", "version": "1.0.0", "title": "Town",
		"scopes":        map[string]any{"world": map[string]any{"owner": map[string]any{"fixed": "town"}, "title": "in town"}},
		"packages":      map[string]any{},
		"context_types": map[string]any{"world.place": map[string]any{"scope": "world", "priority": 20, "layout": "std.frame", "ambient": false}},
	}
}

// Spec 20.12 item 8: a game with no ship scope at all works end to end: the place is a frame with the
// place's title and the textport, a verb runs, and /cmcp status shows only the world scope.
func TestCmcpGameWithoutShipScope(t *testing.T) {
	defOverride = servedDef(worldOnlyDoc(1), false)
	t.Cleanup(func() { defOverride = nil })
	f := newScreenFixture(t, 120, 40)
	f.sendContext(msg.ContextV2{Op: "push", Reason: "entered", Context: &msg.ContextEntryV2{
		Key: "place:square", Ctx: 1, Type: "world.place", ID: "square", Name: "Town Square", Commands: []string{"hatch.toggle"}}})
	cmEventually(t, "frame", func() bool { id, _, _ := f.layoutNow(); return id == "std.frame" })
	f.render()
	f.contain(t, "TOWN SQUARE")
	f.lack(t, "ALERT", "ENGINEER")
	exec(f.c, "/cmcp send hatch.toggle")
	cmEventually(t, "verb ran", func() bool { return len(f.game.commands()) == 1 })
	exec(f.c, "/cmcp status")
	f.render()
	f.contain(t, "definition: town 1.0.0", "scope world:", "context * place:square world.place")
	f.lack(t, "scope ship")
}

// A definition that fails its hash check: the client stays a text client with one visible line, the connection
// and the game's text are untouched (spec 20.3.4); a format newer than the client knows says so.
func TestCmcpDefinitionRefusedKeepsTextClient(t *testing.T) {
	for _, c := range []struct {
		name string
		def  *session.ServedDefinition
		want string
	}{
		{"hash", servedDef(worldOnlyDoc(1), true), "failed its hash check"},
		{"format", servedDef(worldOnlyDoc(99), false), "needs a newer client"},
	} {
		defOverride, skipDefsWait = c.def, true
		f := newScreenFixture(t, 100, 30)
		defOverride, skipDefsWait = nil, false
		cmEventually(t, c.name+": notice", func() bool { f.render(); return strings.Contains(f.text(), c.want) })
		if n := strings.Count(f.text(), "text mode"); n != 1 {
			t.Fatalf("%s: notice shown %d times", c.name, n)
		}
		exec(f.c, "/cmcp status")
		f.render()
		f.contain(t, "text client")
		if c.name == "format" && !f.c.fg.open() {
			t.Fatal("the connection must stay open")
		}
	}
}

// The definition is fetched once and cached under its own kind; the next launch lists it in Hello.defs and the
// server streams without a transfer.
func TestCmcpDefinitionCachedAcrossLaunches(t *testing.T) {
	home := t.TempDir()
	f := newScreenFixtureOpt(t, 100, 30, home, nil)
	if f.d.gm().received("Cobra.Defs.Get") != 1 || f.d.gm().received("Cobra.Defs.Have") != 1 {
		t.Fatalf("first launch: Get %d Have %d", f.d.gm().received("Cobra.Defs.Get"), f.d.gm().received("Cobra.Defs.Have"))
	}
	ps := f.c.packStore()
	if d := ps.cache.RecentDefs("localhost", 16); len(d) != 1 || d[0].ID != "fspace-test" {
		t.Fatalf("cached definitions: %+v", d)
	}
	f.c.Close()
	f2 := newScreenFixtureOpt(t, 100, 30, home, nil)
	if f2.d.gm().received("Cobra.Defs.Get") != 0 {
		t.Fatal("the cached definition was requested again")
	}
	if h := f2.d.gm().srv.Hello(); h == nil || len(h.Defs) != 1 || h.Defs[0].ID != "fspace-test" {
		t.Fatalf("Hello.defs: %+v", h)
	}
	f2.console("std.operations", "", nil) // and the stream works
}

func (f *screenFixture) pushBriefing() {
	data, _ := json.Marshal(map[string]any{"title": "Mission briefing", "status": "Dock 4 is open.", "hint": "Nothing to do here."})
	f.sendContext(msg.ContextV2{Op: "push", Reason: "entered", Context: &msg.ContextEntryV2{
		Key: "panel:briefing", Ctx: 901, Type: "world.briefing", ID: "briefing", Name: "Briefing", Title: "Briefing", Data: data,
		Commands: []string{"world.about"}}})
}

// Spec 20.14.1a/8: with surfaces [screen, panel] a context presented off the main screen never wins it (whatever its
// priority), is listed in the screen menu, and the menu shows its layout as the main screen locally, with the normal text
// output kept; its buttons send the verbs it lists.
func TestCmcpPaneContextInScreenMenu(t *testing.T) {
	f := concourseFixture(t, 120, 40)
	f.pushConcourse()
	f.pushBriefing()
	cmEventually(t, "concourse layout", func() bool { id, fb, _ := f.layoutNow(); return id == "fspace-concourse/concourse" && !fb })
	f.render()
	f.contain(t, "SPACEDOCK CONCOURSE", "BRIEFING") // the menu lists the pane
	f.lack(t, "Mission briefing")                   // and the main screen is the place
	var n int
	f.c.Do(func(*gtfparser.Interp) { n = len(cmcpPanes(f.c.fg.cm.UI())) })
	if n != 1 {
		t.Fatalf("panes: %d", n)
	}
	// pick it from the menu: Tab into the screen, then its number (the menu lists the layouts first, the pane last)
	f.keys(kTab, "3")
	cmEventually(t, "pane shown", func() bool { f.render(); return strings.Contains(f.text(), "Mission briefing") })
	f.contain(t, "BRIEFING", "Dock 4 is open.", "GAME OUTPUT") // title strip, its data, and the normal text output
	// the server's active context did not change
	if id, _, _ := f.layoutNow(); id != "fspace-concourse/concourse" {
		t.Fatalf("the main screen moved to %q", id)
	}
	// a verb the pane lists can be sent from it
	f.keys("a")
	cmEventually(t, "about sent", func() bool { return len(f.game.commands()) >= 1 })
	// the first menu line goes back to the place
	f.keys("\x1b", kTab, "1")
	cmEventually(t, "place again", func() bool {
		f.render()
		return !strings.Contains(f.text(), "Mission briefing") && strings.Contains(f.text(), "SS Booty King")
	})
}

// A type that lists `panel` first is a real pane for gtf (surface panel, in UI.Panes): same menu entry, same rule.
func TestCmcpPanelSurfacePaneInMenu(t *testing.T) {
	briefingPresentation = []string{"panel", "screen"}
	t.Cleanup(func() { briefingPresentation = []string{"dock", "float", "screen"} })
	f := concourseFixture(t, 120, 40)
	f.pushConcourse()
	f.pushBriefing()
	cmEventually(t, "concourse layout", func() bool { id, fb, _ := f.layoutNow(); return id == "fspace-concourse/concourse" && !fb })
	var surf string
	f.c.Do(func(*gtfparser.Interp) {
		for _, p := range f.c.fg.cm.UI().Panes["panel"] {
			surf = p.Surface
		}
	})
	if surf != "panel" {
		t.Fatalf("the briefing is not a panel pane (%q)", surf)
	}
	f.render()
	f.contain(t, "BRIEFING")
	f.lack(t, "Mission briefing")
	exec(f.c, "/cmcp status")
	f.render()
	f.contain(t, "(pane)")
}

// Spec 20.14.9: a context whose level is 1 (text only) draws no screen, whatever its layout; the game's text is the view.
func TestCmcpLevelOneIsTextOnly(t *testing.T) {
	f := concourseFixture(t, 120, 40)
	data, _ := json.Marshal(map[string]any{"title": "Spacedock Concourse"})
	f.sendContext(msg.ContextV2{Op: "push", Context: &msg.ContextEntryV2{Key: "place:concourse", Ctx: 7, Type: "world.place", ID: "concourse", Name: "Concourse", Data: data, Level: 1}})
	cmEventually(t, "context active", func() bool { id, _, _ := f.layoutNow(); return id == "fspace-concourse/concourse" })
	f.render()
	f.lack(t, "SHIPS", "SPACEDOCK CONCOURSE", "GAME OUTPUT")
	// the same context at level 3 draws
	f.sendContext(msg.ContextV2{Op: "push", Context: &msg.ContextEntryV2{Key: "place:concourse", Ctx: 8, Type: "world.place", ID: "concourse", Name: "Concourse", Data: data, Level: 3}})
	cmEventually(t, "screen back", func() bool { f.render(); return strings.Contains(f.text(), "SHIPS") })
}
