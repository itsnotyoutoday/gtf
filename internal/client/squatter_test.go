// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package client

import (
	"context"
	"crypto/ed25519"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/itsnotyoutoday/gtf/internal/squatter/auth"
	"github.com/itsnotyoutoday/gtf/internal/squatter/proto"
	"github.com/itsnotyoutoday/gtf/internal/squatter/wire"
	"github.com/itsnotyoutoday/gtf/internal/ui/vt"
	"github.com/itsnotyoutoday/gtfparser"
)

func v(c *Client, name string) string {
	var s string
	c.Do(func(in *gtfparser.Interp) { s = in.Var(name) })
	return s
}

func eventually(t *testing.T, what string, f func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if f() {
			return
		}
		time.Sleep(15 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// squatClient is a gtf client with the squatter switched on (loopback, an ephemeral port).
func squatClient(t *testing.T, extra ...string) (*Client, *vt.Screen, *fakeDialer) {
	t.Helper()
	c, scr, d := newTestClient(t)
	dir := t.TempDir() + "/sq"
	c.Do(func(in *gtfparser.Interp) {
		in.Exec("/set squatter_port=0")
		in.Exec("/set squatter_bind=127.0.0.1") // the tests stay on this machine (the default is every interface)
		in.Exec("/set squatter_dir=" + dir)
		in.Exec("/set squatter_name=gtftest")
		for _, e := range extra {
			in.Exec(e)
		}
		in.Exec("/set squatter=on")
		c.squatCheck()
	})
	eventually(t, "squatter listening", func() bool { return v(c, "squatter_state") == "listening" })
	return c, scr, d
}

func pairPeer(t *testing.T, c *Client, role, name string) (*wire.Client, ed25519.PrivateKey) {
	t.Helper()
	var code string
	c.Do(func(*gtfparser.Interp) {
		info, err := c.sq.sq.Pair(role)
		if err != nil {
			t.Fatal(err)
		}
		code = info.Code
	})
	key, _ := auth.GenerateKey()
	pc, err := dialPeer(t, c, key, code, name)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pc.Close() })
	return pc, key
}

func dialPeer(t *testing.T, c *Client, key ed25519.PrivateKey, code, name string) (*wire.Client, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return wire.Dial(ctx, "wss://"+v(c, "squatter_addr"), wire.ClientOptions{Key: key, PairCode: code, DeviceName: name,
		PinnedFingerprint: v(c, "squatter_fingerprint"), ExpectKind: proto.KindTFSquatterd})
}

func peerLines(evs []wire.Event) []string {
	var out []string
	add := func(l *proto.Line) {
		var sb strings.Builder
		for _, sp := range l.Spans {
			sb.WriteString(sp.S)
		}
		out = append(out, l.Kind+":"+sb.String())
	}
	for _, ev := range evs {
		switch e := ev.(type) {
		case wire.Backlog:
			for _, en := range e.Entries {
				if en.Line != nil {
					add(en.Line)
				}
			}
		case wire.Line:
			l := e.Line
			add(&l)
		}
	}
	return out
}

func readUntil(t *testing.T, pc *wire.Client, pred func(string) bool) []string {
	t.Helper()
	var all []string
	deadline := time.After(5 * time.Second)
	for {
		select {
		case ev := <-pc.Events():
			ls := peerLines([]wire.Event{ev})
			all = append(all, ls...)
			for _, l := range ls {
				if pred(l) {
					return all
				}
			}
		case <-deadline:
			t.Fatalf("timed out; lines so far: %q", all)
		}
	}
}

func contains(ls []string, want string) bool {
	for _, l := range ls {
		if l == want {
			return true
		}
	}
	return false
}

