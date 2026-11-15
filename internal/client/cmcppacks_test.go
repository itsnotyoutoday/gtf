// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package client

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/itsnotyoutoday/cmcp/msg"
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
	f.console("fspace-std/damage", "operations", packConsole(ref))
	if id, fb := f.screenState(); id != "std.operations" || !fb {
		t.Fatalf("expected the built-in screen first, got %q fallback=%v", id, fb)
	}
	mustContain(t, f.term.Screen, "offers a layout pack")
	if f.d.gets != 0 {
		t.Fatal("fetched before the player agreed")
	}
	exec(f.c, "/cmcp pack allow")
	cmEventually(t, "pack in use", func() bool { id, fb := f.screenState(); return id == "fspace-std/damage" && !fb })
	if f.d.gets != 1 {
		t.Fatalf("Pack.Get sent %d times", f.d.gets)
	}
	f.render()
	mustContain(t, f.term.Screen, "DAMAGE CONTROL")
	f.c.Close()

	// second launch: same home, same host: cached and already allowed
	f2 := packFixture(t, home, map[string][]byte{ref.SHA256: raw})
	f2.console("fspace-std/damage", "operations", packConsole(ref))
	cmEventually(t, "pack from cache", func() bool { id, fb := f2.screenState(); return id == "fspace-std/damage" && !fb })
	if f2.d.gets != 0 {
		t.Fatalf("fetched again (%d)", f2.d.gets)
	}
	mustNotContain(t, f2.term.Screen, "offers a layout pack")
}

func TestCmcpPackRefused(t *testing.T) {
	raw, ref := readPack(t)
	f := packFixture(t, t.TempDir(), map[string][]byte{ref.SHA256: raw})
	f.console("fspace-std/damage", "operations", packConsole(ref))
	exec(f.c, "/cmcp pack deny")
	time.Sleep(100 * time.Millisecond)
	if id, fb := f.screenState(); id != "std.operations" || !fb || f.d.gets != 0 {
		t.Fatalf("refused pack changed things: %q %v gets=%d", id, fb, f.d.gets)
	}
	// the answer is remembered: announcing it again does not ask again
	f.console("fspace-std/damage", "operations", func(c *msg.Console) { packConsole(ref)(c); c.Reason = "pack_changed" })
	mustNotContain(t, f.term.Screen, "offers a layout pack\n")
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
	cmEventually(t, "transfer attempted", func() bool { return f.d.gets == 1 })
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
	if f.d.gets != 0 {
		t.Fatal("fetched in never mode")
	}
	exec(f.c, "/cmcp packs")
	f.render()
	mustContain(t, f.term.Screen, "consent mode: never")
}
