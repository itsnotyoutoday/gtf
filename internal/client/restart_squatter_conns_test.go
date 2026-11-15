// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

//go:build darwin || dragonfly || freebsd || netbsd || openbsd || linux

package client

import (
	"crypto/ed25519"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/itsnotyoutoday/gtf/internal/handoff"
	"github.com/itsnotyoutoday/gtf/internal/squatter/auth"
	"github.com/itsnotyoutoday/gtf/internal/squatter/proto"
	"github.com/itsnotyoutoday/gtf/internal/squatter/wire"
	"github.com/itsnotyoutoday/gtfparser"
)

// tape records, in order, every line and sequence number a peer receives, across the whole test.
type tape struct {
	mu    sync.Mutex
	seqs  []uint64
	lines []string // kind:text
}

func newTape(pc *wire.Client) *tape {
	tp := &tape{}
	go func() {
		for {
			select {
			case ev := <-pc.Events():
				tp.add(ev)
			case <-pc.Done():
				return
			}
		}
	}()
	return tp
}

func (tp *tape) add(ev wire.Event) {
	tp.mu.Lock()
	defer tp.mu.Unlock()
	one := func(l *proto.Line) {
		var sb strings.Builder
		for _, sp := range l.Spans {
			sb.WriteString(sp.S)
		}
		tp.seqs = append(tp.seqs, l.Seq)
		tp.lines = append(tp.lines, l.Kind+":"+sb.String())
	}
	switch e := ev.(type) {
	case wire.Backlog:
		for _, en := range e.Entries {
			if en.Line != nil {
				one(en.Line)
			}
		}
	case wire.Line:
		l := e.Line
		one(&l)
	}
}

func (tp *tape) has(line string) bool {
	tp.mu.Lock()
	defer tp.mu.Unlock()
	for _, l := range tp.lines {
		if l == line {
			return true
		}
	}
	return false
}

func (tp *tape) snapshot() ([]uint64, []string) {
	tp.mu.Lock()
	defer tp.mu.Unlock()
	return append([]uint64(nil), tp.seqs...), append([]string(nil), tp.lines...)
}

// consecutive checks that the sequence numbers received are n, n+1, n+2...: no gap, no duplicate.
func consecutive(t *testing.T, seqs []uint64) {
	t.Helper()
	for i := 1; i < len(seqs); i++ {
		if seqs[i] != seqs[i-1]+1 {
			t.Fatalf("sequence numbers are not consecutive at %d: ...%v", i, seqs[max(0, i-3):min(len(seqs), i+3)])
		}
	}
}

// withPrefix returns the texts of the lines of kind p whose text starts with start.
func withPrefix(lines []string, p, start string) []string {
	var out []string
	for _, l := range lines {
		if t, ok := strings.CutPrefix(l, p); ok && strings.HasPrefix(t, start) {
			out = append(out, t)
		}
	}
	return out
}

// pairPeerOn pairs a device with the squatter of c, over TLS or (squatter_tls=off) plain ws.
func pairPeerOn(t *testing.T, c *Client, tlsOn bool, name string, tune func(*tls.Config)) (*wire.Client, ed25519.PrivateKey) {
	t.Helper()
	var code string
	c.Do(func(*gtfparser.Interp) {
		info, err := c.sq.sq.Pair("operator")
		if err != nil {
			t.Fatal(err)
		}
		code = info.Code
	})
	key, _ := auth.GenerateKey()
	pc, err := dialPeerOn(t, c, tlsOn, key, code, name, tune)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pc.Close() })
	return pc, key
}

func dialPeerOn(t *testing.T, c *Client, tlsOn bool, key ed25519.PrivateKey, code, name string, tune func(*tls.Config)) (*wire.Client, error) {
	t.Helper()
	opts := wire.ClientOptions{Key: key, PairCode: code, DeviceName: name, ExpectKind: proto.KindTFSquatterd, TLSTune: tune}
	scheme := "ws://"
	if tlsOn {
		scheme = "wss://"
		opts.PinnedFingerprint = v(c, "squatter_fingerprint")
	} else {
		opts.InsecureLoopback = true
	}
	return wire.Dial(bg(t), scheme+v(c, "squatter_addr"), opts)
}

func (r *rig) squatterOnPlain(tlsOn bool) {
	r.t.Helper()
	if !tlsOn {
		exec(r.cur(), "/set squatter_tls=off")
	}
	r.squatterOn()
}

func peerCount(c *Client) int {
	var n int
	c.Do(func(*gtfparser.Interp) { n = c.sq.sq.NumPeers() })
	return n
}

