// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package client

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/itsnotyoutoday/cmcp/go/msg"
	"github.com/itsnotyoutoday/cmcp/go/session"
)

// federationFixture serves the real ship layout pack (fspace-federation) the way the testgame does: announced in
// Welcome.packs and as the ship owner's pack, never in a v1 Console ship block.
func federationFixture(t *testing.T, w, h int) (*screenFixture, msg.PackRef) {
	t.Helper()
	return federationFixtureOpt(t, w, h, true)
}

// federationFixtureOpt: inWelcome false announces the pack only in the Scope owner block (spec 20.4.1 owner.packs).
func federationFixtureOpt(t *testing.T, w, h int, inWelcome bool) (*screenFixture, msg.PackRef) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "cmcp", "fspace-federation.pack.json"))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	ref := msg.PackRef{ID: "fspace-federation", Kind: "layout", Version: "1.0.0", SHA256: hex.EncodeToString(sum[:]), Size: len(raw)}
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/blob/"+ref.SHA256) {
			rw.Write(raw)
			return
		}
		http.NotFound(rw, r)
	}))
	t.Cleanup(srv.Close)
	ref.URL = srv.URL + "/cmcp/v1/blob/" + ref.SHA256
	if inWelcome {
		layoutAnnounce = []msg.PackRef{ref}
	}
	welcomeAssets = &msg.Assets{Origin: srv.URL, Base: "/cmcp/v1"}
	t.Cleanup(func() { layoutAnnounce, welcomeAssets = nil, nil })
	// the pack is too big for inline transfer: it comes over loopback http from the local game (no developer setting needed)
	return newScreenFixtureSetup(t, w, h, t.TempDir(), nil), ref
}

// Every console of the ship draws its pack screen with its widgets (the v1 look), in a v2 session where the pack is
// announced in Welcome.packs and in the Scope owner block only.
func TestCmcpConsolesDrawFromShipPack(t *testing.T) {
	cases := []struct {
		console, screen string
		want            []string
	}{
		{"navigation", "navigation", []string{"FORWARD VIEWSCREEN", "HELM CONTROLS", "[ STOP ]"}},
		{"operations", "operations", []string{"OPERATIONS  -", "CONTACTS", "POWER", "VESSEL"}},
		{"tactical", "tactical", []string{"TACTICAL  -", "Weapons", "Shield facings"}},
		{"science", "science", []string{"SCIENCE  -", "CID NAME"}},
		{"engineering", "engineering", []string{"POWER  6.4 GW total", "DAMAGE CONTROL", "BALANCED"}},
	}
	for _, c := range cases {
		f, ref := federationFixture(t, 140, 44)
		f.console("fspace-federation/"+c.screen, c.console, func(m *msg.Console) {
			m.Ship.Pack = &msg.PackIdent{ID: ref.ID, Version: ref.Version, SHA256: ref.SHA256}
			m.Ship.Packs = []msg.PackRef{ref}
		})
		cmEventually(t, c.console+": pack in use", func() bool { id, fb := f.screenState(); return id == "fspace-federation/"+c.screen && !fb })
		f.render()
		if testing.Verbose() {
			t.Logf("== %s\n%s", c.console, f.term.Dump())
		}
		for _, w := range c.want {
			if !strings.Contains(f.text(), w) {
				t.Errorf("%s: the screen lacks %q", c.console, w)
			}
		}
	}
}

// The ship's pack announced ONLY in the Scope owner block (pack + packs, as the testgame sends it with the owner), with
// Welcome.packs not listing it: gtf still finds, fetches and uses it for the console.
func TestCmcpShipPackOnlyInScopeOwner(t *testing.T) {
	f, ref := federationFixtureOpt(t, 140, 44, false)
	f.console("fspace-federation/navigation", "navigation", func(m *msg.Console) {
		m.Ship.Pack = &msg.PackIdent{ID: ref.ID, Version: ref.Version, SHA256: ref.SHA256}
		m.Ship.Packs = []msg.PackRef{ref}
	})
	cmEventually(t, "pack in use", func() bool { id, fb := f.screenState(); return id == "fspace-federation/navigation" && !fb })
	f.render()
	f.contain(t, "FORWARD VIEWSCREEN", "HELM CONTROLS")
}

// The resume path: the player already mans a console when the v2 session starts, so the server sends the ship owner
// and then one `set` of the whole stack (console and room). The console must draw from the ship pack.
func TestCmcpConsoleDrawsOnResumeSet(t *testing.T) {
	f, ref := federationFixture(t, 140, 44)
	owner := msg.ScopeOwner{ID: "ss-booty-king", Epoch: 1, Name: "SS Booty King", Theme: "tng", Pack: &msg.PackIdent{ID: ref.ID, Version: ref.Version, SHA256: ref.SHA256}, Packs: []msg.PackRef{ref}}
	var subs []msg.SubSpec
	for _, pkg := range sortedDocKeys(f.docs) {
		subs = append(subs, msg.SubSpec{Pkg: pkg})
	}
	f.d.gm().with(func(s *session.Server) {
		s.SetScope("ship", &owner)
		s.SendContext(msg.ContextV2{Op: "set", Reason: "resume", Stack: []msg.ContextEntryV2{
			{Key: "room:shiproom", Ctx: 1, Type: "ship.room", ID: "shiproom", Name: "Bridge"},
			{Key: "console", Ctx: 2, Type: "ship.console", ID: "navigation", Layout: "fspace-federation/navigation", Subscribe: subs},
		}})
		for _, sub := range subs {
			s.PushScoped("ship", sub.Pkg, true, f.docs[sub.Pkg])
		}
	})
	cmEventually(t, "pack in use", func() bool { id, fb := f.screenState(); return id == "fspace-federation/navigation" && !fb })
	f.render()
	f.contain(t, "NAVIGATION", "FORWARD VIEWSCREEN", "HELM CONTROLS")
}

