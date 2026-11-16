// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package client

import (
	"strings"
	"testing"
	"time"

	"github.com/itsnotyoutoday/cmcp/go/frame"
	"github.com/itsnotyoutoday/cmcp/go/msg"
	"github.com/itsnotyoutoday/cmcp/go/session"
	"github.com/itsnotyoutoday/gtf/internal/layout"
	"github.com/itsnotyoutoday/gtfparser"
)

// `home` from a manned console: the game pops the contexts and says the ship scope has no owner. Whatever the order of
// those messages, the console screen must go and the text view must come back (and the screen must not linger as a
// stale picture).
func TestCmcpHomeFromConsoleClearsScreen(t *testing.T) {
	for name, fn := range map[string]func(s *session.Server){
		"scope only": func(s *session.Server) { s.SetScope("ship", nil) },
		"pops then scope": func(s *session.Server) {
			s.SendContext(msg.ContextV2{Op: "pop", Key: "console", Ctx: 1})
			s.SetScope("ship", nil)
		},
		"scope then pops": func(s *session.Server) {
			s.SetScope("ship", nil)
			s.SendContext(msg.ContextV2{Op: "pop", Key: "console", Ctx: 1})
		},
		"clear": func(s *session.Server) { s.SendContext(msg.ContextV2{Op: "clear"}); s.SetScope("ship", nil) },
	} {
		f := newScreenFixture(t, 120, 40)
		f.console("std.command", "command", nil)
		f.pushRoom(200, "Bridge")
		f.render()
		f.contain(t, "COMMAND")
		f.d.gm().with(fn)
		cmEventually(t, name+": screen gone", func() bool {
			f.render()
			var surf bool
			f.c.Do(func(*gtfparser.Interp) { surf = f.c.ui.Surf != nil })
			return !surf
		})
		f.lack(t, "COMMAND", "BRIDGE", "ALERT")
	}
}

// Another ship with another theme: the screen takes that ship's theme (the owner's theme, chosen again whenever the
// ship scope's owner changes), not the first ship's.
func TestCmcpSecondShipChangesTheme(t *testing.T) {
	f := newScreenFixture(t, 120, 40)
	theme := func() (th string) {
		f.c.Do(func(*gtfparser.Interp) { th = f.c.cmcpThemeFor(f.c.fg, f.c.fg.cm.UI(), "") })
		return
	}
	f.console("std.command", "command", func(c *msg.Console) { c.Ship.Theme = "tng" })
	if got := theme(); got != "tng" {
		t.Fatalf("first ship: %s", got)
	}
	first := f.surface()
	f.d.gm().with(func(s *session.Server) { s.SendContext(msg.ContextV2{Op: "clear"}); s.SetScope("ship", nil) }) // home
	cmEventually(t, "home", func() bool { f.render(); return f.c.fg.cm.UI().Owner("ship") == nil })
	f.owner = nil
	f.console("std.command", "command", func(c *msg.Console) { c.Ship.ID, c.Ship.Name, c.Ship.Theme = "ss-other", "SS Other", "klingon" })
	if got := theme(); got != "klingon" {
		t.Fatalf("second ship kept the first ship's theme: %s", got)
	}
	if second := f.surface(); second == first {
		t.Fatal("the picture did not change with the ship")
	}
	// and straight from ship to ship, with no home in between
	f.console("std.command", "command", func(c *msg.Console) { c.Ship.ID, c.Ship.Name, c.Ship.Theme = "ss-third", "SS Third", "romulan" })
	if got := theme(); got != "romulan" {
		t.Fatalf("third ship: %s", got)
	}
}

func (f *screenFixture) hit(cid string) {
	b, _ := frame.Encode("Cobra.Event", msg.Event{Type: "hit", Data: map[string]any{"cid": cid, "facing": "port", "strength": 0.9}})
	f.d.gm().write(b)
}

// A hit on our ship (cid self) shows on the screen aboard: in a manned console the frame jitters, in a room with no screen of its
// own the edge flashes; a hit on another contact does nothing.
func TestCmcpHitShowsInConsoleAndRoom(t *testing.T) {
	clock := fixedNow
	nowFn = func() time.Time { return clock }
	defer func() { nowFn = func() time.Time { return fixedNow } }()
	// a console: some cell of the picture outside the text pane differs while the shake lasts
	f := newScreenFixtureSetup(t, 120, 40, t.TempDir(), nil, "/set cmcp_layout=stacked")
	f.console("std.damage", "operations", nil)
	grab := func() (cells []layout.Cell) {
		f.c.Do(func(*gtfparser.Interp) { cells = append([]layout.Cell(nil), f.c.ui.Surf.Grid.Cells...) })
		return
	}
	rest := grab()
	f.hit("some-other-ship")
	time.Sleep(200 * time.Millisecond)
	clock = clock.Add(10 * time.Millisecond)
	f.render()
	for i, c := range grab() {
		if c != rest[i] {
			t.Fatal("a hit on another contact moved the screen")
		}
	}
	f.hit("self")
	moved, flashed := false, false
	cmEventually(t, "console shakes", func() bool {
		clock = clock.Add(10 * time.Millisecond)
		f.render()
		for i, c := range grab() {
			if c != rest[i] {
				moved = true
			}
		}
		flashed = flashed || strings.Contains(strings.SplitN(f.text(), "\n", 2)[0], "HIT") // the top row says so
		return moved && flashed
	})
	// a plain room: a ship room whose layout is not one gtf holds draws no screen; the hit flashes the edge (HIT)
	g := newScreenFixture(t, 120, 40)
	g.console("std.command", "command", nil)
	g.unman()
	g.sendContext(msg.ContextV2{Op: "push", Context: &msg.ContextEntryV2{Key: "room:plain", Ctx: 300, Type: "ship.room", ID: "plain", Name: "Corridor", Layout: "nowhere/none"}})
	cmEventually(t, "plain room", func() bool { return g.c.fg.cm.UI().CtxKey == "room:plain" })
	g.render()
	g.lack(t, "HIT")
	g.hit("someone-else")
	time.Sleep(200 * time.Millisecond)
	g.render()
	g.lack(t, "HIT")
	g.hit("self")
	cmEventually(t, "room flashes", func() bool { g.render(); return strings.Contains(g.text(), "HIT") })
}

