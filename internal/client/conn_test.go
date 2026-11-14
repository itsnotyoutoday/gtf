// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package client

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	gnet "github.com/itsnotyoutoday/gtf/internal/net"
	"github.com/itsnotyoutoday/gtf/internal/net/nettest"
	"github.com/itsnotyoutoday/gtf/internal/ui/vt"
	"github.com/itsnotyoutoday/gtfparser"
)

// scriptDialer plays the part of the network: a connection to host:port either
// fails the way a refused or unresolvable one does, or yields a fake server.
type scriptDialer struct {
	t    *testing.T
	mu   sync.Mutex
	fail map[string]*DialError // "host port" -> error
	srv  map[string]*nettest.Server
	slow map[string]bool // report a lookup that takes a while
	// more lists extra addresses to fail over: host -> addresses tried (and failed) first
	more map[string][]string
}

func newScriptDialer(t *testing.T) *scriptDialer {
	return &scriptDialer{t: t, fail: map[string]*DialError{}, srv: map[string]*nettest.Server{}, slow: map[string]bool{}, more: map[string][]string{}}
}

func (d *scriptDialer) Dial(ctx context.Context, w World, tr *DialTrace) (*gnet.Conn, error) {
	if d.slow[w.Host] {
		tr.lookup()
	}
	if e := d.fail[w.Host+" "+w.Port]; e != nil && e.Lookup {
		return nil, e
	}
	for _, a := range d.more[w.Host] {
		tr.try(a + " " + w.Port)
		tr.failed(a+" "+w.Port, "Connection refused")
	}
	addr := "127.0.0.1 " + w.Port
	tr.try(addr)
	if e := d.fail[w.Host+" "+w.Port]; e != nil {
		return nil, &DialError{Addr: addr, Why: e.Why}
	}
	rw, srv := nettest.Pair(d.t)
	d.mu.Lock()
	d.srv[w.Name] = srv
	d.mu.Unlock()
	return gnet.NewConn(rw, gnet.Options{}), nil
}

func (d *scriptDialer) server(t *testing.T, c *Client, world string) *nettest.Server {
	t.Helper()
	for i := 0; i < 200; i++ {
		d.mu.Lock()
		s := d.srv[world]
		d.mu.Unlock()
		if s != nil {
			return s
		}
		c.Flush()
		sleepMs(10)
	}
	t.Fatalf("no connection to %s", world)
	return nil
}