// The point of phase two: a client that is connected when gtf restarts stays connected. Traffic runs
// in both directions while the restart happens; nothing is lost, repeated or reordered, the sequence
// numbers go on without a gap, and the same connection keeps working afterwards.
func TestRestartKeepsSquatterClientsConnected(t *testing.T) {
	for _, tlsOn := range []bool{true, false} {
		name := map[bool]string{true: "tls", false: "plain"}[tlsOn]
		t.Run(name, func(t *testing.T) {
			r := newSqRig(t)
			srv := r.connect("w", "4000")
			r.squatterOnPlain(tlsOn)
			a := r.a
			addr, fp := v(a, "squatter_addr"), v(a, "squatter_fingerprint")
			pc, key := pairPeerOn(t, a, tlsOn, "phone", nil)
			id := peerSession(t, pc, "w").ID
			tp := newTape(pc)
			if err := pc.Attach(bg(t), id, nil, 0); err != nil {
				t.Fatal(err)
			}
			srv.Send([]byte("l1\r\nl2\r\nl3\r\n"))
			eventually(t, "l3", func() bool { return tp.has("game:l3") })

			const nGame, nPeer = 300, 120
			var wg sync.WaitGroup
			started := make(chan struct{})
			wg.Add(2)
			go func() { // the game talks to the peers
				defer wg.Done()
				for i := 0; i < nGame; i++ {
					if i == 20 {
						close(started)
					}
					srv.Send([]byte(fmt.Sprintf("m%03d\r\n", i)))
				}
			}()
			go func() { // the peer talks to the game
				defer wg.Done()
				for i := 0; i < nPeer; i++ {
					if err := pc.Send(bg(t), id, fmt.Sprintf("s%03d", i)); err != nil {
						t.Errorf("send %d: %v", i, err)
						return
					}
				}
			}()
			<-started
			var filed []byte
			r.probe = func(req ProbeRequest) error {
				filed, _ = os.ReadFile(req.StatePath)
				return r.rehearse(req)
			}
			if err := r.restart(false); err != nil {
				t.Fatal(err)
			}
			wg.Wait()
			kind := map[bool]string{true: "tls", false: "tcp"}[tlsOn]
			i := strings.Index(string(filed), `"conns":`)
			if i < 0 || !strings.Contains(string(filed)[i:], `"kind":"`+kind+`"`) || !strings.Contains(string(filed)[i:], `"listener":`) {
				t.Fatalf("the state file does not carry the client as %s", kind)
			}
			b := r.newClient()
			if b == nil {
				t.Fatal("no new client")
			}
			eventually(t, "the squatter listens again", func() bool { return v(b, "squatter_state") == "listening" })
			if v(b, "squatter_addr") != addr || v(b, "squatter_fingerprint") != fp {
				t.Fatalf("address/fingerprint changed: %s %s -> %s %s", addr, fp, v(b, "squatter_addr"), v(b, "squatter_fingerprint"))
			}
			eventually(t, "everything arrived", func() bool {
				return tp.has(fmt.Sprintf("game:m%03d", nGame-1)) && tp.has(fmt.Sprintf("echo:s%03d", nPeer-1))
			})
			select {
			case <-pc.Done():
				t.Fatal("the client was disconnected by the restart")
			default:
			}
			seqs, lines := tp.snapshot()
			consecutive(t, seqs)
			if seqs[0] != 1 {
				t.Fatalf("first seq %d", seqs[0])
			}
			var wantM, wantS []string
			for i := 0; i < nGame; i++ {
				wantM = append(wantM, fmt.Sprintf("m%03d", i))
			}
			for i := 0; i < nPeer; i++ {
				wantS = append(wantS, fmt.Sprintf("s%03d", i))
			}
			if got := withPrefix(lines, "game:", "m"); strings.Join(got, ",") != strings.Join(wantM, ",") {
				t.Fatalf("game lines lost, repeated or reordered: got %d lines", len(got))
			}
			if got := withPrefix(lines, "echo:", "s"); strings.Join(got, ",") != strings.Join(wantS, ",") {
				t.Fatalf("echoes lost, repeated or reordered: got %d lines", len(got))
			}
			// the game got everything the peer sent, once, in order
			var want strings.Builder
			for _, s := range wantS {
				want.WriteString(s + "\r\n")
			}
			srv.Expect([]byte(want.String()))
			// the carried connection works for requests and new input, the listener for new clients
			ss, err := pc.List(bg(t))
			if err != nil || len(ss) != 1 || ss[0].ID != id {
				t.Fatalf("list over the carried connection: %v %+v", err, ss)
			}
			if err := pc.Send(bg(t), id, "after"); err != nil {
				t.Fatal(err)
			}
			srv.Expect([]byte("after\r\n"))
			pc2, err := dialPeerOn(t, b, tlsOn, key, "", "phone", nil)
			if err != nil {
				t.Fatalf("a new connection to the inherited listener: %v", err)
			}
			defer pc2.Close()
			eventually(t, "two clients", func() bool { return peerCount(b) == 2 })
		})
	}
}

