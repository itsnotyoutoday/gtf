// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package client

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/itsnotyoutoday/cmcp/frame"
	"github.com/itsnotyoutoday/cmcp/msg"
	"github.com/itsnotyoutoday/cmcp/session"
	gnet "github.com/itsnotyoutoday/gtf/internal/net"
	"github.com/itsnotyoutoday/gtf/internal/ui/vt"
	"github.com/itsnotyoutoday/gtfparser"
)

// cmcpGame is a fake game: a telnet peer that speaks GMCP and runs the core's session.Server.
type cmcpGame struct {
	t     *testing.T
	mu    sync.Mutex
	conn  net.Conn
	srv   *session.Server
	dec   *frame.Decoder
	got   []frame.Frame // every GMCP frame received from the client
	packs map[string][]byte
	onGet func()
	done  chan struct{}
}

type cmcpDialer struct {
	packs map[string][]byte // sha256 -> bytes the fake game serves inline
	gets  atomic.Int32      // Cobra.Pack.Get received
	t     *testing.T
	c     **Client
	gmu   sync.Mutex
	gm0   *cmcpGame
	cfg   session.ServerConfig
}

func (d *cmcpDialer) Dial(ctx context.Context, w World, tr *DialTrace) (*gnet.Conn, error) {
	tr.try("127.0.0.1 " + w.Port)
	a, b := net.Pipe()
	g := &cmcpGame{t: d.t, conn: b, srv: session.NewServer(d.cfg), dec: frame.NewDecoder(0), done: make(chan struct{})}
	d.gmu.Lock()
	d.gm0 = g
	d.gmu.Unlock()
	g.packs, g.onGet = d.packs, func() { d.gets.Add(1) }
	go g.run()
	return gnet.NewConn(a, gnet.Options{Env: (*d.c).cmcpEnv()}), nil
}

func (g *cmcpGame) write(p []byte) {
	g.conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	g.conn.Write(p)
}

// flush writes whatever the server endpoint produced.
func (g *cmcpGame) flush() {
	g.mu.Lock()
	o := g.srv.Drain()
	g.mu.Unlock()
	for _, f := range o.Frames {
		b, err := frame.Encode(f.Package, f.Payload)
		if err != nil {
			g.t.Errorf("encode %s: %v", f.Package, err)
			continue
		}
		g.write(b)
	}
}

func (g *cmcpGame) run() {
	defer close(g.done)
	g.write([]byte{255, 251, 201}) // IAC WILL GMCP
	buf := make([]byte, 8192)
	for {
		n, err := g.conn.Read(buf)
		if err != nil {
			return
		}
		p := buf[:n]
		for i := 0; i+2 < len(p); i++ {
			if p[i] == 255 && p[i+1] == 253 && p[i+2] == 201 {
				g.mu.Lock()
				g.srv.TelnetEvent(session.DoGMCP)
				g.mu.Unlock()
			}
		}
		for _, r := range g.dec.Feed(p) {
			if r.Err != nil {
				continue
			}
			g.mu.Lock()
			g.got = append(g.got, r.Frame)
			g.srv.Receive(time.Now(), r.Frame)
			g.mu.Unlock()
			if r.Frame.Package == "Cobra.Pack.Get" {
				g.servePack(r.Frame)
			}
		}
		g.flush()
	}
}

// servePack answers Cobra.Pack.Get with inline chunks of the pack registered for the hash.
func (g *cmcpGame) servePack(f frame.Frame) {
	var m msg.PackGet
	if json.Unmarshal(f.Payload, &m) != nil {
		return
	}
	if g.onGet != nil {
		g.onGet()
	}
	raw, ok := g.packs[m.SHA256]
	if !ok {
		b, _ := frame.Encode("Cobra.Pack.Fail", msg.PackFail{ID: m.ID, SHA256: m.SHA256, Err: "not_found"})
		g.write(b)
		return
	}
	const chunk = 8192
	parts := (len(raw) + chunk - 1) / chunk
	for i := 0; i < parts; i++ {
		end := min(len(raw), (i+1)*chunk)
		b, err := frame.Encode("Cobra.Pack.Data", msg.PackData{ID: m.ID, Version: m.Version, SHA256: m.SHA256, Part: i + 1, Parts: parts, Data: base64.StdEncoding.EncodeToString(raw[i*chunk : end])})
		if err != nil {
			g.t.Errorf("encode chunk: %v", err)
			return
		}
		g.write(b)
	}
}

func (g *cmcpGame) with(f func(s *session.Server)) {
	g.mu.Lock()
	f(g.srv)
	g.mu.Unlock()
	g.flush()
}

func (g *cmcpGame) received(pkg string) (n int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, f := range g.got {
		if f.Package == pkg {
			n++
		}
	}
	return
}

func newCmcpTest(t *testing.T, cfg session.ServerConfig) (*Client, *vt.Screen, *cmcpDialer) {
	t.Helper()
	scr := vt.New(100, 32)
	d := &cmcpDialer{t: t, cfg: cfg}
	var c *Client
	d.c = &c
	var err error
	c, err = New(Config{Term: scr, HomeDir: t.TempDir(), NoRC: true, Quiet: true, Dialer: d, Interactive: true, NoUpdateCheck: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	return c, scr, d
}

func cmEventually(t *testing.T, what string, f func() bool) {
	t.Helper()
	for i := 0; i < 300; i++ {
		if f() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timeout: %s", what)
}

func testServerConfig() session.ServerConfig {
	return session.ServerConfig{Server: "Test", GameName: "T", MaxBody: 65536,
		Packages: []string{"Space.Power", "Space.Ship.Status"},
		Commands: []msg.CommandSpec{{Name: "hatch.toggle", Consoles: []string{"operations"}, Mutating: true, Text: "hatch", Args: map[string]msg.ArgSpec{}}}}
}

// Milestone (a): the handshake runs through gtf's gmcp handler and the core session, and
// /cmcp status shows it.
func TestCmcpHandshake(t *testing.T) {
	c, scr, d := newCmcpTest(t, testServerConfig())
	exec(c, "/connect localhost 4101")
	cmEventually(t, "server ready", func() bool { return d.gm() != nil && d.gm().srv.State() == session.SrvReady })
	if d.gm().received("Core.Supports.Set") != 1 || d.gm().received("Cobra.Hello") != 1 {
		t.Fatalf("frames: %+v", d.gm().got)
	}
	h := d.gm().srv.Hello()
	if h == nil || h.Render == nil || h.Render.Kind != "cell" || h.Client.Kind != "tty" {
		t.Fatalf("hello %+v", h)
	}
	cmEventually(t, "client ready", func() bool {
		var st session.State
		c.Do(func(*gtfparser.Interp) { st = c.fg.cm.UI().State })
		return st == session.StateReady
	})
	exec(c, "/cmcp status")
	waitScreen(t, c, scr, "CMCP: ready")
}

func (d *cmcpDialer) gm() *cmcpGame {
	d.gmu.Lock()
	defer d.gmu.Unlock()
	return d.gm0
}