func (f *screenFixture) pushShipContext(typ, key, name, layout string, ctx int64) {
	f.sendContext(msg.ContextV2{Op: "push", Context: &msg.ContextEntryV2{Key: key, Ctx: ctx, Type: typ, ID: "x", Name: name, Layout: layout}})
}

func (f *screenFixture) surfaceDrawn() (b bool) {
	f.c.Do(func(*gtfparser.Interp) {
		b = f.c.ui.Surf != nil && f.c.fg.cs != nil && f.c.fg.cs.frame != nil && len(f.c.fg.cs.frame.Items) > 0
	})
	return
}

// A room of the ship (shiproom, room, dock, away) draws NO screen by default: the text stays the view; %cmcp_room_screens=on draws
// the room's screen; a console always draws (the player's rule: no console layout on regular rooms).
func TestCmcpRoomsDrawNoScreenByDefault(t *testing.T) {
	rooms := []struct{ typ, layout, title string }{
		{"ship.shiproom", "std.shiproom", "BRIDGE"}, {"ship.room", "std.shiproom", "CORRIDOR"}, {"ship.dock", "std.shiproom", "DOCKING PORT"}, {"ship.away", "std.away", "AWAY"},
	}
	for _, r := range rooms {
		for _, on := range []bool{false, true} {
			setup := []string{}
			if on {
				setup = append(setup, "/set cmcp_room_screens=on")
			}
			f := newScreenFixtureSetup(t, 120, 40, t.TempDir(), nil, setup...)
			f.console("std.command", "command", nil) // boards: the ship owner
			f.unman()
			f.pushShipContext(r.typ, "room:x", strings.ToTitle(r.title), r.layout, 400)
			cmEventually(t, r.typ+": active", func() bool { return f.c.fg.cm.UI().CtxType == r.typ })
			f.render()
			if drawn := f.surfaceDrawn(); drawn != on {
				t.Errorf("%s with room screens %v: screen drawn = %v\n%s", r.typ, on, drawn, f.term.Dump())
			}
			if !on {
				f.lack(t, "SHIP ROOM", "HULL ")
			}
		}
	}
	// a console always draws, with the setting off
	f := newScreenFixture(t, 120, 40)
	f.console("std.command", "command", nil)
	f.render()
	if !f.surfaceDrawn() {
		t.Fatal("a manned console must draw")
	}
}

// With no screen the alert still shows: a coloured border round the output area for yellow or red, nothing at green, and it
// goes when the ship scope loses its owner.
func TestCmcpRoomAlertGlowsAroundOutput(t *testing.T) {
	for _, c := range []struct {
		alert string
		glow  bool
	}{{"green", false}, {"yellow", true}, {"red", true}} {
		f := newScreenFixture(t, 120, 40)
		f.docs["Space.Ship.Status"].(map[string]any)["alert"] = c.alert
		f.console("std.command", "command", nil)
		f.unman()
		f.pushShipContext("ship.room", "room:plain", "Corridor", "std.shiproom", 401)
		cmEventually(t, "room active", func() bool { return f.c.fg.cm.UI().CtxType == "ship.room" })
		f.render()
		if f.surfaceDrawn() {
			t.Fatalf("%s: a room drew a screen", c.alert)
		}
		var edge bool
		f.c.Do(func(*gtfparser.Interp) {
			if sf := f.c.ui.Surf; sf != nil {
				g := sf.Grid
				edge = g.At(0, 5).R == '▌' && g.At(g.W-1, 5).R == '▐' && g.At(10, 1).R != 0
				// nothing of the border sits inside the text rectangle
				tr := sf.Text
				if edge && (tr.X < 1 || tr.X+tr.W > g.W-1 || tr.Y < 2) {
					t.Errorf("the border overlaps the text area %+v", tr)
				}
			}
		})
		if edge != c.glow {
			t.Fatalf("%s: edge drawn = %v, want %v\n%s", c.alert, edge, c.glow, f.term.Dump())
		}
		if c.glow {
			f.contain(t, strings.ToUpper(c.alert)+" ALERT")
			// leaving the ship clears it
			f.d.gm().with(func(s *session.Server) { s.SendContext(msg.ContextV2{Op: "clear"}); s.SetScope("ship", nil) })
			cmEventually(t, c.alert+": glow gone", func() bool {
				f.render()
				var has bool
				f.c.Do(func(*gtfparser.Interp) { has = f.c.ui.Surf != nil })
				return !has
			})
		}
	}
}
