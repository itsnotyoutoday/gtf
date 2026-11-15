// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package squatter

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/itsnotyoutoday/gtf/internal/squatter/proto"
	"github.com/itsnotyoutoday/gtf/internal/squatter/scrollback"
	"github.com/itsnotyoutoday/gtf/internal/squatter/wire"
)

// populated returns a rig with two sessions that have every kind of state the export carries.
func populated(t *testing.T) (*rig, string, string) {
	t.Helper()
	r := newRig(t, nil)
	a, b := r.open("alpha"), r.open("beta")
	for _, w := range []string{"one", "two", "three"} {
		r.line(a, proto.KindGame, w)
	}
	r.line(a, proto.KindEcho, "look")
	r.line(b, proto.KindSystem, "system")
	r.h.Do(func() {
		r.sq.Gmcp(a, "Room.Info", `{"name":"x"}`)
		r.sq.Msdp(a, "HEALTH", "100")
		r.sq.SetRemoteEcho(a, true)
		r.sq.SessionEnded(b, proto.EndRemoteClosed, "bye")
		r.sq.session(a).ack("dev1", 2)
		r.sq.nsent[a] = 100
	})
	return r, a, b
}

func fresh(t *testing.T) *rig {
	t.Helper()
	return newRig(t, func(c *Config) { c.Enabled = false })
}

func TestStateRoundTripKeepsEverything(t *testing.T) {
	r, a, b := populated(t)
	blob, err := r.sq.Export()
	if err != nil {
		t.Fatal(err)
	}
	st, err := DecodeState(blob)
	if err != nil {
		t.Fatal(err)
	}
	if got := st.SessionIDs(); !reflect.DeepEqual(got, []string{a, b}) {
		t.Fatalf("ids %v", got)
	}
	if w := st.Worlds(); w[a] != "alpha" || w[b] != "beta" {
		t.Fatalf("worlds %v", w)
	}
	n := fresh(t)
	n.sq.Activate()
	if err := n.sq.ImportState(st, 500); err != nil {
		t.Fatal(err)
	}
	blob2, err := n.sq.Export()
	if err != nil {
		t.Fatal(err)
	}
	var e1, e2 exported
	_ = json.Unmarshal(blob, &e1)
	_ = json.Unmarshal(blob2, &e2)
	e1.Port, e2.Port = 0, 0 // the importing squatter is not listening
	if !reflect.DeepEqual(e1, e2) {
		t.Fatalf("export of the imported state differs:\n%s\n%s", blob, blob2)
	}
	ss := n.sq.session(a)
	if ss == nil || !ss.remoteEcho || ss.acks["dev1"] != 2 || ss.lastText != "three" || len(ss.gmcpLatest) != 1 || len(ss.msdpLatest) != 1 {
		t.Fatalf("session a after import: %+v", ss)
	}
	if first, last := ss.store.Bounds(); first != 1 || last != 6 {
		t.Fatalf("bounds %d..%d want 1..6 (lines, echo, gmcp, msdp)", first, last)
	}
	if sb := n.sq.session(b); sb == nil || sb.state != proto.StateEnded || sb.endReason != proto.EndRemoteClosed || sb.endDetail != "bye" {
		t.Fatalf("session b after import: %+v", sb)
	}
	if n.sq.nsent[a] != 100 {
		t.Fatalf("the width told to the game was lost: %v", n.sq.nsent)
	}
	// a new session after the import does not reuse an id
	var c string
	n.h.Do(func() { c = n.sq.SessionOpened(SessionMeta{World: "gamma"}) })
	if c == a || c == b || c == "" {
		t.Fatalf("new id %q collides", c)
	}
	// and a new line continues the sequence
	n.line(a, proto.KindGame, "four")
	if _, last := n.sq.session(a).store.Bounds(); last != 7 {
		t.Fatalf("last seq %d want 7", last)
	}
}

