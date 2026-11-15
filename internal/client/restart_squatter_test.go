// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

//go:build darwin || dragonfly || freebsd || netbsd || openbsd || linux

package client

import (
	"context"
	"crypto/ed25519"
	"strings"
	"testing"
	"time"

	"github.com/itsnotyoutoday/gtf/internal/handoff"
	"github.com/itsnotyoutoday/gtf/internal/squatter/proto"
	"github.com/itsnotyoutoday/gtf/internal/squatter/wire"
	"github.com/itsnotyoutoday/gtf/internal/ui/vt"
	"github.com/itsnotyoutoday/gtfparser"
)

// newSqRig is a rig whose terminal is a sink: the old and the new client both draw on it, which the
// shared virtual screen does not allow under the race detector, and these tests look at the peers.
func newSqRig(t *testing.T) *rig {
	r := &rig{t: t, scr: vt.New(100, 30), d: newScriptDialer(t), home: t.TempDir(), term: newSink()}
	a, err := New(r.config())
	if err != nil {
		t.Fatal(err)
	}
	r.a = a
	t.Cleanup(func() {
		a.Close()
		if b := r.newClient(); b != nil {
			b.Close()
		}
	})
	return r
}

// squatterOn switches the squatter on in the rig's current client (loopback, an ephemeral port, its own directory).
func (r *rig) squatterOn() {
	r.t.Helper()
	c := r.cur()
	exec(c, "/set squatter_port=0", "/set squatter_bind=127.0.0.1", "/set squatter_dir="+r.home+"/sq", "/set squatter_name=gtftest", "/set squatter=on")
	c.Do(func(*gtfparser.Interp) { c.squatCheck() })
	eventually(r.t, "squatter listening", func() bool { return v(c, "squatter_state") == "listening" })
}

// stateOnly makes /restart carry the squatter's state but not its connections (the fallback behaviour).
func stateOnly(t *testing.T) {
	squatCarryConns.Store(false)
	t.Cleanup(func() { squatCarryConns.Store(true) })
}

func peerSession(t *testing.T, pc *wire.Client, name string) proto.SessionInfo {
	t.Helper()
	for _, s := range pc.Sessions() {
		if s.Name == name {
			return s
		}
	}
	t.Fatalf("no session %q in %+v", name, pc.Sessions())
	return proto.SessionInfo{}
}

