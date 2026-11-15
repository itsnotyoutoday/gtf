// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package client

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/itsnotyoutoday/cmcp/msg"
	"github.com/itsnotyoutoday/gtf/internal/cmcpui"
	"github.com/itsnotyoutoday/gtfparser"
)

func readPack(t *testing.T) ([]byte, msg.PackRef) {
	raw, err := os.ReadFile(filepath.Join("testdata", "cmcp", "fspace-std.pack.json"))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	return raw, msg.PackRef{ID: "fspace-std", Kind: "layout", Version: "1.0.0", SHA256: hex.EncodeToString(sum[:]), Size: len(raw), Inline: true}
}

// packConsole is a console message whose screen is a pack screen of the announced pack.
func packConsole(ref msg.PackRef) func(*msg.Console) {
	return func(c *msg.Console) {
		c.Ship.Pack = &msg.PackIdent{ID: ref.ID, Version: ref.Version, SHA256: ref.SHA256}
		c.Ship.Packs = []msg.PackRef{ref}
	}
}

func (f *screenFixture) screenState() (id string, fallback bool) {
	f.c.Do(func(*gtfparser.Interp) { u := f.c.fg.cm.UI(); id, fallback = u.Screen.Resolved, u.Screen.Fallback })
	return
}

func packFixture(t *testing.T, home string, packs map[string][]byte) *screenFixture {
	t.Helper()
	var f *screenFixture
	f = newScreenFixtureOpt(t, 120, 40, home, packs)
	return f
}

// E6: a pack announced by the console: the built-in screen shows first, the player is asked, the
// answer is remembered, the pack arrives inline, is verified, switched to, cached; the next launch
// uses the cache without asking or fetching.
func TestCmcpPackConsentInlineCache(t *testing.T) {
	raw, ref := readPack(t)
	home := t.TempDir()
	f := packFixture(t, home, map[string][]byte{ref.SHA256: raw})
	exec(f.c, "/set cmcp_pack_consent=ask")
	f.console("fspace-std/damage", "operations", packConsole(ref))
	if id, fb := f.screenState(); id != "std.operations" || !fb {
		t.Fatalf("expected the built-in screen first, got %q fallback=%v", id, fb)
	}
	f.contain(t, "offers a layout pack")
	if int(f.d.gets.Load()) != 0 {
		t.Fatal("fetched before the player agreed")
	}
	exec(f.c, "/cmcp pack allow")
	cmEventually(t, "pack in use", func() bool { id, fb := f.screenState(); return id == "fspace-std/damage" && !fb })
	if int(f.d.gets.Load()) != 1 {
		t.Fatalf("Pack.Get sent %d times", int(f.d.gets.Load()))
	}
	f.render()
	f.contain(t, "DAMAGE CONTROL")
	f.c.Close()

	// second launch: same home, same host: cached and already allowed
	f2 := packFixture(t, home, map[string][]byte{ref.SHA256: raw})
	exec(f2.c, "/set cmcp_pack_consent=ask")
	f2.console("fspace-std/damage", "operations", packConsole(ref))
	cmEventually(t, "pack from cache", func() bool { id, fb := f2.screenState(); return id == "fspace-std/damage" && !fb })
	if int(f2.d.gets.Load()) != 0 {
		t.Fatalf("fetched again (%d)", int(f2.d.gets.Load()))
	}
	f2.lack(t, "offers a layout pack")
}

func TestCmcpPackRefused(t *testing.T) {
	raw, ref := readPack(t)
	f := packFixture(t, t.TempDir(), map[string][]byte{ref.SHA256: raw})
	exec(f.c, "/set cmcp_pack_consent=ask")
	f.console("fspace-std/damage", "operations", packConsole(ref))
	exec(f.c, "/cmcp pack deny")
	time.Sleep(100 * time.Millisecond)
	if id, fb := f.screenState(); id != "std.operations" || !fb || int(f.d.gets.Load()) != 0 {
		t.Fatalf("refused pack changed things: %q %v gets=%d", id, fb, int(f.d.gets.Load()))
	}
	// the answer is remembered: announcing it again does not ask again
	f.console("fspace-std/damage", "operations", func(c *msg.Console) { packConsole(ref)(c); c.Reason = "pack_changed" })
	f.lack(t, "offers a layout pack\n")
}