func TestStateRefusesNewerTruncatedAndCorrupt(t *testing.T) {
	r, _, _ := populated(t)
	blob, err := r.sq.Export()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeState(blob); err != nil {
		t.Fatal(err)
	}
	// a newer format is refused with its own error, an older one (no optional members) is read
	newer := strings.Replace(string(blob), `"version":1`, `"version":2`, 1)
	if _, err := DecodeState([]byte(newer)); !errors.Is(err, ErrStateTooNew) {
		t.Fatalf("newer version: %v", err)
	}
	if _, err := DecodeState([]byte(`{"version":1,"serial":3,"sessions":[]}`)); err != nil {
		t.Fatalf("minimal v1: %v", err)
	}
	for _, bad := range []string{``, `{`, `[]`, `null`, `{"version":0}`, `{"sessions":[]}`, `{"version":-1}`, `{"version":"1"}`} {
		if _, err := DecodeState([]byte(bad)); !errors.Is(err, ErrStateCorrupt) {
			t.Errorf("%q: %v, want ErrStateCorrupt", bad, err)
		}
	}
	// truncated at every position of a small state and at many of the real one: never accepted
	small := `{"version":1,"serial":3,"sessions":[{"id":"g1","meta":{"World":"w","State":"connected"},"entries":[{"v":1,"t":"line","session":"g1","seq":1,"ts":5,"kind":"game","spans":[{"s":"x"}]}]}]}`
	if _, err := DecodeState([]byte(small)); err != nil {
		t.Fatalf("small state: %v", err)
	}
	for i := 0; i < len(small); i++ {
		if _, err := DecodeState([]byte(small[:i])); !errors.Is(err, ErrStateCorrupt) {
			t.Fatalf("truncated to %d bytes: %v", i, err)
		}
	}
	for i := 0; i < len(blob); i += max(1, len(blob)/97) {
		if _, err := DecodeState(blob[:i]); err == nil {
			t.Fatalf("truncated to %d of %d bytes was accepted", i, len(blob))
		}
	}
	// well-formed JSON that makes no sense
	for name, mod := range map[string]func(*exported){
		"repeated id":   func(e *exported) { e.Sessions[1].ID = e.Sessions[0].ID },
		"empty id":      func(e *exported) { e.Sessions[0].ID = "" },
		"unknown state": func(e *exported) { e.Sessions[0].Meta.State = "bogus" },
		"scrollback gap": func(e *exported) {
			e.Sessions[0].Entries = append(e.Sessions[0].Entries[:1], e.Sessions[0].Entries[2:]...)
		},
		"seq zero": func(e *exported) { e.Sessions[0].Entries = []scrollback.Entry{{}} },
	} {
		var e exported
		_ = json.Unmarshal(blob, &e)
		mod(&e)
		b, _ := json.Marshal(e)
		if _, err := DecodeState(b); !errors.Is(err, ErrStateCorrupt) {
			t.Errorf("%s: %v, want ErrStateCorrupt", name, err)
		}
	}
}

func TestImportIsAllOrNothingAndOnlyIntoAnEmptySquatter(t *testing.T) {
	r, a, _ := populated(t)
	blob, _ := r.sq.Export()
	st, err := DecodeState(blob)
	if err != nil {
		t.Fatal(err)
	}
	// a squatter that already holds a session refuses and keeps what it has
	n := fresh(t)
	n.sq.Activate()
	var own string
	n.h.Do(func() { own = n.sq.SessionOpened(SessionMeta{World: "own"}) })
	if err := n.sq.ImportState(st, 100); err == nil {
		t.Fatal("import into a squatter with sessions succeeded")
	}
	if n.sq.NumSessions() != 1 || n.sq.session(own) == nil {
		t.Fatalf("a refused import changed the squatter: %d sessions", n.sq.NumSessions())
	}
	// a damaged scrollback in the second session leaves nothing imported from the first
	var e exported
	_ = json.Unmarshal(blob, &e)
	e.Sessions[1].Entries = []scrollback.Entry{e.Sessions[0].Entries[0], e.Sessions[0].Entries[2]} // seeded, not contiguous (DecodeState is skipped on purpose)
	m := fresh(t)
	m.sq.Activate()
	if err := m.sq.ImportState(&State{ex: e}, 100); err == nil {
		t.Fatal("a non-contiguous scrollback was imported")
	}
	if m.sq.NumSessions() != 0 || m.sq.session(a) != nil {
		t.Fatalf("partial import: %d sessions", m.sq.NumSessions())
	}
	// the ring is the configured size: a smaller one keeps the newest entries and their numbers
	k := fresh(t)
	k.sq.Activate()
	if err := k.sq.ImportState(st, 2); err != nil {
		t.Fatal(err)
	}
	if first, last := k.sq.session(a).store.Bounds(); last != 6 || first != 5 {
		t.Fatalf("ring 2: bounds %d..%d want 5..6", first, last)
	}
}