func bg(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// redial connects the paired device again, to whichever client is current (no pairing code).
func (r *rig) redial(key ed25519.PrivateKey, name string) *wire.Client {
	r.t.Helper()
	pc, err := dialPeer(r.t, r.cur(), key, "", name)
	if err != nil {
		r.t.Fatalf("reconnect: %v", err)
	}
	r.t.Cleanup(func() { pc.Close() })
	return pc
}

func seqsOf(evs []wire.Event) []uint64 {
	var out []uint64
	for _, ev := range evs {
		switch e := ev.(type) {
		case wire.Backlog:
			for _, en := range e.Entries {
				if en.Line != nil {
					out = append(out, en.Line.Seq)
				}
			}
		case wire.Line:
			out = append(out, e.Seq)
		}
	}
	return out
}

// State only: the sessions keep their ids and their scrollback, a device that was paired before is
// not asked to pair again, the certificate is the same, and the client resumes exactly where it was.
func TestRestartSquatterStateSurvivesAndClientResumes(t *testing.T) {
	stateOnly(t)
	r := newSqRig(t)
	srv2 := r.connect("x", "4001")
	srv := r.connect("w", "4000") // the foreground world: its lines show on the screen
	r.squatterOn()
	a := r.a
	addr, fp := v(a, "squatter_addr"), v(a, "squatter_fingerprint")
	srv.Send([]byte("l1\r\nl2\r\nl3\r\n"))
	srv.Send([]byte{255, 251, 1}) // IAC WILL ECHO: a password prompt, the squatter's remoteEcho flag

	pc, key := pairPeer(t, a, "operator", "phone")
	idW, idX := peerSession(t, pc, "w").ID, peerSession(t, pc, "x").ID
	if err := pc.Attach(bg(t), idW, nil, 0); err != nil {
		t.Fatal(err)
	}
	readUntil(t, pc, func(l string) bool { return l == "game:l3" })
	srv.Send([]byte("l4\r\n"))
	readUntil(t, pc, func(l string) bool { return l == "game:l4" })
	last := pc.LastSeq(idW)
	// x ends: its (ended) session is still held
	srv2.Close()
	eventually(t, "x ended", func() bool {
		ss, err := pc.List(bg(t))
		if err != nil {
			return false
		}
		for _, s := range ss {
			if s.ID == idX && s.State == proto.StateEnded {
				return true
			}
		}
		return false
	})
	eventually(t, "echo flag", func() bool {
		ss, _ := pc.List(bg(t))
		for _, s := range ss {
			if s.ID == idW && s.RemoteEcho {
				return true
			}
		}
		return false
	})
	pc.Close()
	srv.Send([]byte("while away\r\n")) // arrives after the device left and before the restart
	eventually(t, "the line in the scrollback", func() bool {
		var n uint64
		a.Do(func(*gtfparser.Interp) { n = a.sq.sq.SessionLastSeq(idW) })
		return n == last+1
	})

	if err := r.restart(false); err != nil {
		t.Fatal(err)
	}
	b := r.newClient()
	if b == nil {
		t.Fatal("no new client")
	}
	eventually(t, "squatter listening again", func() bool { return v(b, "squatter_state") == "listening" })
	if got := v(b, "squatter_fingerprint"); got != fp || fp == "" {
		t.Fatalf("fingerprint %q -> %q", fp, got)
	}
	if got := v(b, "squatter_addr"); got != addr {
		t.Fatalf("with squatter_port=0 the port should be asked for again: %q -> %q", addr, got)
	}
	pc2 := r.redial(key, "phone") // same key, no code
	ss := map[string]proto.SessionInfo{}
	for _, s := range pc2.Sessions() {
		ss[s.ID] = s
	}
	if len(ss) != 2 || ss[idW].Name != "w" || ss[idX].Name != "x" {
		t.Fatalf("session ids changed: %+v", pc2.Sessions())
	}
	if ss[idX].State != proto.StateEnded || ss[idX].EndReason == "" {
		t.Fatalf("the ended session is not held as ended: %+v", ss[idX])
	}
	if !ss[idW].RemoteEcho || ss[idW].State != proto.StateConnected || ss[idW].LastSeq != last+1 {
		t.Fatalf("session w after restart: %+v (last was %d)", ss[idW], last)
	}
	if err := pc2.Attach(bg(t), idW, &last, 0); err != nil {
		t.Fatal(err)
	}
	evs := collect(t, pc2, func(ev wire.Event) bool { b, ok := ev.(wire.Backlog); return ok && b.Done })
	if g := peerLines(evs); len(g) != 1 || g[0] != "game:while away" {
		t.Fatalf("resume replays %v, want exactly the missed line", g)
	}
	if s := seqsOf(evs); len(s) != 1 || s[0] != last+1 {
		t.Fatalf("resume seqs %v, want [%d]", s, last+1)
	}
	// still one paired device; the carried connection works in both directions, numbers continue
	var devs int
	b.Do(func(*gtfparser.Interp) { d, _ := b.sq.sq.Devices(); devs = len(d) })
	if devs != 1 {
		t.Fatalf("%d paired devices after the restart", devs)
	}
	srv.Send([]byte("after restart\r\n"))
	evs = collect(t, pc2, func(ev wire.Event) bool { l, ok := ev.(wire.Line); return ok && plainOf(l) == "after restart" })
	if s := seqsOf(evs); s[len(s)-1] != last+2 {
		t.Fatalf("seqs %v, want to end at %d", s, last+2)
	}
	if err := pc2.Send(bg(t), idW, "hello"); err != nil {
		t.Fatal(err)
	}
	srv.Expect([]byte{255, 253, 1}, []byte("hello\r\n")) // the client's DO ECHO answer to the password prompt came first
	// the game connection that went away ended in the squatter's list and a new one gets its own session
	_ = srv2
}

func plainOf(l wire.Line) string {
	var sb strings.Builder
	for _, sp := range l.Spans {
		sb.WriteString(sp.S)
	}
	return sb.String()
}

func collect(t *testing.T, c *wire.Client, pred func(wire.Event) bool) []wire.Event {
	t.Helper()
	var out []wire.Event
	deadline := time.After(5 * time.Second)
	for {
		select {
		case ev := <-c.Events():
			out = append(out, ev)
			if pred(ev) {
				return out
			}
		case <-deadline:
			t.Fatalf("timed out; events so far: %#v", out)
		}
	}
}

// A damaged or too new squatter section is refused by the rehearsal: the old process keeps running
// with its squatter and its clients untouched.
func TestRestartSquatterBadSectionIsRefusedAndNothingChanges(t *testing.T) {
	stateOnly(t)
	r := newSqRig(t)
	srv := r.connect("w", "4000")
	r.squatterOn()
	pc, _ := pairPeer(t, r.a, "operator", "phone")
	id := peerSession(t, pc, "w").ID
	if err := pc.Attach(bg(t), id, nil, 0); err != nil {
		t.Fatal(err)
	}
	collect(t, pc, func(ev wire.Event) bool { b, ok := ev.(wire.Backlog); return ok && b.Done })
	for name, data := range map[string]string{
		"too new":    `{"active":true,"state":{"version":2,"sessions":[]}}`,
		"wrong type": `{"active":true,"state":"cut off"}`,
		"unknown id": `{"active":true,"state":{"version":1,"serial":1,"sessions":[]},"bind":[{"world":"w","id":"nope"}]}`,
	} {
		raw := data
		r.probe = func(req ProbeRequest) error {
			r.mutate(req.StatePath, func(f *handoff.File) {
				f.Sections[secSquatter] = handoff.Section{V: 1, Data: []byte(raw)}
			})
			return r.rehearse(req)
		}
		err := r.restart(false)
		if err == nil || r.newClient() != nil {
			t.Fatalf("%s: the restart went ahead: %v", name, err)
		}
		// the squatter and its client carry on as if nothing had happened
		srv.Send([]byte("still here " + name + "\r\n"))
		readUntil(t, pc, func(l string) bool { return l == "game:still here "+name })
	}
}

// A connection that cannot be carried (-f) is closed and made again by the new process: its old
// session ends (and stays held), the new connection gets a session of its own.
func TestRestartSquatterSessionOfAnUncarriedConnection(t *testing.T) {
	stateOnly(t)
	r, d := tlsSqRig(t, false)
	exec(r.a, "/addworld w localhost 4000", "/connect w")
	srv := d.server(1)
	r.squatterOn()
	pc, key := pairPeer(t, r.a, "operator", "phone")
	old := peerSession(t, pc, "w").ID
	srv.Write([]byte("hello\r\n"))
	if err := pc.Attach(bg(t), old, nil, 0); err != nil {
		t.Fatal(err)
	}
	readUntil(t, pc, func(l string) bool { return l == "game:hello" })
	if err := r.restart(true); err != nil {
		t.Fatal(err)
	}
	srv2 := d.server(2) // the new process connected the world again
	srv2.Write([]byte("fresh\r\n"))
	b := r.newClient()
	eventually(t, "squatter listening", func() bool { return v(b, "squatter_state") == "listening" })
	pc2 := r.redial(key, "phone")
	eventually(t, "two sessions, the old one ended", func() bool {
		ss, err := pc2.List(bg(t))
		if err != nil || len(ss) != 2 {
			return false
		}
		var ended, live proto.SessionInfo
		for _, s := range ss {
			if s.ID == old {
				ended = s
			} else {
				live = s
			}
		}
		return ended.State == proto.StateEnded && ended.EndReason == proto.EndSquatterRestarted &&
			live.ID != "" && live.ID != old && live.Name == "w" && live.State == proto.StateConnected
	})
}

func tlsSqRig(t *testing.T, migratable bool) (*rig, *tlsDialer) {
	r := newSqRig(t)
	d := newTLSDialer(t, migratable)
	r.a.cfg.Dialer = d
	r.dialer = d
	return r, d
}