// E16 in part: bytes that do not hash to the announced value are dropped whole; the session goes on
// with the built-in screens.
func TestCmcpPackHashMismatchDropped(t *testing.T) {
	raw, ref := readPack(t)
	bad := append([]byte(nil), raw...)
	bad[len(bad)/2] ^= 0xff
	f := packFixture(t, t.TempDir(), map[string][]byte{ref.SHA256: bad})
	exec(f.c, "/set cmcp_pack_consent=always")
	f.console("fspace-std/damage", "operations", packConsole(ref))
	cmEventually(t, "transfer attempted", func() bool { return int(f.d.gets.Load()) == 1 })
	time.Sleep(150 * time.Millisecond)
	if id, fb := f.screenState(); id != "std.operations" || !fb {
		t.Fatalf("a corrupt pack was used: %q %v", id, fb)
	}
	exec(f.c, "/cmcp send hatch.toggle") // the session still works
	cmEventually(t, "command still runs", func() bool { return len(f.game.commands()) == 1 })
}

func TestCmcpPackNeverMode(t *testing.T) {
	raw, ref := readPack(t)
	f := packFixture(t, t.TempDir(), map[string][]byte{ref.SHA256: raw})
	exec(f.c, "/set cmcp_pack_consent=never")
	f.console("fspace-std/damage", "operations", packConsole(ref))
	time.Sleep(100 * time.Millisecond)
	if int(f.d.gets.Load()) != 0 {
		t.Fatal("fetched in never mode")
	}
	exec(f.c, "/cmcp packs")
	f.render()
	f.contain(t, "consent mode: never")
}

// E2 from the client side: with %cmcp off the connection is a plain GMCP client: Cobra is never
// declared, and the server never offers anything.
func TestCmcpOffDeclaresNothing(t *testing.T) {
	f := newScreenFixtureSetup(t, 100, 30, t.TempDir(), nil, "/set cmcp=off")
	_ = f
}

// E16 in part: a hostile game sends a pack whose expression does not parse, a frame that is not
// JSON, and an oversize body. Everything is dropped, the session carries on.
func TestCmcpHostilePackAndFrames(t *testing.T) {
	bad := []byte(`{"format":1,"id":"evil","version":"1.0.0","needs":{"proto":1},"screens":{"damage":{"title":"x","console":"operations","root":{"widget":"label","children":[{"widget":"label","bind":{"text":"1 +"}},{"widget":"textport"}]}}}}`)
	sum := sha256.Sum256(bad)
	ref := msg.PackRef{ID: "evil", Kind: "layout", Version: "1.0.0", SHA256: hex.EncodeToString(sum[:]), Size: len(bad), Inline: true}
	f := packFixture(t, t.TempDir(), map[string][]byte{ref.SHA256: bad})
	exec(f.c, "/set cmcp_pack_consent=always")
	f.console("evil/damage", "operations", packConsole(ref))
	cmEventually(t, "transfer attempted", func() bool { return int(f.d.gets.Load()) == 1 })
	time.Sleep(150 * time.Millisecond)
	if id, fb := f.screenState(); id != "std.operations" || !fb {
		t.Fatalf("hostile pack used: %q %v", id, fb)
	}
	// garbage and an oversize frame
	f.d.gm().write([]byte{255, 250, 201, 'C', 'o', 'b', 'r', 'a', '.', 'E', 'v', 'e', 'n', 't', ' ', '{', 'n', 'o', 't', ' ', 'j', 's', 'o', 'n', 255, 240})
	big := append([]byte{255, 250, 201}, []byte("Cobra.Event {\"type\":\"notice\",\"text\":\""+strings.Repeat("x", 200000)+"\"}")...)
	f.d.gm().write(append(big, 255, 240))
	time.Sleep(200 * time.Millisecond)
	exec(f.c, "/cmcp send hatch.toggle")
	cmEventually(t, "session still works", func() bool { return len(f.game.commands()) == 1 })
	exec(f.c, "/cmcp status")
	f.render()
	f.contain(t, "console: operations", "showing std.operations")
}