// The whole point: a client that reconnects to the restarted squatter (same directory: same paired
// devices, same certificate) resumes from its last sequence number with no gap and no duplicate.
func TestReconnectAfterRestartResumesExactly(t *testing.T) {
	dir := t.TempDir() + "/sq"
	r1 := newRig(t, func(c *Config) { c.Dir = dir })
	id := r1.open("mud")
	for _, w := range []string{"l1", "l2", "l3"} {
		r1.line(id, proto.KindGame, w)
	}
	pc, key := r1.paired("operator", "phone")
	ctx := tctx(t)
	if err := pc.Attach(ctx, id, nil, 0); err != nil {
		t.Fatal(err)
	}
	if g := linesOf(collect(t, pc, isBacklogDone)); len(g) != 3 {
		t.Fatalf("first replay %v", g)
	}
	r1.line(id, proto.KindGame, "l4")
	collect(t, pc, func(ev wire.Event) bool { l, ok := ev.(wire.Line); return ok && plain(l.Spans) == "l4" })
	last := pc.LastSeq(id)
	fp1 := r1.sq.Fingerprint()
	r1.line(id, proto.KindGame, "l5") // the client has not seen this one when the old process goes away
	blob, err := r1.sq.Export()
	if err != nil {
		t.Fatal(err)
	}
	pc.Close()
	r1.sq.Stop()

	// the "new process": a fresh squatter over the same directory, state imported before the first Apply
	h := newFakeHost(t)
	sq := New(h, Options{Version: "test", Home: t.TempDir()})
	h.sq = sq
	st, err := DecodeState(blob)
	if err != nil {
		t.Fatal(err)
	}
	sq.Activate()
	if err := sq.ImportState(st, 1000); err != nil {
		t.Fatal(err)
	}
	cfg := r1.cfg
	r2 := &rig{t: t, h: h, sq: sq}
	r2.apply(cfg)
	t.Cleanup(sq.Stop)
	if r2.sq.Fingerprint() != fp1 || fp1 == "" {
		t.Fatalf("fingerprint %q -> %q", fp1, r2.sq.Fingerprint())
	}
	// the paired key works with no pairing code; the session list shows the same id and the numbers
	pc2, err := r2.dial(key, "", "phone")
	if err != nil {
		t.Fatalf("reconnect without pairing: %v", err)
	}
	defer pc2.Close()
	ss := pc2.Sessions()
	if len(ss) != 1 || ss[0].ID != id || ss[0].LastSeq != 5 || ss[0].State != "connected" {
		t.Fatalf("sessions after restart: %+v", ss)
	}
	if err := pc2.Attach(ctx, id, &last, 0); err != nil {
		t.Fatal(err)
	}
	evs := collect(t, pc2, isBacklogDone)
	if g := linesOf(evs); len(g) != 1 || g[0] != "game:l5" {
		t.Fatalf("resume replays %v, want only the line that was missed", g)
	}
	for _, ev := range evs {
		if a, ok := ev.(wire.Attached); ok && (a.Gap != nil || a.ReplayFrom != last+1) {
			t.Fatalf("attached %+v", a)
		}
	}
	r2.line(id, proto.KindGame, "l6")
	collect(t, pc2, func(ev wire.Event) bool { l, ok := ev.(wire.Line); return ok && plain(l.Spans) == "l6" && l.Seq == 6 })
}