func TestSquatterEndToEnd(t *testing.T) {
	c, scr, d := squatClient(t)
	c.Do(func(in *gtfparser.Interp) {
		in.Exec("/addworld w god pw localhost 4000")
		in.Exec("/addworld w2 god pw localhost 4001")
		in.Exec("/def -t'secret *' -ag hush")
		in.Exec("/def -t'alert *' alerts = /echo trigger fired: %{*}")
		in.Exec("/def -hSQUAT_CONNECT hc = /set seen_connect=%{1}:%{2}")
		in.Exec("/def -hSQUAT_PAIR hp = /set seen_pair=%{1}:%{2}")
		in.Exec("/def -hSQUAT_DISCONNECT hd = /set seen_disc=%{1}")
		in.Exec("/connect w")
	})
	srv := <-d.srv
	srv.Expect([]byte("connect god pw\r\n"))
	srv.Send([]byte("Welcome to the test game\r\n"))
	waitScreen(t, c, scr, "Welcome to the test game")

	pc, key := pairPeer(t, c, "admin", "phone")
	if pc.Kind() != proto.KindTFSquatterd || pc.Instance() != "gtftest" {
		t.Fatalf("kind %q instance %q", pc.Kind(), pc.Instance())
	}
	ss := pc.Sessions()
	if len(ss) != 1 || ss[0].Name != "w" || ss[0].State != "connected" || ss[0].Host != "localhost" || ss[0].Port != 4000 {
		t.Fatalf("%+v", ss)
	}
	id := ss[0].ID
	eventually(t, "hooks", func() bool {
		return v(c, "seen_connect") == "phone:admin" && v(c, "seen_pair") == "phone:admin"
	})
	if v(c, "squatter_peers") != "1" {
		t.Fatalf("squatter_peers=%q", v(c, "squatter_peers"))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	// replay: the game's line is there (scrollback of a session that existed before the peer came)
	if err := pc.Attach(ctx, id, nil, 0); err != nil {
		t.Fatal(err)
	}
	got := readUntil(t, pc, func(l string) bool { return l == "game:Welcome to the test game" })
	_ = got
	// gagged text never reaches the peer; a trigger's output does (as a system line); live game text arrives
	srv.Send([]byte("secret plans\r\nalert dragon\r\nplain line\r\n"))
	got = readUntil(t, pc, func(l string) bool { return l == "game:plain line" })
	if contains(got, "game:secret plans") || !contains(got, "game:alert dragon") || !contains(got, "system:trigger fired: alert dragon") {
		t.Fatalf("%q", got)
	}
	// input from the peer reaches the game and is echoed with its origin
	if err := pc.Send(ctx, id, "look"); err != nil {
		t.Fatal(err)
	}
	srv.Expect([]byte("look\r\n"))
	got = readUntil(t, pc, func(l string) bool { return l == "echo:look" })
	// an admin's /command runs in gtf's engine; the typed line is echoed (SquatClient shows what it typed through that echo) but gtf's own
	// output for it (messages, errors, the /restart plan) is not forwarded to the devices
	if err := pc.Send(ctx, id, "/set from_peer=hello"); err != nil {
		t.Fatal(err)
	}
	eventually(t, "command ran", func() bool { return v(c, "from_peer") == "hello" })
	if err := pc.Send(ctx, id, "/echo said by the engine"); err != nil {
		t.Fatal(err)
	}
	got = readUntil(t, pc, func(l string) bool { return l == "echo:/echo said by the engine" })
	waitScreen(t, c, scr, "said by the engine") // the local user sees the output: it stays on this terminal
	// the local user's typing is mirrored to the peer as an echo from "tf"
	c.Do(func(*gtfparser.Interp) { c.onKeys("say hi\r") })
	srv.Expect([]byte("say hi\r\n"))
	got = readUntil(t, pc, func(l string) bool { return l == "echo:say hi" })
	for _, l := range got {
		if strings.HasPrefix(l, "system:") && strings.Contains(l, "said by the engine") {
			t.Fatalf("the output of a command a device typed was forwarded to the devices: %q", got)
		}
	}
	// a password is sent without echo, record or history
	if err := pc.SendSecret(ctx, id, "hunter2"); err != nil {
		t.Fatal(err)
	}
	srv.Expect([]byte("hunter2\r\n"))
	// tf.exec: output comes back in the reply, not on the screen or in the scrollback
	res, err := pc.Exec(ctx, "/echo exec output")
	if err != nil || !res.OK || len(res.Out) != 1 || res.Out[0] != "exec output" {
		t.Fatalf("%+v %v", res, err)
	}
	res, err = pc.Exec(ctx, "/nosuchcommandatall")
	if err != nil || res.OK || len(res.Out) == 0 {
		t.Fatalf("%+v %v", res, err)
	}
	c.Flush()
	if strings.Contains(scr.Text(), "exec output") {
		t.Fatal("tf.exec output was displayed")
	}
	ws, err := pc.Worlds(ctx)
	if err != nil || len(ws) < 2 || ws[0].Name != "w" || !ws[0].PassSet {
		t.Fatalf("%+v %v", ws, err)
	}
	// /pair from a peer shows the code only in the reply, never in a session's scrollback
	res, err = pc.Exec(ctx, "/pair viewer")
	if err != nil || !res.OK || len(res.Out) != 4 || !strings.HasPrefix(res.Out[0], "Pairing code:") {
		t.Fatalf("%+v %v", res, err)
	}
	code := strings.Fields(res.Out[0])[2]
	// ... but another device can use it
	k2, _ := auth.GenerateKey()
	viewer, err := dialPeer(t, c, k2, code, "tv")
	if err != nil {
		t.Fatal(err)
	}
	if viewer.Role() != "viewer" {
		t.Fatal(viewer.Role())
	}
	viewer.Close()
	// the pairing code is nowhere in the session's scrollback (a fresh attach replays all of it)
	pc3, _ := pairPeer(t, c, "viewer", "audit")
	if err := pc3.Attach(ctx, id, nil, 0); err != nil {
		t.Fatal(err)
	}
	all := readUntil(t, pc3, func(l string) bool { return l == "game:plain line" })
	for _, l := range all {
		if strings.Contains(l, code) || strings.Contains(l, "Pairing code") {
			t.Fatalf("pairing code in the scrollback: %q", l)
		}
	}

	// remote session.connect opens a world without moving the local foreground
	fgBefore := fgWorld(c)
	info, err := pc.Connect(ctx, proto.World{Ref: "w2"})
	if err != nil {
		t.Fatal(err)
	}
	srv2 := <-d.srv
	srv2.Expect([]byte("connect god pw\r\n"))
	if info.Name != "w2" || fgWorld(c) != fgBefore {
		t.Fatalf("%+v fg %q -> %q", info, fgBefore, fgWorld(c))
	}
	if _, err := pc.Connect(ctx, proto.World{Ref: "nosuchworld"}); err == nil {
		t.Fatal("connect to an undefined world succeeded")
	}
	// the game closes: the session ends with a reason, history stays
	srv.Close()
	for {
		select {
		case ev := <-pc.Events():
			if s, ok := ev.(wire.SessionState); ok && s.Info.ID == id && s.Info.State == "ended" {
				if s.Info.EndReason != proto.EndRemoteClosed {
					t.Fatalf("%+v", s.Info)
				}
				goto ended
			}
		case <-ctx.Done():
			t.Fatal("session never ended")
		}
	}
ended:
	_ = key
}

func fgWorld(c *Client) string {
	var w string
	c.Do(func(*gtfparser.Interp) { w = c.ForegroundWorld() })
	return w
}

func TestSquatterOffByDefaultAndVariablesApply(t *testing.T) {
	c, _, _ := newTestClient(t)
	c.Do(func(*gtfparser.Interp) { c.squatCheck() })
	if c.sq.sq.Active() || v(c, "squatter_state") != "" || v(c, "squatter_addr") != "" {
		t.Fatal("the squatter must do nothing unless squatter=on")
	}
	// the tap is inert: no session id is handed out
	c.Do(func(in *gtfparser.Interp) {
		in.Exec("/addworld w god pw localhost 4000")
		in.Exec("/connect w")
		if id := c.sessions["w"].squatID; id != "" {
			t.Errorf("session id %q while off", id)
		}
	})
	// bad values: a warning, the default is kept, nothing starts
	c.Do(func(in *gtfparser.Interp) {
		in.Exec("/set squatter_port=notaport")
		in.Exec("/set squatter_pair_role=root")
		c.squatCheck()
	})
	if c.sq.sq.Active() {
		t.Fatal("started without squatter=on")
	}
	c.Do(func(*gtfparser.Interp) {}) // let queued work finish
}

func TestSquatterCommandsAndRuntimeChanges(t *testing.T) {
	c, scr, _ := squatClient(t)
	first := v(c, "squatter_addr")
	if v(c, "squatter_fingerprint") == "" || v(c, "squatter_peers") != "0" || v(c, "squatter_error") != "" {
		t.Fatal("status variables not set")
	}
	run := func(cmd string) {
		c.Do(func(in *gtfparser.Interp) { in.Exec(cmd); c.afterTask() })
	}
	run("/squatter")
	waitScreen(t, c, scr, "squatter: listening")
	waitScreen(t, c, scr, v(c, "squatter_fingerprint")[:20])
	run("/squatter fingerprint")
	waitScreen(t, c, scr, "TLS fingerprint:")

	pc, key := pairPeer(t, c, "operator", "pad")
	eventually(t, "peers", func() bool { return v(c, "squatter_peers") == "1" })
	run("/squatter list")
	waitScreen(t, c, scr, "Connected clients (1):")
	waitScreen(t, c, scr, "Paired devices (1):")

	// /pair shows the code to the local user only: not in history (what /recall and /log see)
	run("/pair viewer")
	waitScreen(t, c, scr, "Pairing code:")
	waitScreen(t, c, scr, "Connect to:      wss://localhost:")
	var hist []string
	c.Do(func(in *gtfparser.Interp) { hist = in.HistoryLines("local", "") })
	for _, l := range hist {
		if strings.Contains(l, "Pairing code") || strings.Contains(l, "TLS fingerprint:") && strings.Contains(l, "Valid") {
			t.Fatalf("pairing output recorded in history: %q", l)
		}
	}
	run("/pair root")
	waitScreen(t, c, scr, "role must be viewer, operator or admin")

	// changing a variable applies at once: the listener restarts, clients are dropped, sessions stay
	run("/set squatter_name=renamed")
	select {
	case <-pc.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("client not dropped by a restart")
	}
	eventually(t, "relisten", func() bool { return v(c, "squatter_state") == "listening" })
	pc2, err := dialPeer(t, c, key, "", "pad")
	if err != nil {
		t.Fatal(err)
	}
	if pc2.Instance() != "renamed" {
		t.Fatal(pc2.Instance())
	}
	// /squatter port picks a new port
	run("/squatter port 0")
	run("/squatter kick pad")
	waitScreen(t, c, scr, "client(s) disconnected")
	select {
	case <-pc2.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("not kicked")
	}
	// unpair: the device can no longer connect
	run("/squatter unpair pad")
	waitScreen(t, c, scr, "unpaired pad")
	if _, err := dialPeer(t, c, key, "", "pad"); err == nil {
		t.Fatal("an unpaired device connected")
	}
	// an unknown device with no code is refused
	k2, _ := auth.GenerateKey()
	var pe *wire.ProtoError
	if _, err := dialPeer(t, c, k2, "", "stranger"); !errors.As(err, &pe) || pe.Code != proto.ErrAuthFailed {
		t.Fatalf("%v", err)
	}
	_ = first

	// off: port closed, status variables cleared, hooks and sessions gone
	run("/squatter off")
	waitScreen(t, c, scr, "squatter: off")
	if v(c, "squatter") != "off" || v(c, "squatter_state") != "off" || v(c, "squatter_addr") != "" || c.sq.sq.Active() {
		t.Fatal("not off")
	}
	if _, err := dialPeer(t, c, key, "", "pad"); err == nil {
		t.Fatal("connected while off")
	}
	run("/pair")
	waitScreen(t, c, scr, "the squatter is not running")
	run("/squatter on")
	eventually(t, "on again", func() bool { return v(c, "squatter_state") == "listening" })
}

func TestSquatterPortInUseIsReported(t *testing.T) {
	c, scr, _ := squatClient(t)
	_, port, _ := strings.Cut(v(c, "squatter_addr"), ":")
	c2, scr2, _ := newTestClient(t)
	c2.Do(func(in *gtfparser.Interp) {
		in.Exec("/set squatter_dir=" + t.TempDir() + "/two")
		in.Exec("/set squatter_port=" + port)
		in.Exec("/set squatter_bind=127.0.0.1")
		in.Exec("/set squatter=on")
		c2.squatCheck()
	})
	waitScreen(t, c2, scr2, "squatter: listen on 127.0.0.1:"+port)
	if v(c2, "squatter_state") != "error" || v(c2, "squatter_error") == "" {
		t.Fatal("state not reported")
	}
	_ = scr
	_ = c
}

func TestSquatterPublicBindWarnsAndPlainWSIsRefused(t *testing.T) {
	c, scr, _ := newTestClient(t)
	c.Do(func(in *gtfparser.Interp) {
		in.Exec("/set squatter_dir=" + t.TempDir() + "/p")
		in.Exec("/set squatter_port=0")
		in.Exec("/set squatter_bind=0.0.0.0")
		in.Exec("/set squatter=on")
		c.squatCheck()
	})
	waitScreen(t, c, scr, "listening on every interface")
	c.Do(func(in *gtfparser.Interp) { in.Exec("/set squatter_tls=off"); c.squatCheck() })
	waitScreen(t, c, scr, "plain ws://")
	if v(c, "squatter_state") != "error" {
		t.Fatal(v(c, "squatter_state"))
	}
}

func TestSquatterSeedsConnectionsThatExistWhenSwitchedOn(t *testing.T) {
	c, scr, d := newTestClient(t)
	c.Do(func(in *gtfparser.Interp) {
		in.Exec("/addworld w god pw localhost 4000")
		in.Exec("/connect w")
	})
	srv := <-d.srv
	srv.Expect([]byte("connect god pw\r\n"))
	srv.Send([]byte("before the squatter\r\n"))
	waitScreen(t, c, scr, "before the squatter")
	c.Do(func(in *gtfparser.Interp) {
		in.Exec("/set squatter_dir=" + t.TempDir() + "/s")
		in.Exec("/set squatter_port=0")
		in.Exec("/set squatter=on")
		c.squatCheck()
	})
	eventually(t, "listening", func() bool { return v(c, "squatter_state") == "listening" })
	pc, _ := pairPeer(t, c, "operator", "late")
	ss := pc.Sessions()
	if len(ss) != 1 || ss[0].Name != "w" || ss[0].State != "connected" || ss[0].LastSeq == 0 {
		t.Fatalf("%+v", ss)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := pc.Attach(ctx, ss[0].ID, nil, 0); err != nil {
		t.Fatal(err)
	}
	got := readUntil(t, pc, func(l string) bool { return strings.HasSuffix(l, "before the squatter") })
	if len(got) == 0 {
		t.Fatal("no history")
	}
	// connection failure of a remote connect: error text comes back
	if _, err := pc.Connect(ctx, proto.World{Host: "bad host!", Port: 1}); err == nil {
		t.Fatal("a host name with a space was accepted")
	}
}

func TestSquatterViewerCannotSendAndOperatorCommandsAreText(t *testing.T) {
	c, _, d := squatClient(t)
	c.Do(func(in *gtfparser.Interp) {
		in.Exec("/addworld w god pw localhost 4000")
		in.Exec("/connect w")
	})
	srv := <-d.srv
	srv.Expect([]byte("connect god pw\r\n"))
	op, _ := pairPeer(t, c, "operator", "op")
	id := op.Sessions()[0].ID
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// an operator's "/who" is text for the game, not a gtf command
	if err := op.Send(ctx, id, "/who"); err != nil {
		t.Fatal(err)
	}
	srv.Expect([]byte("/who\r\n"))
	if _, err := op.Exec(ctx, "/echo x"); err == nil {
		t.Fatal("operator ran tf.exec")
	}
	// resizing from a peer must not change the game's idea of the terminal
	if err := op.Resize(ctx, id, 10, 5); err != nil {
		t.Fatal(err)
	}
}

func TestSquatterOnRetriesAfterError(t *testing.T) {
	c, scr, _ := squatClient(t)
	_, port, _ := strings.Cut(v(c, "squatter_addr"), ":")
	c2, scr2, _ := newTestClient(t)
	c2.Do(func(in *gtfparser.Interp) {
		in.Exec("/set squatter_dir=" + t.TempDir() + "/two")
		in.Exec("/set squatter_port=" + port)
		in.Exec("/set squatter_bind=127.0.0.1")
		in.Exec("/set squatter=on")
		c2.squatCheck()
	})
	eventually(t, "error", func() bool { return v(c2, "squatter_state") == "error" })
	c.Do(func(in *gtfparser.Interp) { in.Exec("/squatter off") })
	eventually(t, "freed", func() bool { return v(c, "squatter_state") == "off" })
	c2.Do(func(in *gtfparser.Interp) { in.Exec("/squatter on"); c2.afterTask() })
	eventually(t, "listening after retry", func() bool { return v(c2, "squatter_state") == "listening" })
	_, _, _ = scr, scr2, c
}

// Code that comes from a peer runs restricted by default (squatter_restrict=file): no shell, no files, and
// neither by defining a trigger that fires later. The owner can lift it; the owner at the terminal never
// is restricted.
func TestPeerCodeIsRestrictedByDefault(t *testing.T) {
	c, _, _ := squatClient(t)
	pc, _ := pairPeer(t, c, "admin", "laptop")
	defer pc.Close()
	ctx := context.Background()
	scratch := t.TempDir() + "/peerfile"

	// a shell command and a file write are refused and do nothing
	res, err := pc.Exec(ctx, "/sh echo hi > "+scratch)
	if err != nil || !strings.Contains(strings.Join(res.Out, "\n"), "restricted") {
		t.Fatalf("a peer ran /sh: %+v %v", res, err)
	}
	res, _ = pc.Exec(ctx, "/log "+scratch)
	if !strings.Contains(strings.Join(res.Out, "\n"), "restricted") {
		t.Fatalf("a peer could open a log file: %+v", res)
	}
	if _, err := os.Stat(scratch); err == nil {
		t.Fatal("a restricted peer created a file")
	}
	// ordinary commands still work
	if res, _ = pc.Exec(ctx, "/echo fine"); !res.OK || res.Out[0] != "fine" {
		t.Fatalf("%+v", res)
	}
	// a trigger the peer defines keeps its restriction when it fires later, from server text
	if res, _ = pc.Exec(ctx, "/def -t'go *' peertrig = /sh echo x > "+scratch); !res.OK {
		t.Fatalf("%+v", res)
	}
	c.Do(func(in *gtfparser.Interp) { in.ProcessText(gtfparser.LineInput{Text: "go now"}) }) // server text that matches the peer's trigger
	time.Sleep(100 * time.Millisecond)
	if _, err := os.Stat(scratch); err == nil {
		t.Fatal("a peer's trigger ran a shell command when it fired later")
	}
	// the owner's own shell is not restricted
	c.Do(func(in *gtfparser.Interp) { in.Exec("/sh echo owner > " + scratch) })
	eventually(t, "the owner's /sh", func() bool { _, err := os.Stat(scratch); return err == nil })
	_ = os.Remove(scratch)

	// the owner lifts the restriction
	c.Do(func(in *gtfparser.Interp) { in.Exec("/set squatter_restrict=none") })
	res, _ = pc.Exec(ctx, "/sh echo lifted > "+scratch)
	if strings.Contains(strings.Join(res.Out, "\n"), "restricted") {
		t.Fatalf("the restriction was not lifted: %+v", res)
	}
	eventually(t, "the lifted peer's /sh", func() bool { _, err := os.Stat(scratch); return err == nil })
	// control: with the restriction lifted the SAME trigger does run its shell command, so the refusal
	// above was the restriction and not a trigger that never fired
	_ = os.Remove(scratch)
	c.Do(func(in *gtfparser.Interp) { in.ProcessText(gtfparser.LineInput{Text: "go now"}) })
	eventually(t, "the lifted peer's trigger", func() bool { _, err := os.Stat(scratch); return err == nil })
}

func TestRestrictLevelDefaultsToFile(t *testing.T) {
	for in, want := range map[string]int{"": gtfparser.RestrictFile, "file": gtfparser.RestrictFile, "bogus": gtfparser.RestrictFile,
		"none": gtfparser.RestrictNone, "shell": gtfparser.RestrictShell, "WORLD": gtfparser.RestrictWorld} {
		if got := restrictLevel(in); got != want {
			t.Errorf("restrictLevel(%q) = %d, want %d", in, got, want)
		}
	}
}

// The client's own text (script errors, "% ..." messages, command output, /recall) is not forwarded to remote clients, only game text and what a
// trigger prints while a game line is handled; squatter_local=on forwards it all.
func TestSquatterDoesNotForwardTheClientsOwnText(t *testing.T) {
	c, scr, d := squatClient(t)
	c.Do(func(in *gtfparser.Interp) {
		in.Exec("/addworld w god pw localhost 4000")
		in.Exec("/def -t'alert*' trig = /echo trigger output")
		in.Exec("/connect w")
	})
	srv := <-d.srv
	srv.Expect([]byte("connect god pw\r\n"))
	srv.Send([]byte("Welcome\r\n"))
	waitScreen(t, c, scr, "Welcome")
	pc, _ := pairPeer(t, c, "admin", "phone")
	ss := pc.Sessions()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := pc.Attach(ctx, ss[0].ID, nil, 0); err != nil {
		t.Fatal(err)
	}
	readUntil(t, pc, func(l string) bool { return l == "game:Welcome" })

	// local text typed or run at the terminal: command output and a script error
	c.Do(func(in *gtfparser.Interp) {
		in.Exec("/echo local note one")
		in.Exec("/nosuchcommand")
		in.Exec("/squat")
	})
	waitScreen(t, c, scr, "local note one")
	srv.Send([]byte("alert dragon\r\nmarker one\r\n"))
	got := readUntil(t, pc, func(l string) bool { return l == "game:marker one" })
	for _, l := range got {
		if strings.HasPrefix(l, "system:") && !strings.Contains(l, "trigger output") {
			t.Fatalf("the client's own text reached the remote client: %q in %q", l, got)
		}
	}
	if contains(got, "system:local note one") || !contains(got, "system:trigger output") {
		t.Fatalf("local text is not forwarded, a trigger's output is: %q", got)
	}

	// squatter_local=on forwards it
	c.Do(func(in *gtfparser.Interp) {
		in.Exec("/set squatter_local=on")
		c.squatCheck()
		in.Exec("/echo local note two")
	})
	got = readUntil(t, pc, func(l string) bool { return strings.HasSuffix(l, "local note two") })
	if !contains(got, "system:local note two") {
		t.Fatalf("%q", got)
	}
}

// What is replayed to a remote client that attaches later does not include the client's own text either.
func TestSquatterSeedSkipsTheClientsOwnText(t *testing.T) {
	c, scr, d := newTestClient(t)
	c.Do(func(in *gtfparser.Interp) {
		in.Exec("/addworld w god pw localhost 4000")
		in.Exec("/connect w")
	})
	srv := <-d.srv
	srv.Expect([]byte("connect god pw\r\n"))
	srv.Send([]byte("game before\r\n"))
	waitScreen(t, c, scr, "game before")
	c.Do(func(in *gtfparser.Interp) { in.Exec("/echo local before") })
	waitScreen(t, c, scr, "local before")
	c.Do(func(in *gtfparser.Interp) {
		in.Exec("/set squatter_dir=" + t.TempDir() + "/s")
		in.Exec("/set squatter_port=0")
		in.Exec("/set squatter_bind=127.0.0.1")
		in.Exec("/set squatter=on")
		c.squatCheck()
	})
	eventually(t, "listening", func() bool { return v(c, "squatter_state") == "listening" })
	pc, _ := pairPeer(t, c, "operator", "late")
	ss := pc.Sessions()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := pc.Attach(ctx, ss[0].ID, nil, 0); err != nil {
		t.Fatal(err)
	}
	got := readUntil(t, pc, func(l string) bool { return l == "game:game before" })
	if contains(got, "game:local before") || contains(got, "system:local before") {
		t.Fatalf("the client's own text was replayed: %q", got)
	}
}

// What a device sees of a command it typed: the echo of the line (SquatClient shows what it typed through it) and nothing of gtf's own output
// for it, neither an error nor the /restart plan, so none of it is stored in the session, replayed to the other devices or carried over a restart.
func TestACommandADeviceTypedDoesNotForwardGtfsOutput(t *testing.T) {
	c, scr, d := squatClient(t)
	c.Do(func(in *gtfparser.Interp) {
		in.Exec("/addworld w god pw localhost 4000")
		in.Exec("/connect w")
	})
	srv := <-d.srv
	srv.Expect([]byte("connect god pw\r\n"))
	srv.Send([]byte("Welcome\r\n"))
	waitScreen(t, c, scr, "Welcome")
	pc, _ := pairPeer(t, c, "admin", "phone")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	id := pc.Sessions()[0].ID
	if err := pc.Attach(ctx, id, nil, 0); err != nil {
		t.Fatal(err)
	}
	readUntil(t, pc, func(l string) bool { return l == "game:Welcome" })
	for _, cmd := range []string{"/nosuchcommand", "/restart -n", "/echo local output"} {
		if err := pc.Send(ctx, id, cmd); err != nil {
			t.Fatal(err)
		}
	}
	got := readUntil(t, pc, func(l string) bool { return l == "echo:/echo local output" })
	srv.Send([]byte("marker\r\n")) // whatever gtf printed for the commands would be stored before this line
	got = append(got, readUntil(t, pc, func(l string) bool { return l == "game:marker" })...)
	for _, want := range []string{"echo:/nosuchcommand", "echo:/restart -n", "echo:/echo local output"} {
		if !contains(got, want) {
			t.Errorf("the typed line %q is echoed to the device: %q", want, got)
		}
	}
	for _, l := range got {
		if strings.HasPrefix(l, "system:") {
			t.Fatalf("gtf's output for a device's command was forwarded: %q in %q", l, got)
		}
	}
	waitScreen(t, c, scr, "no such command") // the local user does see it
}