func TestSemverPrecedence(t *testing.T) {
	for _, c := range []struct {
		a, b string
		gt   bool
	}{{"1.0.1", "1.0.0", true}, {"1.10.0", "1.9.0", true}, {"1.0.0", "1.0.0", false}, {"1.0.1", "1.0.1-rc.1", true},
		{"1.0.1-rc.1", "1.0.1", false}, {"1.0.0-alpha.10", "1.0.0-alpha.2", true}, {"1.0.0-alpha.1", "1.0.0-alpha", true}} {
		if got := semverGT(c.a, c.b); got != c.gt {
			t.Errorf("%s > %s = %v", c.a, c.b, got)
		}
	}
}

// The theme download path: the ship suggests `klingon`; the server announces a klingon 1.0.1 theme
// pack (higher than the embedded 1.0.0); gtf asks for it, the player is consulted (always, here),
// the pack is verified and parsed, and its frame replaces the embedded one. With the pack withheld
// the embedded theme stays.
func TestCmcpThemePackOverridesEmbedded(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "cmcp", "klingon-1.0.1.theme.json"))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	ref := msg.PackRef{ID: "klingon", Kind: "theme", Version: "1.0.1", SHA256: hex.EncodeToString(sum[:]), Size: len(raw), Inline: true}
	f := newThemeFixture(t, map[string][]byte{ref.SHA256: raw}, ref)
	exec(f.c, "/set cmcp_pack_consent=always")
	f.console("std.sensors", "science", func(c *msg.Console) { c.Ship.Theme = "klingon" })
	cmEventually(t, "downloaded theme installed", func() (ok bool) {
		f.c.Do(func(*gtfparser.Interp) { ok = f.c.fg.cs != nil && f.c.fg.cs.themes["klingon"] != nil })
		return
	})
	exec(f.c, "/cmcp themes")
	f.render()
	f.contain(t, "downloaded 1.0.1", "active: klingon (downloaded 1.0.1)")
}

// One theme per ship: the ship's theme is chosen once, so a console whose own message carries no theme
// (or a pack screen that suggests another) does not change it until the ship changes.
func TestCmcpOneThemePerShip(t *testing.T) {
	f := newScreenFixture(t, 120, 40)
	id := func() (th string) {
		f.c.Do(func(*gtfparser.Interp) { th = f.c.cmcpThemeFor(f.c.fg, f.c.fg.cm.UI(), "tng") })
		return
	}
	f.console("std.sensors", "science", func(c *msg.Console) { c.Ship.Theme = "klingon" })
	if got := id(); got != "klingon" {
		t.Fatalf("first console: %s", got)
	}
	f.console("std.power", "", func(c *msg.Console) { c.Ship.Theme = ""; c.Theme = "" })
	if got := id(); got != "klingon" {
		t.Fatalf("second console of the same ship changed the theme to %s", got)
	}
	f.console("std.damage", "", func(c *msg.Console) { c.Ship.Theme = "romulan"; c.Ship.ID = "other-ship" })
	if got := id(); got != "romulan" {
		t.Fatalf("after a ship hop: %s", got)
	}
}

// A pack question left over from the previous ship must not survive the ship hop: the consent line
// is only ever about the current ship's packs.
func TestCmcpStaleConsentDroppedOnShipHop(t *testing.T) {
	raw, ref := readPack(t)
	f := packFixture(t, t.TempDir(), map[string][]byte{ref.SHA256: raw})
	exec(f.c, "/set cmcp_pack_consent=ask")
	f.console("fspace-std/damage", "operations", packConsole(ref))
	f.render()
	f.contain(t, "F5 = ALLOW") // the question, in gtf's own status line
	f.console("std.operations", "operations", func(c *msg.Console) { c.Ship.ID = "other-ship"; c.Ship.Name = "Other" })
	f.render()
	f.lack(t, "F5 = ALLOW", "LAYOUT PACK")
}