func TestHintedPortIsReboundWhenNothingIsInherited(t *testing.T) {
	r1 := newRig(t, nil)
	_, port, _ := splitAddr(t, r1.sq.Addr())
	blob, _ := r1.sq.Export()
	st, err := DecodeState(blob)
	if err != nil {
		t.Fatal(err)
	}
	r1.sq.Stop()
	h := newFakeHost(t)
	sq := New(h, Options{Version: "test", Home: t.TempDir()})
	h.sq = sq
	sq.Activate()
	if err := sq.ImportState(st, 100); err != nil {
		t.Fatal(err)
	}
	r2 := &rig{t: t, h: h, sq: sq}
	r2.apply(r1.cfg)
	t.Cleanup(sq.Stop)
	if _, p2, _ := splitAddr(t, sq.Addr()); p2 != port {
		t.Fatalf("port %s -> %s with squatter_port=0", port, p2)
	}
}

var _ = wire.Simple

func splitAddr(t *testing.T, a string) (string, string, error) {
	t.Helper()
	i := strings.LastIndex(a, ":")
	if i < 0 {
		t.Fatalf("bad address %q", a)
	}
	return a[:i], a[i+1:], nil
}

func TestDecodeConnsRefusesNewerTruncatedAndNonsense(t *testing.T) {
	good := ConnsState{V: 1, Listener: 3, TLS: true, Port: 4000, Peers: []PeerState{
		{ClientState: wire.ClientState{V: 1, ID: 1, Device: "a", Name: "A", Role: "operator", Buf: []byte("abc"), Out: [][]byte{[]byte("x")}}, Kind: "tls", Slot: 4, TLS: []byte("blob")},
		{ClientState: wire.ClientState{V: 1, ID: 2, Device: "b", Name: "B"}, Kind: "tcp", Slot: 5},
	}}
	blob, _ := json.Marshal(good)
	cs, err := DecodeConns(blob)
	if err != nil || len(cs.Peers) != 2 || string(cs.Peers[0].Buf) != "abc" || string(cs.Peers[0].TLS) != "blob" {
		t.Fatalf("%v %+v", err, cs)
	}
	for i := 0; i < len(blob); i += max(1, len(blob)/61) {
		if _, err := DecodeConns(blob[:i]); err == nil {
			t.Fatalf("truncated to %d of %d bytes was accepted", i, len(blob))
		}
	}
	if _, err := DecodeConns([]byte(strings.Replace(string(blob), `"v":1,"listener"`, `"v":2,"listener"`, 1))); !errors.Is(err, ErrStateTooNew) {
		t.Fatalf("newer: %v", err)
	}
	for name, mod := range map[string]func(*ConnsState){
		"no version":      func(c *ConnsState) { c.V = 0 },
		"repeated id":     func(c *ConnsState) { c.Peers[1].ID = 1 },
		"repeated slot":   func(c *ConnsState) { c.Peers[1].Slot = 4 },
		"listener slot":   func(c *ConnsState) { c.Peers[1].Slot = 3 },
		"negative slot":   func(c *ConnsState) { c.Peers[1].Slot = -1 },
		"unknown kind":    func(c *ConnsState) { c.Peers[1].Kind = "udp" },
		"tls without key": func(c *ConnsState) { c.Peers[0].TLS = nil },
		"tcp with key":    func(c *ConnsState) { c.Peers[1].TLS = []byte("k") },
		"client version":  func(c *ConnsState) { c.Peers[1].V = 9 },
		"client no id":    func(c *ConnsState) { c.Peers[1].ID = 0 },
	} {
		c := good
		c.Peers = append([]PeerState(nil), good.Peers...)
		mod(&c)
		b, _ := json.Marshal(c)
		if _, err := DecodeConns(b); !errors.Is(err, ErrStateCorrupt) && !errors.Is(err, ErrStateTooNew) {
			t.Errorf("%s: %v", name, err)
		}
	}
}
