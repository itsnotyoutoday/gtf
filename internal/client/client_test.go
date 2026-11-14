// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package client

import (
	"context"
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

func (d *fakeDialer) Dial(ctx context.Context, w World) (*gnet.Conn, error) {
	rw, srv := nettest.Pair(d.t)
	d.srv <- srv
	return gnet.NewConn(rw, gnet.Options{}), nil
}

func newTestClient(t *testing.T) (*Client, *vt.Screen, *fakeDialer) {
	t.Helper()
	scr := vt.New(100, 30)
	d := &fakeDialer{t: t, srv: make(chan *nettest.Server, 4)}
	c, err := New(Config{Term: scr, HomeDir: t.TempDir(), NoRC: true, Quiet: true, Dialer: d, Interactive: true})
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
	srv.Read(len(nettest.SB(telnet.OptGMCP, []byte(`Core.Hello {"client":"GTF","version":"0.0.1"}`)...)))
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
	c, err := New(Config{Term: scr, HomeDir: t.TempDir(), NoRC: true, Interactive: true})
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
	c, err := New(Config{Term: scr, HomeDir: t.TempDir(), NoRC: true, Quiet: true, Interactive: true})
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
	c, err := New(Config{Term: scr, HomeDir: t.TempDir(), NoRC: true, Interactive: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	c.Flush()
	txt := scr.Text()
	for _, w := range []string{
		"GoTinyFugue version 0.0.1 (gtf), a TinyFugue-compatible client",
		"Copyright (C) 2026 Rick Bird. Inspired by and compatible with TinyFugue 5.0 beta 8, Copyright (C) 1993-2007 Ken Keys.",
		"Type `/quit' to quit gtf.",
	} {
		if !strings.Contains(txt, w) {
			t.Errorf("banner lacks %q:\n%s", w, scr.Dump())
		}
	}
	c.Do(func(in *gtfparser.Interp) {
		if v := in.Var("version"); v != "GoTinyFugue version 0.0.1 (gtf), compatible with TinyFugue 5.0 beta 8" {
			t.Errorf("version = %q", v)
		}
		in.Exec("/version")
		in.Exec("/ver")
		in.Exec("/help copyright")
	})
	c.Flush()
	txt = scr.Text()
	for _, w := range []string{"% GoTinyFugue version 0.0.1 (gtf), compatible with", "\n5.0 beta 8\n"} {
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