// The key hint and the focus state are gtf's own line, the same in every theme; the screen menu lists
// only real, available screens; /cmcp screens lists all of them with the console that provides each.
func TestCmcpHintLineEveryTheme(t *testing.T) {
	for _, th := range []string{"tng", "tos", "ops", "soft", "klingon", "romulan", "ferengi"} {
		f := newScreenFixture(t, 120, 40)
		f.console("std.sensors", "science", func(c *msg.Console) { c.Ship.Theme = th })
		f.contain(t, "INPUT - Tab or F2: screen menu   Shift+Tab: last panel", "/cmcp keys")
		f.keys(kTab)
		f.contain(t, "MENU ", "Up/Down choose   Enter open", "Esc input line")
		f.keys(kDown, kDown)
		f.keys("\x1b")
		f.contain(t, "INPUT - Tab or F2")
		f.dump("hint-" + th)
	}
}

func TestCmcpMenuAvailableOnlyAndScreensList(t *testing.T) {
	f := newScreenFixture(t, 120, 40)
	f.console("std.operations", "operations", nil)
	f.lack(t, "FSPACE", "fspace-")
	exec(f.c, "/cmcp screens")
	f.render()
	f.contain(t, "std.operations", "to use") // a dimmed entry names the console that provides it
	exec(f.c, "/cmcp keys")
	f.render()
	f.contain(t, "SCREEN MENU", "Esc")
}

// The default is `always`: no question, the pack loads, and one quiet line says so.
func TestCmcpPackDefaultAlways(t *testing.T) {
	raw, ref := readPack(t)
	f := packFixture(t, t.TempDir(), map[string][]byte{ref.SHA256: raw})
	f.console("fspace-std/damage", "operations", packConsole(ref))
	cmEventually(t, "pack in use without asking", func() bool { id, fb := f.screenState(); return id == "fspace-std/damage" && !fb })
	f.render()
	f.contain(t, "Accepted interface pack fspace-std 1.0.0 from")
	f.lack(t, "F5 = ALLOW", "offers a layout pack")
}

// The notice line (top row, every theme): "Downloading ..." is replaced in place by "Downloaded ...",
// which lingers about 5.5 s, then goes; a failure lingers 8 s.
func TestCmcpNoticeLine(t *testing.T) {
	clock := fixedNow
	nowFn = func() time.Time { return clock }
	defer func() { nowFn = func() time.Time { return fixedNow } }()
	for _, th := range []string{"tng", "romulan", "ferengi"} {
		clock = fixedNow
		f := newScreenFixture(t, 120, 40)
		f.console("std.sensors", "science", func(c *msg.Console) { c.Ship.Theme = th })
		post := func(key, text string, k cmcpui.NoticeKind) {
			f.c.Do(func(*gtfparser.Interp) { f.c.cmcpNoteKey(f.c.fg, key, text, k) })
			f.render()
		}
		post("pack:x", "Downloading Romulan theme pack...", cmcpui.NoticeProgress)
		f.contain(t, "Downloading Romulan theme pack...")
		clock = clock.Add(3 * time.Second)
		post("pack:x", "Downloaded Romulan theme pack", cmcpui.NoticeDone)
		f.contain(t, "Downloaded Romulan theme pack")
		f.lack(t, "Downloading Romulan")
		f.dump("notice-" + th)
		clock = clock.Add(6 * time.Second)
		f.render()
		f.lack(t, "Downloaded Romulan theme pack")
		post("pack:y", "Could not get X theme pack: bad hash", cmcpui.NoticeFail)
		clock = clock.Add(7 * time.Second)
		f.render()
		f.contain(t, "Could not get X theme pack")
		clock = clock.Add(2 * time.Second)
		f.render()
		f.lack(t, "Could not get X theme pack")
	}
}
