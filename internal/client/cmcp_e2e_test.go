// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package client

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/itsnotyoutoday/cmcp/session"
	"github.com/itsnotyoutoday/gtf/internal/ui/vt"
	"github.com/itsnotyoutoday/gtfparser"
)

// TestCmcpEvenniaE2E drives gtf against the Evennia PoC on 127.0.0.1:4101 (GTF_E2E_EVENNIA=1;
// never the owner's CobraMUSH game). Throwaway account alice.
func TestCmcpEvenniaE2E(t *testing.T) {
	if os.Getenv("GTF_E2E_EVENNIA") == "" {
		t.Skip("set GTF_E2E_EVENNIA=1 with the Evennia PoC running on 127.0.0.1:4101")
	}
	term := &recTerm{Screen: vt.New(120, 40)}
	c, err := New(Config{Term: term, HomeDir: t.TempDir(), NoRC: true, Quiet: true, Interactive: true, NoUpdateCheck: true})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	f := &screenFixture{t: t, c: c, term: term}
	exec(c, "/set cmcp_pack_consent=always", "/connect 127.0.0.1 4101")
	cmEventually(t, "handshake", f.clientReady)
	exec(c, "connect alice ember-tide-91")
	time.Sleep(1500 * time.Millisecond)
	for _, cmd := range []string{"board resolute", "man opera"} {
		exec(c, cmd)
		time.Sleep(1500 * time.Millisecond)
	}
	var id string
	cmEventually(t, "an operations screen", func() bool {
		c.Do(func(*gtfparser.Interp) { id = c.fg.cm.UI().Screen.Resolved })
		return strings.Contains(id, "operations")
	})
	time.Sleep(2 * time.Second)
	f.render()
	t.Logf("screen %s\n%s", id, term.Dump())
	f.dump("evennia-ops")
	exec(c, "/cmcp status")
	f.render()
	var st session.State
	c.Do(func(*gtfparser.Interp) { st = c.fg.cm.UI().State })
	if st != session.StateReady {
		t.Fatal(st)
	}
	exec(c, "unman")
	time.Sleep(time.Second)
}

// TestCmcpEvenniaTransporterAndDamage walks to the transporter room (the context stack), then
// mans operations and assigns a damage team (GTF_E2E_EVENNIA=1; Evennia PoC on 127.0.0.1:4101).
func TestCmcpEvenniaTransporterAndDamage(t *testing.T) {
	if os.Getenv("GTF_E2E_EVENNIA") == "" {
		t.Skip("set GTF_E2E_EVENNIA=1 with the Evennia PoC running on 127.0.0.1:4101")
	}
	term := &recTerm{Screen: vt.New(120, 40)}
	c, err := New(Config{Term: term, HomeDir: t.TempDir(), NoRC: true, Quiet: true, Interactive: true, NoUpdateCheck: true})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	f := &screenFixture{t: t, c: c, term: term}
	exec(c, "/set cmcp_pack_consent=always", "/set cmcp_debug=1", "/connect 127.0.0.1 4101")
	cmEventually(t, "handshake", f.clientReady)
	exec(c, "connect bob cobalt-fern-52")
	time.Sleep(1500 * time.Millisecond)
	screen := func() (id string, stack int) {
		c.Do(func(*gtfparser.Interp) { u := c.fg.cm.UI(); id, stack = u.Screen.Resolved, len(u.Stack) })
		return
	}
	for _, cmd := range []string{"board resolute", "lift", "d3", "tr"} {
		exec(c, cmd)
		time.Sleep(1500 * time.Millisecond)
	}
	f.render()
	id, n := screen()
	t.Logf("after tr: screen %s, stack %d\n%s", id, n, term.Dump())
	f.dump("evennia-transporter")
	for _, cmd := range []string{"corridor", "board resolute", "man opera"} {
		exec(c, cmd)
		time.Sleep(1200 * time.Millisecond)
	}
	exec(c, "/cmcp screen std.damage")
	time.Sleep(1500 * time.Millisecond)
	exec(c, "/cmcp send dmg.assign team=1 system=Hull")
	time.Sleep(1500 * time.Millisecond)
	f.render()
	id, n = screen()
	t.Logf("operations/damage: screen %s, stack %d\n%s", id, n, term.Dump())
	f.dump("evennia-damage")
	exec(c, "unman")
	time.Sleep(time.Second)
}