// consoleScreens are the ship's consoles as the testgame sends them: console id -> pack screen.
var consoleScreens2 = []struct{ console, screen string }{
	{"navigation", "navigation"}, {"operations", "operations"}, {"tactical", "tactical"}, {"science", "science"},
	{"engineering", "engineering"}, {"damage", "damage"}, {"command", "command"}, {"communications", "communications"},
	{"medical", "medical"}, {"security", "security"},
}

// packTexts lists the literal texts of a pack screen's labels and buttons (what a console must draw whatever the data).
func packTexts(t *testing.T, screen string) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "cmcp", "fspace-federation.pack.json"))
	if err != nil {
		t.Fatal(err)
	}
	var d struct {
		Screens map[string]struct {
			Root json.RawMessage `json:"root"`
		} `json:"screens"`
	}
	if err := json.Unmarshal(raw, &d); err != nil {
		t.Fatal(err)
	}
	var out []string
	var walk func(n map[string]any)
	walk = func(n map[string]any) {
		w, _ := n["widget"].(string)
		pr, _ := n["props"].(map[string]any)
		bind, _ := n["bind"].(map[string]any)
		if n["show_if"] != nil {
			return // drawn only when the data says so (a tube, a door, a team that exists)
		}
		if txt, _ := pr["text"].(string); txt != "" && bind["text"] == nil && (w == "button" || w == "label") {
			out = append(out, txt)
		}
		if kids, ok := n["children"].([]any); ok {
			for _, k := range kids {
				if km, ok := k.(map[string]any); ok {
					walk(km)
				}
			}
		}
	}
	var root map[string]any
	json.Unmarshal(d.Screens[screen].Root, &root)
	if root == nil {
		t.Fatalf("the pack has no screen %q", screen)
	}
	walk(root)
	return out
}

// Every console draws through a full v2 session with the real ship pack and the testgame's message shapes: the owner
// announced with its pack, the console context with `level` 3, `ambient` true and ten allowed `layouts`. Each screen must show
// every label and button its pack screen defines (a clipped or dropped widget row is the bug this guards against), its
// pictures, the console frame (title strip, status strip) and the text pane.
func TestCmcpEveryConsoleDrawsLikeV1(t *testing.T) {
	for _, c := range consoleScreens2 {
		f, ref := federationFixture(t, 170, 80)
		owner := msg.ScopeOwner{ID: "acs-pathfinder", Epoch: 1, Name: "ACS Pathfinder", Theme: "tng", Pack: &msg.PackIdent{ID: ref.ID, Version: ref.Version, SHA256: ref.SHA256}, Packs: []msg.PackRef{ref}}
		var subs []msg.SubSpec
		for _, pkg := range sortedDocKeys(f.docs) {
			subs = append(subs, msg.SubSpec{Pkg: pkg})
		}
		var layouts []string
		layouts = append(layouts, "fspace-federation/"+c.screen)
		for _, o := range consoleScreens2 {
			if o.screen != c.screen {
				layouts = append(layouts, "fspace-federation/"+o.screen)
			}
		}
		level, amb := 3, true
		f.d.gm().with(func(s *session.Server) {
			s.SetScope("ship", &owner)
			s.SendContext(msg.ContextV2{Op: "push", Reason: "manned", Context: &msg.ContextEntryV2{Key: "console", Ctx: 5, Type: "ship.console", ID: c.console,
				Name: c.console, Layout: "fspace-federation/" + c.screen, Layouts: layouts, Level: level, Ambient: &amb, Subscribe: subs}})
			for _, sub := range subs {
				s.PushScoped("ship", sub.Pkg, true, f.docs[sub.Pkg])
			}
		})
		cmEventually(t, c.console+": pack screen in use", func() bool { id, fb := f.screenState(); return id == "fspace-federation/"+c.screen && !fb })
		f.render()
		txt := f.text()
		for _, w := range packTexts(t, c.screen) {
			if !strings.Contains(txt, w) && !strings.Contains(strings.ToUpper(txt), strings.ToUpper(w)) {
				t.Errorf("%s: the screen lacks %q", c.console, w)
			}
		}
		for _, w := range []string{"GAME OUTPUT", "ACS PATHFINDER", "GREEN ALERT", "HULL "} { // text pane, ship in the title strip, alert, status strip
			if !strings.Contains(strings.ToUpper(txt), w) {
				t.Errorf("%s: the frame lacks %q", c.console, w)
			}
		}
		if testing.Verbose() {
			t.Logf("== %s\n%s", c.console, f.term.Dump())
		}
	}
}
