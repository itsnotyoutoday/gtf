// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package client

import (
	"context"
	"github.com/itsnotyoutoday/gtf"
	"github.com/itsnotyoutoday/gtf/internal/update"
	"strings"
	"testing"
	"time"

	gnet "github.com/itsnotyoutoday/gtf/internal/net"
	"github.com/itsnotyoutoday/gtf/internal/net/nettest"
	"github.com/itsnotyoutoday/gtf/internal/net/telnet"
	"github.com/itsnotyoutoday/gtf/internal/ui/vt"
	"github.com/itsnotyoutoday/gtfparser"
)

type fakeDialer struct {
	t   *testing.T
	srv chan *nettest.Server
}

func (d *fakeDialer) Dial(ctx context.Context, w World, tr *DialTrace) (*gnet.Conn, error) {
	rw, srv := nettest.Pair(d.t)
	d.srv <- srv
	return gnet.NewConn(rw, gnet.Options{}), nil
}

func newTestClient(t *testing.T) (*Client, *vt.Screen, *fakeDialer) {
	t.Helper()
	scr := vt.New(100, 30)
	d := &fakeDialer{t: t, srv: make(chan *nettest.Server, 4)}
	c, err := New(Config{Term: scr, HomeDir: t.TempDir(), NoRC: true, Quiet: true, Dialer: d, Interactive: true, NoUpdateCheck: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	return c, scr, d
}

func waitScreen(t *testing.T, c *Client, scr *vt.Screen, want string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		c.Flush()
		if strings.Contains(scr.Text(), want) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("screen never showed %q:\n%s", want, scr.Dump())
}

func TestConnectGmcpPanelAndInput(t *testing.T) {
	c, scr, d := newTestClient(t)
	c.Do(func(in *gtfparser.Interp) {
		in.Exec("/addworld w god pw localhost 4000")
		in.Exec("/def -t'secret *' -ag hush")
		in.Exec("/panel -dock left -w 24 -border single -title Vitals vit = HP {bar \"gmcp:Char.Vitals.hp\" \"gmcp:Char.Vitals.maxhp\" width=10 chars=ascii color=red} {gmcp:Char.Vitals.hp}")
		in.Exec("/connect w")
	})
	srv := <-d.srv
	// auto-login in the tiny format
	srv.Expect([]byte("connect god pw\r\n"))
	srv.Send(nettest.IAC(telnet.WILL, telnet.OptGMCP))
	srv.Expect(nettest.IAC(telnet.DO, telnet.OptGMCP))
	srv.Read(len(nettest.SB(telnet.OptGMCP, []byte(`Core.Hello {"client":"GTF","version":"`+Version+`"}`)...)))
	srv.Read(len(nettest.SB(telnet.OptGMCP, []byte(`Core.Supports.Set ["Char 1","Room 1","Comm 1","Core 1","Group 1","Client.Media 1"]`)...)))
	srv.Send([]byte("Welcome to the test game\r\nsecret plans\r\n"))
	srv.Send(nettest.SB(telnet.OptGMCP, []byte(`Char.Vitals {"hp":40,"maxhp":80}`)...))
	waitScreen(t, c, scr, "Welcome to the test game")
	waitScreen(t, c, scr, "HP #####-----")
	if strings.Contains(scr.Text(), "secret plans") {
		t.Fatalf("gagged line shown:\n%s", scr.Dump())
	}
	// data is readable from scripts
	var got string
	c.Do(func(in *gtfparser.Interp) {
		got = in.Run("/echo hp=$[gmcp_get(\"Char.Vitals.hp\")] j=$[gmcp_json(\"Char.Vitals\")]").String()
		in.Run("/test echo(strcat(\"hp=\", gmcp_get(\"Char.Vitals.hp\"), \" \", gmcp_json(\"Char.Vitals\")))")
	})
	_ = got
	waitScreen(t, c, scr, `hp=40 {"hp":40,"maxhp":80}`)
	// typing goes through the engine's key dispatch to the world
	c.Do(func(*gtfparser.Interp) { c.onKeys("look\r") })
	srv.Expect([]byte("look\r\n"))
	waitScreen(t, c, scr, "look") // scrolled up in the input window
	// the status line shows the world
	waitScreen(t, c, scr, "w___")
	// panels degrade on a narrow terminal and come back
	scr.Resize(60, 30)
	c.Do(func(*gtfparser.Interp) { c.onResize() })
	c.Flush()
	if strings.Contains(scr.Text(), "Vitals") && !strings.Contains(scr.Text(), "│") {
		t.Fatalf("unexpected:\n%s", scr.Dump())
	}
}

func TestStartupLookAndTfKeys(t *testing.T) {
	scr := vt.New(80, 24)
	c, err := New(Config{Term: scr, HomeDir: t.TempDir(), NoRC: true, Interactive: true, NoUpdateCheck: true})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.Do(func(*gtfparser.Interp) { c.onKeys("abc def") })
	c.Do(func(*gtfparser.Interp) { c.onKeys("\x01X") }) // ^A then X
	c.Flush()
	l := scr.Lines()
	if !strings.HasPrefix(l[20], "_________(no world)") || l[21] != "Xabc def" {
		t.Fatalf("\n%s", scr.Dump())
	}
	if scr.Y != 21 || scr.X != 1 {
		t.Fatalf("cursor %d,%d", scr.Y, scr.X)
	}
	c.Do(func(*gtfparser.Interp) { c.onKeys("\x1b[3~") }) // delete key via kbbind.tf macros
	c.Flush()
	if got := scr.Lines()[21]; got != "Xbc def" && got != "Xabc def" {
		t.Fatalf("%q", got)
	}
}

func TestPagerAndRecall(t *testing.T) {
	scr := vt.New(80, 24)
	c, err := New(Config{Term: scr, HomeDir: t.TempDir(), NoRC: true, Quiet: true, Interactive: true, NoUpdateCheck: true})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.Do(func(in *gtfparser.Interp) {
		in.Exec("/set more=on")
		in.Exec("/for i 1 40 /echo m%i")
	})
	c.Flush()
	var paused bool
	var size int64
	c.Do(func(in *gtfparser.Interp) {
		paused = in.Run("/test morepaused()").Truth()
		size = in.Run("/test moresize()").Int()
	})
	if !paused || size == 0 {
		t.Fatalf("paused=%v size=%d\n%s", paused, size, scr.Dump())
	}
	c.Do(func(*gtfparser.Interp) { c.onKeys("\t\t") }) // tab = page
	c.Flush()
	if !strings.Contains(scr.Text(), "m40") {
		t.Fatalf("page key did not show the rest:\n%s", scr.Dump())
	}
	// /recall shows history through the engine
	c.Do(func(in *gtfparser.Interp) { in.Exec("/recall -2") })
	c.Flush()
}

func TestIdentityBannerVersionAndCopyright(t *testing.T) {
	scr := vt.New(130, 150)
	c, err := New(Config{Term: scr, HomeDir: t.TempDir(), NoRC: true, Interactive: true, NoUpdateCheck: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	c.Flush()
	txt := scr.Text()
	for _, w := range []string{
		"GoTinyFugue version " + Version + " (gtf), a TinyFugue-compatible client",
		"Copyright (C) 2026 Rick Bird. Compatible with TinyFugue 5.0 beta 8 (Copyright (C) 1993-2007 Ken Keys).",
		"Type `/quit' to quit gtf.",
	} {
		if !strings.Contains(txt, w) {
			t.Errorf("banner lacks %q:\n%s", w, scr.Dump())
		}
	}
	c.Do(func(in *gtfparser.Interp) {
		if v := in.Var("version"); v != VersionVar() {
			t.Errorf("version = %q", v)
		}
		in.Exec("/version")
		in.Exec("/ver")
		in.Exec("/help copyright")
	})
	c.Flush()
	txt = scr.Text()
	for _, w := range []string{"% GoTinyFugue version " + Version + " (gtf), compatible with", "\n5.0 beta 8\n"} {
		if !strings.Contains(txt, w) {
			t.Errorf("missing %q:\n%s", w, scr.Dump())
		}
	}
	// own notice first, then the original TinyFugue text
	own := strings.Index(txt, "Copyright (C) 2026 Rick Bird\n")
	orig := strings.Index(txt, "TinyFugue - programmable mud client")
	if own < 0 || orig < 0 || own > orig {
		t.Errorf("copyright order wrong (own=%d orig=%d):\n%s", own, orig, scr.Dump())
	}
	for _, w := range []string{"WITHOUT ANY WARRANTY", "gtfparser", "Ken Keys"} {
		if !strings.Contains(txt, w) {
			t.Errorf("copyright lacks %q", w)
		}
	}
}

// A panel bound to a variable that a timer-driven /repeat changes must repaint
// on its own, without any keyboard or network activity.
func TestPanelRepaintsOnTimerWork(t *testing.T) {
	c, scr, _ := newTestClient(t)
	c.Start()
	c.Do(func(in *gtfparser.Interp) {
		in.Exec("/set tickv=0")
		in.Exec("/def -i bump = /set tickv=$[tickv + 1]%; /repeat -1 1 /bump")
		in.Exec("/panel -dock top -h 1 t = TICK {tickv}")
		in.Exec("/bump")
	})
	waitScreen(t, c, scr, "TICK 1")
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		seen := false
		c.Do(func(*gtfparser.Interp) { seen = strings.Contains(scr.Text(), "TICK 3") }) // no Flush: the client must repaint by itself
		if seen {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("panel never advanced to TICK 3")
}

func TestParseUpdateArgs(t *testing.T) {
	a := parseUpdateArgs([]string{"-y"})
	if !a.install || a.restart {
		t.Errorf("-y: %+v", a)
	}
	a = parseUpdateArgs([]string{"-y", "-r"})
	if !a.install || !a.restart {
		t.Errorf("-y -r: %+v", a)
	}
	a = parseUpdateArgs([]string{"-channel=stable", "-check=off"})
	if a.channel != "stable" || a.check != "off" || a.install {
		t.Errorf("settings: %+v", a)
	}
	if a = parseUpdateArgs([]string{"--bogus"}); a.bad != "bogus" {
		t.Errorf("bad option not reported: %+v", a)
	}
}

// A development build (no release version) must never check or install anything.
func TestUpdateCheckSkippedForDevBuild(t *testing.T) {
	old := Version
	Version = "dev"
	defer func() { Version = old }()
	c, _, _ := newTestClient(t)
	c.startUpdateCheck() // must neither panic nor contact anything
}

// /version names us, not TinyFugue's author, and keeps the shape the library's /ver parses.
func TestVersionCommandHasNoKenKeys(t *testing.T) {
	c, scr, _ := newTestClient(t)
	c.Do(func(in *gtfparser.Interp) { in.Exec("/version") })
	waitScreen(t, c, scr, "Copyright (C) "+Year+" "+Holder)
	var text string
	c.Do(func(*gtfparser.Interp) { text = scr.Text() })
	i := strings.Index(text, "% "+VersionVar()+".")
	if i < 0 {
		t.Fatalf("version line missing:\n%s", text)
	}
	// the output of /version itself (after the banner): no mention of Ken Keys
	if strings.Contains(text[i:], "Ken Keys") {
		t.Fatalf("/version output names Ken Keys:\n%s", text[i:])
	}
}

// The version is the VERSION file's content, compiled in: a plain build says the right number.
func TestVersionComesFromVersionFile(t *testing.T) {
	if versionOverride == "" && Version != gtf.Version() {
		t.Fatalf("Version = %q, VERSION file = %q", Version, gtf.Version())
	}
	if _, err := update.ParseVersion(Version); err != nil {
		t.Fatalf("the VERSION file must hold a semantic version: %v", err)
	}
	if !strings.Contains(VersionLine(), Version) {
		t.Fatalf("VersionLine %q lacks %q", VersionLine(), Version)
	}
}

// A prompt (the server's @prompt text, ANSI coloured, ended by IAC GA) goes in the input bar WITH its colours.
func TestColouredPromptKeepsItsColourInTheInputBar(t *testing.T) {
	c, scr, d := newTestClient(t)
	c.Do(func(in *gtfparser.Interp) {
		in.Exec("/addworld w localhost 4000")
		in.Exec("/connect w")
	})
	srv := <-d.srv
	srv.Send([]byte("hello\r\n\x1b[1;36m[N/P] Page \x1b[0m->"))
	srv.Send(nettest.IAC(telnet.GA))
	waitScreen(t, c, scr, "[N/P] Page ->")
	var attr string
	c.Do(func(*gtfparser.Interp) {
		for y := 0; y < scr.H; y++ {
			if strings.Contains(scr.Row(y), "[N/P] Page ->") && scr.Row(y) != "" {
				attr = scr.CellAt(strings.Index(scr.Row(y), "["), y).Attr
			}
		}
	})
	if attr == "" || !strings.Contains(attr, "1") || !(strings.Contains(attr, "36") || strings.Contains(attr, "fg=")) {
		t.Fatalf("the prompt in the input bar lost its colour: attr %q\n%s", attr, scr.Dump())
	}
}