func newConnTest(t *testing.T) (*Client, *vt.Screen, *scriptDialer) {
	t.Helper()
	scr := vt.New(80, 24)
	d := newScriptDialer(t)
	c, err := New(Config{Term: scr, HomeDir: t.TempDir(), NoRC: true, Quiet: true, Dialer: d, Interactive: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	return c, scr, d
}

func exec(c *Client, cmds ...string) {
	c.Do(func(in *gtfparser.Interp) {
		for _, cmd := range cmds {
			in.Exec(cmd)
		}
	})
}

func statusRow(scr *vt.Screen) string {
	for _, l := range scr.Lines() {
		if strings.HasPrefix(l, "_") {
			return l
		}
	}
	return ""
}

func sockets(c *Client) (out []gtfparser.SocketInfo) {
	c.Do(func(*gtfparser.Interp) { out = c.Sockets() })
	return
}

func mustContain(t *testing.T, scr *vt.Screen, subs ...string) {
	t.Helper()
	for _, s := range subs {
		if !strings.Contains(scr.Text(), s) {
			t.Fatalf("screen lacks %q:\n%s", s, scr.Dump())
		}
	}
}

func mustNotContain(t *testing.T, scr *vt.Screen, subs ...string) {
	t.Helper()
	for _, s := range subs {
		if strings.Contains(scr.Text(), s) {
			t.Fatalf("screen has %q:\n%s", s, scr.Dump())
		}
	}
}

// A refused connection reports itself like tf: the attempt, then the failure,
// on the new world's own screen, and the world stays in the foreground, dead.
func TestConnectRefusedMessages(t *testing.T) {
	c, scr, d := newConnTest(t)
	d.fail["localhost 3"] = &DialError{Why: "Connection refused"}
	exec(c, "/connect localhost 3")
	waitScreen(t, c, scr, "% Connection to (unnamed1) failed: 127.0.0.1 3: Connection refused")
	mustContain(t, scr, "% Trying to connect to (unnamed1): 127.0.0.1 3.")
	mustNotContain(t, scr, "No world", "---- World")
	if st := statusRow(scr); !strings.HasPrefix(st, "_________!(unnamed1)") {
		t.Fatalf("status %q", st)
	}
	s := sockets(c)
	if len(s) != 1 || s[0].State != gtfparser.SockZombie {
		t.Fatalf("sockets %+v", s)
	}
}

// /telnet makes named worlds "host,port"; a bare /connect numbers its
// temporary worlds per attempt, and a dead one goes when another takes over.
func TestTelnetNamesAndUnnamedNumbering(t *testing.T) {
	c, scr, d := newConnTest(t)
	d.fail["localhost 1"] = &DialError{Why: "Connection refused"}
	d.fail["localhost 3"] = &DialError{Why: "Connection refused"}
	exec(c, "/telnet localhost 1")
	waitScreen(t, c, scr, "% Connection to localhost,1 failed: 127.0.0.1 1: Connection refused")
	exec(c, "/connect localhost 3")
	waitScreen(t, c, scr, "% Connection to (unnamed1) failed: 127.0.0.1 3: Connection refused")
	exec(c, "/connect localhost 3")
	waitScreen(t, c, scr, "% Connection to (unnamed2) failed")
	mustNotContain(t, scr, "localhost,1")
	if s := sockets(c); len(s) != 1 || s[0].World != "(unnamed2)" {
		t.Fatalf("sockets %+v", s)
	}
}

func TestDnsFailureAndLookupNotice(t *testing.T) {
	c, scr, d := newConnTest(t)
	d.fail["nosuchhost.invalid 1"] = &DialError{Lookup: true, Why: "Unknown host"}
	d.slow["nosuchhost.invalid"] = true
	exec(c, "/connect nosuchhost.invalid 1")
	waitScreen(t, c, scr, "% Unable to connect to (unnamed1): nosuchhost.invalid 1: Unknown host")
	mustContain(t, scr, "% Hostname lookup for (unnamed1) in progress.")
	mustNotContain(t, scr, "Trying to connect")
}

func TestFailoverToNextAddress(t *testing.T) {
	c, scr, d := newConnTest(t)
	d.more["multi"] = []string{"10.0.0.1"}
	exec(c, "/connect multi 4000")
	waitScreen(t, c, scr, "% Connected to (unnamed1).")
	mustContain(t, scr, "% Trying to connect to (unnamed1): 10.0.0.1 4000.",
		"% Intermediate connection to (unnamed1) failed: 10.0.0.1 4000: Connection",
		"% Trying to connect to (unnamed1): 127.0.0.1 4000.")
}

// The server closing the connection: tf's message, the world stays in the
// foreground as a dead socket, and it is forgotten when another world replaces it.
func TestForeignCloseKeepsDeadSocketUntilReplaced(t *testing.T) {
	c, scr, d := newConnTest(t)
	exec(c, "/addworld A localhost 2", "/addworld B localhost 4", "/connect A")
	srv := d.server(t, c, "A")
	srv.Send([]byte("hello\r\n"))
	waitScreen(t, c, scr, "hello")
	srv.Close()
	waitScreen(t, c, scr, "% Connection to A closed by foreign host.")
	if st := statusRow(scr); !strings.HasPrefix(st, "_________!A") {
		t.Fatalf("status %q", st)
	}
	if s := sockets(c); len(s) != 1 || s[0].State != gtfparser.SockZombie {
		t.Fatalf("sockets %+v", s)
	}
	exec(c, "/connect B")
	waitScreen(t, c, scr, "% Connected to B.")
	if s := sockets(c); len(s) != 1 || s[0].World != "B" {
		t.Fatalf("dead socket not forgotten: %+v", s)
	}
	// the world's screen outlives its socket: reconnecting shows the old text
	exec(c, "/fg -n", "/connect A")
	waitScreen(t, c, scr, "% Connected to A.")
	mustContain(t, scr, "hello", "closed by foreign host")
}

// /dc reports in the world, runs no DISCONNECT hook, and leaves a dead socket.
func TestDcMessageAndNoHook(t *testing.T) {
	c, scr, d := newConnTest(t)
	exec(c, "/addworld A localhost 2", "/def -hDISCONNECT = /echo [disconnect hook]", "/connect A")
	d.server(t, c, "A")
	waitScreen(t, c, scr, "% Connected to A.")
	exec(c, "/dc")
	waitScreen(t, c, scr, "% Connection to A closed.")
	mustNotContain(t, scr, "disconnect hook", "foreign host")
	exec(c, "/dc")
	c.Flush()
	if n := strings.Count(scr.Text(), "% Connection to A closed."); n != 2 {
		t.Fatalf("a second /dc reports again (tf), got %d:\n%s", n, scr.Dump())
	}
	exec(c, "/dc nosuch")
	waitScreen(t, c, scr, "Not connected to nosuch")
}

// Text arriving for a background world raises the activity alert on the status
// line; bringing the world forward shows the divider before the new lines and
// clears the alert; the "---- World ----" line is not printed (virtscreen on).
func TestBackgroundActivityAlertAndDivider(t *testing.T) {
	c, scr, d := newConnTest(t)
	exec(c, "/addworld A localhost 2", "/addworld B localhost 4", "/connect A")
	srvA := d.server(t, c, "A")
	waitScreen(t, c, scr, "% Connected to A.")
	exec(c, "/connect B")
	d.server(t, c, "B")
	waitScreen(t, c, scr, "% Connected to B.")
	srvA.Send([]byte("bg line\r\n"))
	waitScreen(t, c, scr, "% Activity in world A")
	if row := statusRow(scr); !strings.Contains(row, "B") || !strings.Contains(row, "% Activity in world A") {
		t.Fatalf("status %q", row)
	}
	mustNotContain(t, scr, "bg line")
	exec(c, "/fg A")
	waitScreen(t, c, scr, "bg line")
	mustContain(t, scr, "=====")
	mustNotContain(t, scr, "% Activity in world A", "---- World")
	lines := scr.Lines()
	div, bg := -1, -1
	for i, l := range lines {
		if strings.HasPrefix(l, "=====") {
			div = i
		}
		if strings.HasPrefix(l, "bg line") {
			bg = i
		}
	}
	if div < 0 || bg != div+1 {
		t.Fatalf("divider not right before the new line:\n%s", scr.Dump())
	}
}

// With %virtscreen off the world switch is announced on the (shared) screen.
func TestWorldLineWithVirtscreenOff(t *testing.T) {
	c, scr, d := newConnTest(t)
	exec(c, "/set virtscreen=off", "/addworld A localhost 2", "/connect A")
	d.server(t, c, "A")
	waitScreen(t, c, scr, "---- World A ----")
	exec(c, "/fg -n")
	waitScreen(t, c, scr, "---- No world ----")
}

// A world that finishes connecting in the background says so in its own
// screen and on the status line.
func TestBackgroundConnectIsAlerted(t *testing.T) {
	c, scr, d := newConnTest(t)
	exec(c, "/addworld A localhost 2", "/addworld B localhost 4", "/connect A")
	d.server(t, c, "A")
	waitScreen(t, c, scr, "% Connected to A.")
	exec(c, "/connect -b B")
	d.server(t, c, "B")
	waitScreen(t, c, scr, "% Connected to B.")
	if st := statusRow(scr); !strings.Contains(st, "A") || strings.Contains(st, "B_") {
		t.Fatalf("status %q", st)
	}
	exec(c, "/fg B")
	waitScreen(t, c, scr, "% Trying to connect to B")
}

func TestConnectTwiceIsAnError(t *testing.T) {
	c, scr, d := newConnTest(t)
	exec(c, "/addworld A localhost 2", "/connect A")
	d.server(t, c, "A")
	waitScreen(t, c, scr, "% Connected to A.")
	exec(c, "/connect A")
	waitScreen(t, c, scr, "socket to A already exists")
}

func TestListsocketsShowsDeadSocket(t *testing.T) {
	c, scr, d := newConnTest(t)
	d.fail["localhost 3"] = &DialError{Why: "Connection refused"}
	exec(c, "/connect localhost 3")
	waitScreen(t, c, scr, "% Connection to (unnamed1) failed")
	exec(c, "/listsockets")
	waitScreen(t, c, scr, "LINES IDLE")
	mustContain(t, scr, "!(unnamed1)")
}

func sleepMs(n int) { time.Sleep(time.Duration(n) * time.Millisecond) }

// /connect -q with automatic login hides the server's login output up to the
// end-of-messages marker (tf's quiet login) but still says "Connected".
func TestQuietLogin(t *testing.T) {
	c, scr, d := newConnTest(t)
	exec(c, "/addworld A joe sekrit localhost 2", "/connect -q A")
	srv := d.server(t, c, "A")
	waitScreen(t, c, scr, "% Connected to A.")
	srv.Expect([]byte("connect joe sekrit\r\n"))
	srv.Send([]byte("banner one\r\nbanner two\r\n### end of messages ###\r\nwelcome back\r\n"))
	waitScreen(t, c, scr, "welcome back")
	mustNotContain(t, scr, "banner", "end of messages")
}

// -b keeps the new world in the background: the foreground screen does not
// switch, and the world's progress lines wait in its own screen.
func TestBackgroundConnectKeepsForeground(t *testing.T) {
	c, scr, d := newConnTest(t)
	exec(c, "/addworld A localhost 2", "/addworld B localhost 4", "/connect A")
	d.server(t, c, "A")
	waitScreen(t, c, scr, "% Connected to A.")
	exec(c, "/connect -b B")
	d.server(t, c, "B")
	waitScreen(t, c, scr, "% Connected to B.") // the status-line alert
	var fg string
	c.Do(func(*gtfparser.Interp) { fg = c.ForegroundWorld() })
	if fg != "A" {
		t.Fatalf("foreground is %q", fg)
	}
	mustNotContain(t, scr, "Trying to connect to B")
}