// TestCmcpEvenniaScenicViewscreen turns the ship toward a nearby body on the live PoC and dumps the
// helm screen with its viewscreen (GTF_E2E_EVENNIA=1; read-only apart from the helm heading).
func TestCmcpEvenniaScenicViewscreen(t *testing.T) {
	if os.Getenv("GTF_E2E_EVENNIA") == "" {
		t.Skip("set GTF_E2E_EVENNIA=1 with the Evennia PoC running on 127.0.0.1:4101")
	}
	term := &recTerm{Screen: vt.New(140, 44)}
	c, err := New(Config{Term: term, HomeDir: t.TempDir(), NoRC: true, Quiet: true, Interactive: true, NoUpdateCheck: true})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	f := &screenFixture{t: t, c: c, term: term}
	exec(c, "/set cmcp_pack_consent=always", "/connect 127.0.0.1 4101")
	cmEventually(t, "handshake", f.clientReady)
	exec(c, "connect carol violet-oak-38")
	time.Sleep(1500 * time.Millisecond)
	for _, cmd := range []string{"board resolute", "man helm"} {
		exec(c, cmd)
		time.Sleep(1500 * time.Millisecond)
	}
	// aim at the nearest planet or station: the contact list tells the bearing
	var az, name string
	c.Do(func(*gtfparser.Interp) {
		best := 1e300
		if m, ok := c.fg.cm.UI().Docs["Space.Contacts"].(map[string]any); ok {
			for _, v := range m["contacts"].(map[string]any) {
				ct := v.(map[string]any)
				d, _ := ct["distance_m"].(float64)
				if ty, _ := ct["type"].(string); (ty == "Planet" || ty == "Station" || ty == "Ship") && d < best && d > 0 {
					best = d
					if b, ok := ct["bearing"].(map[string]any); ok {
						az = fmt.Sprintf("%.0f %.0f 0", b["az"], b["mark"])
						name, _ = ct["name"].(string)
					}
				}
			}
		}
	})
	t.Logf("turning toward %s: heading %s", name, az)
	exec(c, "heading "+az)
	time.Sleep(6 * time.Second)
	exec(c, "/cmcp status")
	f.render()
	t.Logf("%s", term.Dump())
	f.dump("evennia-scenic")
	exec(c, "unman")
	time.Sleep(time.Second)
}

// TestCmcpEvenniaEmpires boards the Klingon, Romulan and Ferengi ships of the PoC, downloads their
// layout packs over the announced loopback http origin (developer flag), and dumps each console
// screen (GTF_E2E_EVENNIA=1; boarding only moves the throwaway character).
func TestCmcpEvenniaEmpires(t *testing.T) {
	if os.Getenv("GTF_E2E_EVENNIA") == "" {
		t.Skip("set GTF_E2E_EVENNIA=1 with the Evennia PoC running on 127.0.0.1:4101")
	}
	term := &recTerm{Screen: vt.New(140, 44)}
	c, err := New(Config{Term: term, HomeDir: t.TempDir(), NoRC: true, Quiet: true, Interactive: true, NoUpdateCheck: true})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	f := &screenFixture{t: t, c: c, term: term}
	exec(c, "/set cmcp_pack_consent=always", "/set cmcp_dev_loopback_http=on", "/set cmcp_debug=1", "/connect 127.0.0.1 4101")
	cmEventually(t, "handshake", f.clientReady)
	exec(c, "connect carol violet-oak-38")
	time.Sleep(1500 * time.Millisecond)
	for _, ship := range []string{"drakhar", "nhaalveth", "goldenmargin"} {
		exec(c, "board "+ship, "man sensors")
		time.Sleep(4 * time.Second)
		exec(c, "/cmcp packs", "/cmcp status")
		f.render()
		var scr, th string
		var fb bool
		c.Do(func(*gtfparser.Interp) {
			u := c.fg.cm.UI()
			scr, fb = u.Screen.Resolved, u.Screen.Fallback
			th = c.cmcpThemeFor(c.fg, u, "")
		})
		t.Logf("%s: screen %s fallback=%v theme=%s\n%s", ship, scr, fb, th, term.Dump())
		f.dump("empire-" + ship)
	}
}