// A restart that is abandoned after the clients were taken out of service puts them back: they never
// notice, and the listener accepts again.
func TestRestartAbandonedKeepsSquatterClientsAndListener(t *testing.T) {
	r := newSqRig(t)
	srv := r.connect("w", "4000")
	r.squatterOn()
	pc, key := pairPeerOn(t, r.a, true, "phone", nil)
	id := peerSession(t, pc, "w").ID
	tp := newTape(pc)
	if err := pc.Attach(bg(t), id, nil, 0); err != nil {
		t.Fatal(err)
	}
	for name, mut := range map[string]func(*handoff.File){
		"refused by the rehearsal": func(f *handoff.File) {
			sec := f.Sections[secSquatter]
			sec.Data = []byte(`{"active":true,"state":{"version":9}}`)
			f.Sections[secSquatter] = sec
		},
	} {
		mut := mut
		r.probe = func(req ProbeRequest) error {
			r.mutate(req.StatePath, mut)
			return r.rehearse(req)
		}
		if err := r.restart(false); err == nil || r.newClient() != nil {
			t.Fatalf("%s: the restart went ahead: %v", name, err)
		}
		// no line is lost or repeated, in either direction, and the connection is still the same one
		srv.Send([]byte("after " + strings.ReplaceAll(name, " ", "_") + "\r\n"))
		eventually(t, name, func() bool { return tp.has("game:after " + strings.ReplaceAll(name, " ", "_")) })
		if err := pc.Send(bg(t), id, "up"); err != nil {
			t.Fatal(err)
		}
		srv.Expect([]byte("up\r\n"))
		// and the listener serves again
		pc2, err := dialPeerOn(t, r.a, true, key, "", "phone", nil)
		if err != nil {
			t.Fatalf("%s: new connection after the abandoned restart: %v", name, err)
		}
		pc2.Close()
	}
	seqs, _ := tp.snapshot()
	consecutive(t, seqs)
	select {
	case <-pc.Done():
		t.Fatal("the client was disconnected")
	default:
	}
	// a restart that works afterwards still carries it
	r.probe = nil
	if err := r.restart(false); err != nil {
		t.Fatal(err)
	}
	srv.Send([]byte("finally\r\n"))
	eventually(t, "finally", func() bool { return tp.has("game:finally") })
	consecutive(t, func() []uint64 { s, _ := tp.snapshot(); return s }())
}

// A request that a client has sent and the squatter has received but the engine has not run yet (it is
// waiting in the queue while /restart occupies the engine) is run during the quiesce, not lost.
func TestRestartRunsRequestsStillInTheQueue(t *testing.T) {
	r := newSqRig(t)
	srv := r.connect("w", "4000")
	r.squatterOn()
	pc, _ := pairPeerOn(t, r.a, true, "phone", nil)
	id := peerSession(t, pc, "w").ID
	tp := newTape(pc)
	if err := pc.Attach(bg(t), id, nil, 0); err != nil {
		t.Fatal(err)
	}
	var err error
	a := r.a
	a.Do(func(*gtfparser.Interp) {
		go func() { _ = pc.Send(bg(t), id, "in flight") }()
		eventually(t, "the request to reach the engine's queue", func() bool {
			a.sq.q.mu.Lock()
			defer a.sq.q.mu.Unlock()
			return len(a.sq.q.q) > 0
		})
		err = a.Restart(false) // the request cannot run before this returns, except through the quiesce
	})
	if err != nil {
		t.Fatal(err)
	}
	srv.Expect([]byte("in flight\r\n"))
	eventually(t, "its echo", func() bool { return tp.has("echo:in flight") })
	seqs, lines := tp.snapshot()
	consecutive(t, seqs)
	if n := len(withPrefix(lines, "echo:", "in flight")); n != 1 {
		t.Fatalf("the echo arrived %d times", n)
	}
}

// A client whose TLS session cannot be exported (TLS 1.2 with a CBC suite) does not stop the restart
// and does not take the others down: it is dropped by the exec, reconnects, and resumes by sequence
// number; the client that can be carried stays connected.
func TestRestartDropsOnlyTheClientThatCannotBeCarried(t *testing.T) {
	r := newSqRig(t)
	srv := r.connect("w", "4000")
	r.squatterOn()
	good, _ := pairPeerOn(t, r.a, true, "good", nil)
	cbc := func(c *tls.Config) {
		c.MinVersion, c.MaxVersion = tls.VersionTLS12, tls.VersionTLS12
		c.CipherSuites = []uint16{tls.TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA}
	}
	old, oldKey := pairPeerOn(t, r.a, true, "old", cbc)
	id := peerSession(t, good, "w").ID
	tg, to := newTape(good), newTape(old)
	for _, p := range []*wire.Client{good, old} {
		if err := p.Attach(bg(t), id, nil, 0); err != nil {
			t.Fatal(err)
		}
	}
	srv.Send([]byte("one\r\n"))
	eventually(t, "one", func() bool { return tg.has("game:one") && to.has("game:one") })
	var plan RestartPlan
	r.a.Do(func(*gtfparser.Interp) { plan = r.a.RestartPlan() })
	if len(plan.Ends) != 0 {
		t.Fatalf("a client that reconnects must not block the restart: %+v", plan.Ends)
	}
	var sawGood, sawOld bool
	for _, it := range plan.Carried {
		sawGood = sawGood || (it.Name == "squatter client good" && it.Kind == "tls")
		sawOld = sawOld || (it.Name == "squatter client old" && strings.HasPrefix(it.Kind, "reconnects"))
	}
	if !sawGood || !sawOld {
		t.Fatalf("plan: %+v", plan.Carried)
	}
	if err := r.restart(false); err != nil {
		t.Fatal(err)
	}
	b := r.newClient()
	srv.Send([]byte("two\r\n"))
	eventually(t, "two for the carried client", func() bool { return tg.has("game:two") })
	select {
	case <-good.Done():
		t.Fatal("the client that could be carried was disconnected")
	default:
	}
	eventually(t, "the other one is dropped", func() bool {
		select {
		case <-old.Done():
			return true
		default:
			return false
		}
	})
	// it reconnects and resumes from its last number, with nothing missing and nothing twice
	last := old.LastSeq(id)
	eventually(t, "the squatter listens", func() bool { return v(b, "squatter_state") == "listening" })
	old2, err := dialPeerOn(t, b, true, oldKey, "", "old", cbc)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { old2.Close() })
	if err := old2.Attach(bg(t), id, &last, 0); err != nil {
		t.Fatal(err)
	}
	t2 := newTape(old2)
	eventually(t, "the resumed client sees two", func() bool { return t2.has("game:two") })
	seqs, lines := t2.snapshot()
	consecutive(t, seqs)
	if seqs[0] != last+1 || len(withPrefix(lines, "game:", "one")) != 0 {
		t.Fatalf("resume started at %d (last was %d): %v", seqs[0], last, lines)
	}
	seqsG, _ := tg.snapshot()
	consecutive(t, seqsG)
}

// If the listener and the clients cannot be used by the new process (a connections format it does not
// know), the restart still goes ahead and the squatter comes back from its state: the clients are
// dropped, reconnect, and resume exactly, and nothing is paired again.
func TestRestartFallsBackToStateWhenTheConnectionsCannotBeUsed(t *testing.T) {
	r := newSqRig(t)
	srv := r.connect("w", "4000")
	r.squatterOn()
	pc, key := pairPeerOn(t, r.a, true, "phone", nil)
	id := peerSession(t, pc, "w").ID
	tp := newTape(pc)
	if err := pc.Attach(bg(t), id, nil, 0); err != nil {
		t.Fatal(err)
	}
	srv.Send([]byte("a\r\nb\r\n"))
	eventually(t, "b", func() bool { return tp.has("game:b") })
	fp := v(r.a, "squatter_fingerprint")
	r.probe = func(req ProbeRequest) error {
		r.mutate(req.StatePath, func(f *handoff.File) {
			sec := f.Sections[secSquatter]
			var m map[string]any
			_ = json.Unmarshal(sec.Data, &m)
			m["conns"] = map[string]any{"v": 9}
			sec.Data, _ = json.Marshal(m)
			f.Sections[secSquatter] = sec
		})
		return r.rehearse(req)
	}
	if err := r.restart(false); err != nil {
		t.Fatalf("the restart was refused: %v", err)
	}
	b := r.newClient()
	srv.Send([]byte("c\r\n")) // while the client is away
	eventually(t, "squatter listening", func() bool { return v(b, "squatter_state") == "listening" })
	eventually(t, "the client is dropped", func() bool {
		select {
		case <-pc.Done():
			return true
		default:
			return false
		}
	})
	if v(b, "squatter_fingerprint") != fp {
		t.Fatal("the certificate changed")
	}
	last := pc.LastSeq(id)
	pc2, err := dialPeerOn(t, b, true, key, "", "phone", nil)
	if err != nil {
		t.Fatalf("reconnect without pairing: %v", err)
	}
	defer pc2.Close()
	if err := pc2.Attach(bg(t), id, &last, 0); err != nil {
		t.Fatal(err)
	}
	t2 := newTape(pc2)
	eventually(t, "c", func() bool { return t2.has("game:c") })
	seqs, _ := t2.snapshot()
	consecutive(t, seqs)
	if seqs[0] != last+1 {
		t.Fatalf("resume from %d, last was %d", seqs[0], last)
	}
}
