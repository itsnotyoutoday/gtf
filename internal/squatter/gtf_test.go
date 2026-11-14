// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package squatter

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/itsnotyoutoday/gtf/internal/squatter/auth"
	"github.com/itsnotyoutoday/gtf/internal/squatter/proto"
	"github.com/itsnotyoutoday/gtf/internal/squatter/wire"
)

func attachWait(t *testing.T, r *rig, c *wire.Client, id string) {
	t.Helper()
	if err := c.Attach(tctx(t), id, nil, 0); err != nil {
		t.Fatal(err)
	}
	collect(t, c, isBacklogDone)
	waitFor(t, "attached", func() bool { return r.sq.session(id).attachedCount() > 0 })
}

// ---------------------------------------------------------------- negotiation

func TestNegotiation(t *testing.T) {
	r := newRig(t, nil)
	// old peer: announces nothing, welcome has no ext member at all
	old, _ := r.paired("operator", "old")
	if _, _, ok := negotiated(t, old); ok {
		t.Fatal("a plain peer must get no ext in its welcome")
	}
	// new peer: intersection, unknown flags ignored, macros reserved but never announced
	nw := r.gtfPeer("operator", "new", hello("author", "typing", "telepathy", "macros", "naws", "author"))
	v, feats, ok := negotiated(t, nw)
	if !ok || v != 1 || joinLines(feats) != "author|naws|typing" {
		t.Fatalf("v=%d feats=%v ok=%v", v, feats, ok)
	}
	// a newer client: the server answers with the version it speaks
	nw2 := r.gtfPeer("operator", "new2", map[string]any{"v": 7, "features": []string{"presence"}})
	if v, feats, _ := negotiated(t, nw2); v != 1 || joinLines(feats) != "presence" {
		t.Fatalf("v=%d %v", v, feats)
	}
	// mismatch: the client cannot speak version 1: explicit refusal, and the peer stays plain
	mm := r.gtfPeer("operator", "mm", map[string]any{"v": 3, "minV": 2, "features": []string{"presence"}})
	raw := mm.WelcomeExt()[GTFName]
	if !strings.Contains(string(raw), `"version_mismatch"`) || !strings.Contains(string(raw), `"v":0`) {
		t.Fatalf("%s", raw)
	}
	if g := r.sq.gtfOf(peerByName(t, r, "mm")); g != nil {
		t.Fatal("a mismatched peer must stay plain")
	}
	// garbage announcement: behaves as if nothing was announced
	for i, bad := range []any{map[string]any{"v": 0}, map[string]any{"v": "x"}, "str", map[string]any{}} {
		c := r.gtfPeer("operator", "bad"+string(rune('a'+i)), bad)
		if _, _, ok := negotiated(t, c); ok {
			t.Fatalf("garbage announcement %v was accepted", bad)
		}
	}
	// switched off by the owner: nothing is negotiated even when asked
	cfg := r.cfg
	cfg.GTF = false
	r.apply(cfg)
	off := r.gtfPeer("operator", "off", hello(allFeatures...))
	if _, _, ok := negotiated(t, off); ok {
		t.Fatal("squatter_gtf=off must not negotiate")
	}
}

func TestNegotiationSWPKind(t *testing.T) {
	r := newRig(t, func(c *Config) { c.Kind = KindSWP })
	c := r.gtfPeer("operator", "x", hello(allFeatures...))
	if _, _, ok := negotiated(t, c); ok {
		t.Fatal("the swp kind speaks plain SWP only")
	}
}

func peerByName(t *testing.T, r *rig, name string) wire.Peer {
	t.Helper()
	var p wire.Peer
	waitFor(t, "peer "+name, func() bool {
		r.sq.pmu.Lock()
		defer r.sq.pmu.Unlock()
		for _, pi := range r.sq.peers {
			if pi.p.DeviceName() == name {
				p = pi.p
			}
		}
		return p != nil
	})
	return p
}

// ---------------------------------------------------------------- old and new peers side by side

func TestPlainAndGTFPeersTogether(t *testing.T) {
	r := newRig(t, nil)
	id := r.open("mud")
	plain, _ := r.paired("operator", "oldie")
	ext := r.gtfPeer("operator", "phone", hello(allFeatures...))
	attachWait(t, r, plain, id)
	attachWait(t, r, ext, id)

	// the extension peer types: both see the echo, only the extension peer sees "author"
	if err := ext.Send(tctx(t), id, "hello"); err != nil {
		t.Fatal(err)
	}
	var pl, el wire.Line
	collect(t, plain, func(ev wire.Event) bool { l, ok := ev.(wire.Line); pl = l; return ok && l.Kind == proto.KindEcho })
	collect(t, ext, func(ev wire.Event) bool { l, ok := ev.(wire.Line); el = l; return ok && l.Kind == proto.KindEcho })
	if pl.Author != "" || pl.Origin == "" || plain_(pl.Spans) != "hello" {
		t.Fatalf("plain peer must see today's echo: %+v", pl)
	}
	if el.Author != "phone" || el.Origin != pl.Origin {
		t.Fatalf("extension peer: %+v", el)
	}
	// the local user (script or typing) is "local"
	r.h.Do(func() { r.sq.Line(id, proto.KindEcho, []proto.Span{{S: "from gtf"}}, false, "tf") })
	collect(t, ext, func(ev wire.Event) bool { l, ok := ev.(wire.Line); el = l; return ok && l.Spans[0].S == "from gtf" })
	collect(t, plain, func(ev wire.Event) bool { l, ok := ev.(wire.Line); pl = l; return ok && l.Spans[0].S == "from gtf" })
	if el.Author != "local" || pl.Author != "" || pl.Origin != "tf" {
		t.Fatalf("%+v / %+v", el, pl)
	}

	// replay: a fresh plain peer and a fresh extension peer
	plain2, _ := r.paired("viewer", "oldie2")
	ext2 := r.gtfPeer("viewer", "tablet", hello(allFeatures...))
	if err := plain2.Attach(tctx(t), id, nil, 0); err != nil {
		t.Fatal(err)
	}
	if err := ext2.Attach(tctx(t), id, nil, 0); err != nil {
		t.Fatal(err)
	}
	var withAuthor, plainAuthor int
	for _, ev := range collect(t, plain2, isBacklogDone) {
		if b, ok := ev.(wire.Backlog); ok {
			for _, e := range b.Entries {
				if e.Line != nil && e.Line.Author != "" {
					plainAuthor++
				}
			}
		}
	}
	for _, ev := range collect(t, ext2, isBacklogDone) {
		if b, ok := ev.(wire.Backlog); ok {
			for _, e := range b.Entries {
				if e.Line != nil && e.Line.Author != "" {
					withAuthor++
				}
			}
		}
	}
	if plainAuthor != 0 || withAuthor != 2 {
		t.Fatalf("replay author tags: plain %d (want 0), extension %d (want 2)", plainAuthor, withAuthor)
	}

	// everything the extension adds stays away from the plain peer: no presence, no gtf.*, no world.changed
	if err := ext.RawSend(tctx(t), "gtf.view", map[string]any{"session": id}); err != nil {
		t.Fatal(err)
	}
	r.h.Do(func() { r.sq.WorldEvent("add", "x", "", "", &proto.TFWorld{Name: "x", Rev: 5}) })
	noExt(t, plain, "gtf.presence", 200*time.Millisecond)
	// plain peer behaviour for the calls the extension changes
	ctx := tctx(t)
	if err := plain.Resize(ctx, id, 50, 20); err != nil {
		t.Fatalf("resize from a plain peer is still accepted: %v", err)
	}
	time.Sleep(3 * nawsDebounce())
	if n := len(r.h.nawsCalls()); n != 0 {
		t.Fatalf("a plain peer's resize must stay ignored, got %v", r.h.nawsCalls())
	}
	var pe *wire.ProtoError
	if _, err := plain.PutWorld(ctx, wire.WorldPutReq{World: proto.TFWorld{Name: "n", Host: "h", Port: "1"}}); !errors.As(err, &pe) || pe.Code != ErrUnsupported {
		t.Fatalf("world.put from a plain peer must still be refused: %v", err)
	}
	ws, err := plain.Worlds(ctx)
	if err != nil || len(ws) != 1 || ws[0].Rev != 0 || ws[0].MFile != "" {
		t.Fatalf("plain world.list: %+v %v", ws, err)
	}
	cs, err := plain.ControlGet(ctx, id)
	if err != nil || cs.Input != wire.ModeBoth {
		t.Fatalf("%+v %v", cs, err)
	}
}

func plain_(sp []proto.Span) string { return plain(sp) }

func TestControlSharedState(t *testing.T) {
	r := newRig(t, nil)
	id := r.open("mud")
	ss := r.sq.session(id)
	plainJSON, _ := json.Marshal(ss.controlFor(nil, "r1"))
	if strings.Contains(string(plainJSON), "shared") || !strings.Contains(string(plainJSON), `"input":"both"`) {
		t.Fatalf("plain control.state must be today's: %s", plainJSON)
	}
	g := &gtfPeer{features: map[string]bool{FeatPresence: true}}
	extJSON, _ := json.Marshal(ss.controlFor(g, "r1"))
	if !strings.Contains(string(extJSON), `"shared":{`) || !strings.Contains(string(extJSON), `"model":"shared"`) || !strings.Contains(string(extJSON), `"input":"both"`) {
		t.Fatalf("%s", extJSON)
	}
}

// ---------------------------------------------------------------- presence and typing

func TestPresenceViewAndTyping(t *testing.T) {
	fastTimers(t)
	r := newRig(t, nil)
	s1 := r.open("alpha")
	s2 := r.open("beta")
	a := r.gtfPeer("operator", "A", hello(allFeatures...))
	b := r.gtfPeer("operator", "B", hello(allFeatures...))
	v := r.gtfPeer("viewer", "V", hello(allFeatures...))
	ctx := tctx(t)

	// the snapshot a peer gets at join has the local terminal and everyone attached
	snap := nextExt(t, v, "gtf.presence", func(m map[string]any) bool { return len(presenceOf(m)) == 4 })
	p := presenceOf(snap)
	if !p["local"]["local"].(bool) || p["A"]["role"] != "operator" || p["V"]["role"] != "viewer" {
		t.Fatalf("%v", p)
	}

	// views are per peer and nobody moves anybody else's
	if err := a.RawSend(ctx, "gtf.view", map[string]any{"session": s1}); err != nil {
		t.Fatal(err)
	}
	if err := b.RawSend(ctx, "gtf.view", map[string]any{"session": s2}); err != nil {
		t.Fatal(err)
	}
	r.h.Do(func() { r.sq.LocalView(s2) })
	got := nextExt(t, v, "gtf.presence", func(m map[string]any) bool {
		q := presenceOf(m)
		return q["A"]["view"] == s1 && q["B"]["view"] == s2 && q["local"]["view"] == s2
	})
	if presenceOf(got)["V"]["view"] != "" {
		t.Fatal("the viewer never viewed anything")
	}
	// a bad session is refused, the view stays
	if _, err := a.RawRequest(ctx, "gtf.view", map[string]any{"session": "nope"}); err == nil {
		t.Fatal("unknown session accepted")
	}

	// typing: B sees A typing, with expiry and without a refresh
	if err := a.RawSend(ctx, "gtf.typing", map[string]any{"session": s1, "typing": true}); err != nil {
		t.Fatal(err)
	}
	m := nextExt(t, b, "gtf.typing", func(m map[string]any) bool { return m["typing"] == true })
	if m["name"] != "A" || m["session"] != s1 {
		t.Fatalf("%v", m)
	}
	start := time.Now()
	nextExt(t, b, "gtf.typing", func(m map[string]any) bool { return m["typing"] == false && m["name"] == "A" })
	if d := time.Since(start); d < typingTTL()/2 {
		t.Fatalf("typing expired after %v, ttl is %v", d, typingTTL())
	}
	// a refresh keeps it alive
	for i := 0; i < 5; i++ {
		_ = a.RawSend(ctx, "gtf.typing", map[string]any{"session": s1, "typing": true})
		time.Sleep(typingTTL() / 3)
	}
	r.sq.pmu.Lock()
	still := ""
	for _, pi := range r.sq.peers {
		if pi.p.DeviceName() == "A" {
			still = pi.gtf.typing
		}
	}
	r.sq.pmu.Unlock()
	if still != s1 {
		t.Fatal("refreshed typing must not expire")
	}
	// sending ends it at once
	attachWait(t, r, a, s1)
	if err := a.Send(ctx, s1, "x"); err != nil {
		t.Fatal(err)
	}
	nextExt(t, b, "gtf.typing", func(m map[string]any) bool { return m["typing"] == false && m["name"] == "A" })
	// a viewer cannot type
	if _, err := v.RawRequest(ctx, "gtf.typing", map[string]any{"session": s1, "typing": true}); err == nil {
		t.Fatal("viewer typing accepted")
	} else {
		var pe *wire.ProtoError
		if !errors.As(err, &pe) || pe.Code != proto.ErrForbidden {
			t.Fatal(err)
		}
	}
	// the local user's typing reaches peers and expires too
	r.h.Do(func() { r.sq.LocalTyping(s1, true) })
	m = nextExt(t, b, "gtf.typing", func(m map[string]any) bool { return m["cid"] == "local" && m["typing"] == true })
	if m["session"] != s1 {
		t.Fatalf("%v", m)
	}
	nextExt(t, b, "gtf.typing", func(m map[string]any) bool { return m["cid"] == "local" && m["typing"] == false })
	// a peer leaving updates everyone
	a.Close()
	nextExt(t, b, "gtf.presence", func(m map[string]any) bool { _, has := presenceOf(m)["A"]; return !has })
	if got := len(r.sq.Presence()); got != 3 {
		t.Fatalf("Presence() has %d entries", got)
	}
}

func TestTypingRateLimitAndBuckets(t *testing.T) {
	b := &bucket{rate: 10, burst: 3}
	now := time.Now()
	for i := 0; i < 3; i++ {
		if !b.allow(now) {
			t.Fatal("burst")
		}
	}
	if b.allow(now) {
		t.Fatal("over burst")
	}
	if !b.allow(now.Add(150 * time.Millisecond)) {
		t.Fatal("refill")
	}
	if b.allow(now.Add(160 * time.Millisecond)) {
		t.Fatal("not refilled yet")
	}
}

func TestSendFloodFromGTFPeerIsLimited(t *testing.T) {
	r := newRig(t, nil)
	id := r.open("mud")
	c := r.gtfPeer("operator", "flood", hello(allFeatures...))
	ctx := tctx(t)
	for i := 0; i < 120; i++ {
		if err := c.Send(ctx, id, "x"); err != nil {
			t.Fatal(err)
		}
	}
	collect(t, c, func(ev wire.Event) bool { e, ok := ev.(wire.Error); return ok && e.Code == proto.ErrRateLimited })
}

// ---------------------------------------------------------------- NAWS policy

func TestNAWSPolicyTable(t *testing.T) {
	now := time.Now()
	mk := func(policy string) *Squatter {
		s := New(newFakeHost(t), Options{Home: t.TempDir()})
		s.local.cols = 80
		return s
	}
	add := func(s *Squatter, id uint64, role string, cols int, input time.Time, attached bool, feats ...string) {
		g := &gtfPeer{features: map[string]bool{}, attached: map[string]bool{"s": attached}, widths: map[string]int{"s": cols}, lastInput: map[string]time.Time{"s": input}}
		for _, f := range feats {
			g.features[f] = true
		}
		s.peers[id] = &peerInfo{p: fakePeer{id, role}, role: role, gtf: g}
	}
	cases := []struct {
		name, policy string
		local        time.Time
		build        func(*Squatter)
		want         int
	}{
		{"local ignores peers", NAWSLocal, now, func(s *Squatter) { add(s, 1, "operator", 200, now.Add(time.Second), true, FeatNAWS) }, 80},
		{"active: peer typed last", NAWSActive, now, func(s *Squatter) { add(s, 1, "operator", 200, now.Add(time.Second), true, FeatNAWS) }, 200},
		{"active: local typed last", NAWSActive, now.Add(2 * time.Second), func(s *Squatter) { add(s, 1, "operator", 200, now.Add(time.Second), true, FeatNAWS) }, 80},
		{"active: nobody typed -> local", NAWSActive, time.Time{}, func(s *Squatter) { add(s, 1, "operator", 200, time.Time{}, true, FeatNAWS) }, 80},
		{"active: two peers, latest wins", NAWSActive, now, func(s *Squatter) {
			add(s, 1, "operator", 120, now.Add(time.Second), true, FeatNAWS)
			add(s, 2, "admin", 90, now.Add(2*time.Second), true, FeatNAWS)
		}, 90},
		{"min", NAWSMin, now, func(s *Squatter) {
			add(s, 1, "operator", 120, now, true, FeatNAWS)
			add(s, 2, "operator", 60, now, true, FeatNAWS)
		}, 60},
		{"max", NAWSMax, now, func(s *Squatter) {
			add(s, 1, "operator", 120, now, true, FeatNAWS)
			add(s, 2, "operator", 60, now, true, FeatNAWS)
		}, 120},
		{"detached peer does not count", NAWSMin, now, func(s *Squatter) { add(s, 1, "operator", 20, now, false, FeatNAWS) }, 80},
		{"viewer does not count", NAWSMin, now, func(s *Squatter) { add(s, 1, "viewer", 20, now, true, FeatNAWS) }, 80},
		{"peer without the naws feature does not count", NAWSMin, now, func(s *Squatter) { add(s, 1, "operator", 20, now, true, FeatPresence) }, 80},
		{"unknown width does not count", NAWSMin, now, func(s *Squatter) { add(s, 1, "operator", 0, now, true, FeatNAWS) }, 80},
		{"tiny clamps up", NAWSMin, now, func(s *Squatter) { add(s, 1, "operator", 3, now, true, FeatNAWS) }, minPeerWidth},
		{"huge clamps down", NAWSMax, now, func(s *Squatter) { add(s, 1, "operator", 99999, now, true, FeatNAWS) }, maxPeerWidth},
		{"no peers -> local", NAWSMax, now, func(s *Squatter) {}, 80},
	}
	for _, tc := range cases {
		s := mk(tc.policy)
		s.local.lastInput = tc.local
		tc.build(s)
		if got := s.computeLocked(tc.policy, "s"); got != tc.want {
			t.Errorf("%s: got %d want %d", tc.name, got, tc.want)
		}
	}
}

type fakePeer struct {
	id   uint64
	role string
}

func (p fakePeer) ID() uint64          { return p.id }
func (p fakePeer) DeviceID() string    { return "d" }
func (p fakePeer) DeviceName() string  { return "n" }
func (p fakePeer) Caps() proto.Caps    { return proto.Caps{} }
func (p fakePeer) Send(any) bool       { return true }
func (p fakePeer) SendRaw([]byte) bool { return true }
func (p fakePeer) Close(string)        {}
func (p fakePeer) Role() string        { return p.role }

func nawsRig(t *testing.T, policy string) (*rig, string) {
	fastTimers(t)
	r := newRig(t, func(c *Config) { c.NAWS = policy })
	id := r.open("mud")
	r.h.Do(func() { r.sq.SessionCols(id, 80) }) // gtf told the game 80 when it connected
	return r, id
}

func TestNAWSActiveDebounceAndDetach(t *testing.T) {
	r, id := nawsRig(t, NAWSActive)
	a := r.gtfPeer("operator", "A", hello(allFeatures...))
	ctx := tctx(t)
	attachWait(t, r, a, id)
	if err := a.Resize(ctx, id, 132, 40); err != nil {
		t.Fatal(err)
	}
	time.Sleep(4 * nawsDebounce())
	if n := len(r.h.nawsCalls()); n != 0 {
		t.Fatalf("nobody typed yet: the local terminal keeps the width, got %v", r.h.nawsCalls())
	}
	// A types: A is the active client
	_ = a.Send(ctx, id, "look")
	waitFor(t, "132", func() bool { c := r.h.nawsCalls(); return len(c) == 1 && c[0] == nawsCall{id, 132} })
	if got := r.sq.ReportedCols(id); got != 132 {
		t.Fatal(got)
	}
	// the NAWS change is announced to extension peers
	m := nextExt(t, a, "gtf.naws", nil)
	if m["cols"].(float64) != 132 || m["session"] != id || m["policy"] != NAWSActive {
		t.Fatalf("%v", m)
	}
	// local types, A types, local types within one debounce window: one change, to the last
	r.h.Do(func() { r.sq.LocalInput() })
	_ = a.Send(ctx, id, "x")
	time.Sleep(2 * time.Millisecond)
	r.h.Do(func() { r.sq.LocalInput() })
	waitFor(t, "back to 80", func() bool { c := r.h.nawsCalls(); return len(c) == 2 && c[1].cols == 80 })
	time.Sleep(4 * nawsDebounce())
	if n := len(r.h.nawsCalls()); n != 2 {
		t.Fatalf("debounce: expected exactly 2 calls, got %v", r.h.nawsCalls())
	}
	// A is active again, then detaches: the width falls back to the local terminal
	_ = a.Send(ctx, id, "y")
	waitFor(t, "132 again", func() bool { c := r.h.nawsCalls(); return len(c) == 3 && c[2].cols == 132 })
	if err := a.Detach(ctx, id); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "fallback after detach", func() bool { c := r.h.nawsCalls(); return len(c) == 4 && c[3].cols == 80 })
	// reattach and disconnect: same
	attachWait(t, r, a, id)
	_ = a.Send(ctx, id, "z")
	waitFor(t, "132 third time", func() bool { c := r.h.nawsCalls(); return len(c) == 5 && c[4].cols == 132 })
	a.Close()
	waitFor(t, "fallback after disconnect", func() bool { c := r.h.nawsCalls(); return len(c) == 6 && c[5].cols == 80 })
}

func TestNAWSMinMaxLocalAndInvalid(t *testing.T) {
	for _, tc := range []struct {
		policy string
		want   int // after A=132, B=70 attached, local 80
	}{{NAWSMin, 70}, {NAWSMax, 132}} {
		r, id := nawsRig(t, tc.policy)
		a := r.gtfPeer("operator", "A", hello(allFeatures...))
		b := r.gtfPeer("admin", "B", hello(allFeatures...))
		ctx := tctx(t)
		attachWait(t, r, a, id)
		attachWait(t, r, b, id)
		_ = a.Resize(ctx, id, 132, 40)
		_ = b.Resize(ctx, id, 70, 24)
		last := func() int {
			c := r.h.nawsCalls()
			if len(c) == 0 {
				return 0
			}
			return c[len(c)-1].cols
		}
		waitFor(t, tc.policy, func() bool { return last() == tc.want })
		// input does not matter for min/max; a peer narrowing below the minimum is clamped
		_ = b.Resize(ctx, id, 3, 24)
		if tc.policy == NAWSMin {
			waitFor(t, "clamp", func() bool { return last() == minPeerWidth })
		}
		// invalid sizes are refused, the width stays
		before := len(r.h.nawsCalls())
		for _, bad := range [][2]int{{0, 24}, {-5, 24}, {5000, 24}} {
			if err := b.Resize(ctx, id, bad[0], bad[1]); err != nil {
				t.Fatal(err)
			}
			e := collect(t, b, func(ev wire.Event) bool { _, ok := ev.(wire.Error); return ok })
			if er := e[len(e)-1].(wire.Error); er.Code != proto.ErrBadRequest {
				t.Fatalf("%+v", er)
			}
		}
		time.Sleep(4 * nawsDebounce())
		if len(r.h.nawsCalls()) != before {
			t.Fatalf("invalid sizes changed the width: %v", r.h.nawsCalls())
		}
	}
	// local: peers never matter, nothing is ever applied by the squatter
	r, id := nawsRig(t, NAWSLocal)
	a := r.gtfPeer("operator", "A", hello(allFeatures...))
	attachWait(t, r, a, id)
	_ = a.Resize(tctx(t), id, 40, 24)
	_ = a.Send(tctx(t), id, "x")
	time.Sleep(4 * nawsDebounce())
	if n := len(r.h.nawsCalls()); n != 0 {
		t.Fatalf("local policy: %v", r.h.nawsCalls())
	}
	if r.sq.LocalResized(100, 30) {
		t.Fatal("local policy: the client resizes games itself")
	}
}

func TestNAWSViewerCannotResizeAndLocalResize(t *testing.T) {
	r, id := nawsRig(t, NAWSMin)
	v := r.gtfPeer("viewer", "V", hello(allFeatures...))
	if err := v.Resize(tctx(t), id, 20, 10); err != nil {
		t.Fatal(err)
	}
	e := collect(t, v, func(ev wire.Event) bool { _, ok := ev.(wire.Error); return ok })
	if e[len(e)-1].(wire.Error).Code != proto.ErrForbidden {
		t.Fatalf("%+v", e)
	}
	// the local terminal shrinks: min follows, debounced
	if !func() bool { var h bool; r.h.Do(func() { h = r.sq.LocalResized(60, 24) }); return h }() {
		t.Fatal("LocalResized must say the squatter handles it")
	}
	waitFor(t, "local 60", func() bool { c := r.h.nawsCalls(); return len(c) == 1 && c[0].cols == 60 })
	// the session cols the client reads at connect time follow the policy too
	var n int
	r.h.Do(func() { n = r.sq.SessionCols(id, 60) })
	if n != 60 {
		t.Fatal(n)
	}
}

// ---------------------------------------------------------------- worlds

func putWorld(t *testing.T, c *wire.Client, w proto.TFWorld, base uint64, extra map[string]any) (map[string]any, *wire.ProtoError) {
	t.Helper()
	f := map[string]any{"world": w, "baseRev": base}
	for k, v := range extra {
		f[k] = v
	}
	v, err := c.RawRequest(tctx(t), "world.put", f)
	if err != nil {
		var pe *wire.ProtoError
		if !errors.As(err, &pe) {
			t.Fatal(err)
		}
		return nil, pe
	}
	var m map[string]any
	if e, ok := v.(wire.Ext); ok {
		_ = json.Unmarshal(e.Raw, &m)
	} else {
		t.Fatalf("unexpected reply %#v", v)
	}
	return m, nil
}

func revOf(m map[string]any) uint64 { return uint64(m["world"].(map[string]any)["rev"].(float64)) }

func TestWorldCRUDWithVersions(t *testing.T) {
	saved := rateClasses["world"]
	rateClasses["world"] = [2]float64{1000, 1000}
	t.Cleanup(func() { rateClasses["world"] = saved })
	r := newRig(t, nil)
	adm := r.gtfPeer("admin", "phone", hello(allFeatures...))
	other := r.gtfPeer("admin", "tablet", hello(allFeatures...))
	plain, _ := r.paired("admin", "oldie")
	ctx := tctx(t)

	// create: baseRev 0
	res, perr := putWorld(t, adm, proto.TFWorld{Name: "new", Host: "n.example", Port: "4242", Char: "bob"}, 0, map[string]any{"password": "s3cret-pw"})
	if perr != nil || res["created"] != true || res["saved"] != true || revOf(res) == 0 {
		t.Fatalf("%v %v", res, perr)
	}
	rev1 := revOf(res)
	if res["world"].(map[string]any)["passSet"] != true {
		t.Fatal("passSet must report the stored password")
	}
	// the other extension peer got world.changed with the author; the plain peer did not
	ch := nextExt(t, other, "world.changed", func(m map[string]any) bool { return m["name"] == "new" })
	if ch["author"] != "phone" || ch["created"] != true || ch["op"] != "put" || uint64(ch["rev"].(float64)) != rev1 {
		t.Fatalf("%v", ch)
	}
	if strings.Contains(string(mustJSON(ch)), "s3cret-pw") {
		t.Fatal("password leaked into world.changed")
	}
	noExt(t, plain, "world.changed", 100*time.Millisecond)

	// creating again with baseRev 0 conflicts and shows the current record, without secrets
	_, perr = putWorld(t, other, proto.TFWorld{Name: "new", Host: "x", Port: "1"}, 0, nil)
	if perr == nil || perr.Code != ErrConflict {
		t.Fatalf("%v", perr)
	}
	var d struct {
		Current proto.TFWorld `json:"current"`
	}
	_ = json.Unmarshal(perr.Data, &d)
	if d.Current.Name != "new" || d.Current.Rev != rev1 || strings.Contains(string(perr.Data), "s3cret") {
		t.Fatalf("%s", perr.Data)
	}
	// update on the current revision: password kept (not sent), fields replaced
	res, perr = putWorld(t, adm, proto.TFWorld{Name: "new", Host: "n2.example", Port: "4243"}, rev1, nil)
	if perr != nil || res["created"] == true || revOf(res) <= rev1 {
		t.Fatalf("%v %v", res, perr)
	}
	rev2 := revOf(res)
	w := res["world"].(map[string]any)
	if w["char"] != nil && w["char"] != "" || w["host"] != "n2.example" || w["passSet"] != true {
		t.Fatalf("replace semantic: %v", w)
	}
	if r.h.db.pw["new"] != "s3cret-pw" {
		t.Fatal("password must be kept when not sent")
	}
	// an update based on the old revision is rejected with the current world
	_, perr = putWorld(t, other, proto.TFWorld{Name: "new", Host: "stale", Port: "1"}, rev1, nil)
	if perr == nil || perr.Code != ErrConflict {
		t.Fatalf("stale update accepted: %v", perr)
	}
	_ = json.Unmarshal(perr.Data, &d)
	if d.Current.Rev != rev2 || d.Current.Host != "n2.example" {
		t.Fatalf("%+v", d.Current)
	}
	// clear the password
	res, perr = putWorld(t, adm, proto.TFWorld{Name: "new", Host: "n2.example", Port: "4243"}, rev2, map[string]any{"clearPassword": true})
	if perr != nil || res["world"].(map[string]any)["passSet"] == true || r.h.db.pw["new"] != "" {
		t.Fatalf("%v %v", res, perr)
	}
	rev3 := revOf(res)
	// world.list for an extension peer carries revisions, never passwords or macro files
	v, err := adm.RawRequest(ctx, "world.list", nil)
	if err != nil {
		t.Fatal(err)
	}
	listed := mustJSON(v)
	if strings.Contains(string(listed), "secret.tf") || strings.Contains(string(listed), "hunter2") || strings.Contains(string(listed), "password") {
		t.Fatalf("world.list leaks: %s", listed)
	}
	ws, err := adm.Worlds(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, x := range ws {
		if x.Name == "new" && x.Rev == rev3 {
			found = true
		}
		if x.Rev == 0 {
			t.Fatalf("an extension peer must get revisions: %+v", x)
		}
	}
	if !found {
		t.Fatalf("%+v", ws)
	}

	// rename: needs the current revision, target must be free
	vv, err := adm.RawRequest(ctx, "world.rename", map[string]any{"name": "new", "newName": "mud", "baseRev": rev3})
	var pe *wire.ProtoError
	if !errors.As(err, &pe) || pe.Code != ErrExists {
		t.Fatalf("%v %v", vv, err)
	}
	if _, err = adm.RawRequest(ctx, "world.rename", map[string]any{"name": "new", "newName": "renamed", "baseRev": rev3 - 1}); !errors.As(err, &pe) || pe.Code != ErrConflict {
		t.Fatal(err)
	}
	if _, err = adm.RawRequest(ctx, "world.rename", map[string]any{"name": "new", "newName": "renamed"}); !errors.As(err, &pe) || pe.Code != proto.ErrBadRequest {
		t.Fatalf("baseRev is required: %v", err)
	}
	vv, err = adm.RawRequest(ctx, "world.rename", map[string]any{"name": "new", "newName": "renamed", "baseRev": rev3})
	if err != nil {
		t.Fatal(err)
	}
	rc := nextExt(t, other, "world.changed", func(m map[string]any) bool { return m["op"] == "rename" })
	if rc["oldName"] != "new" || rc["name"] != "renamed" {
		t.Fatalf("%v", rc)
	}
	var rn map[string]any
	_ = json.Unmarshal(vv.(wire.Ext).Raw, &rn)
	rev4 := revOf(rn)
	// delete: stale, busy, then fine
	if _, err = adm.RawRequest(ctx, "world.delete", map[string]any{"name": "renamed", "baseRev": rev3}); !errors.As(err, &pe) || pe.Code != ErrConflict {
		t.Fatal(err)
	}
	r.h.db.busy["renamed"] = true
	if _, err = adm.RawRequest(ctx, "world.delete", map[string]any{"name": "renamed", "baseRev": rev4}); !errors.As(err, &pe) || pe.Code != proto.ErrBusy {
		t.Fatal(err)
	}
	r.h.db.busy["renamed"] = false
	if _, err = adm.RawRequest(ctx, "world.delete", map[string]any{"name": "nope", "baseRev": 1}); !errors.As(err, &pe) || pe.Code != ErrNotFound {
		t.Fatal(err)
	}
	if _, err = adm.RawRequest(ctx, "world.delete", map[string]any{"name": "renamed", "baseRev": rev4}); err != nil {
		t.Fatal(err)
	}
	dc := nextExt(t, other, "world.changed", func(m map[string]any) bool { return m["op"] == "delete" })
	if dc["deleted"] != true || dc["name"] != "renamed" {
		t.Fatalf("%v", dc)
	}
	// every accepted change was persisted
	r.h.get(func() {
		if r.h.saves != 5 {
			t.Errorf("saves=%d", r.h.saves)
		}
	})
	// audit lines for the owner: who and what, never a secret
	r.h.get(func() {
		var all string
		for _, n := range r.h.notices {
			all += n + "\n"
		}
		if !strings.Contains(all, "phone (admin) added world new [host,port,type,char,tls,echo,charset,password]") ||
			!strings.Contains(all, "phone (admin) removed world renamed") || !strings.Contains(all, "renamed world new to renamed") ||
			strings.Contains(all, "s3cret") {
			t.Errorf("audit:\n%s", all)
		}
	})
}

func mustJSON(v any) []byte {
	if e, ok := v.(wire.Ext); ok {
		return e.Raw
	}
	b, _ := json.Marshal(v)
	return b
}

func TestWorldEditRolesValidationAndLimits(t *testing.T) {
	r := newRig(t, nil)
	ctx := tctx(t)
	for _, role := range []string{"viewer", "operator"} {
		c := r.gtfPeer(role, role, hello(allFeatures...))
		_, perr := putWorld(t, c, proto.TFWorld{Name: "x", Host: "h", Port: "1"}, 0, nil)
		if perr == nil || perr.Code != proto.ErrForbidden {
			t.Fatalf("%s: %v", role, perr)
		}
		if _, err := c.RawRequest(ctx, "world.delete", map[string]any{"name": "mud", "baseRev": 100}); err == nil {
			t.Fatalf("%s deleted a world", role)
		}
		if _, err := c.RawRequest(ctx, "world.rename", map[string]any{"name": "mud", "newName": "q", "baseRev": 100}); err == nil {
			t.Fatalf("%s renamed a world", role)
		}
	}
	if len(r.h.database().w) != 1 {
		t.Fatal("roles must gate every mutation")
	}
	saved := rateClasses["world"]
	rateClasses["world"] = [2]float64{1000, 1000}
	t.Cleanup(func() { rateClasses["world"] = saved })
	adm := r.gtfPeer("admin", "adm", hello(allFeatures...))
	bad := []proto.TFWorld{
		{Name: "", Host: "h", Port: "1"},
		{Name: "a b", Host: "h", Port: "1"},
		{Name: "default", Host: "h", Port: "1"},
		{Name: "(tmp)", Host: "h", Port: "1"},
		{Name: "q\"uote", Host: "h", Port: "1"},
		{Name: "x", Host: "h", Port: "0"},
		{Name: "x", Host: "h", Port: "70000"},
		{Name: "x", Host: "h", Port: "no port"},
		{Name: "x", Host: "bad host", Port: "1"},
		{Name: "x", Host: "h"},
		{Name: "x", Port: "1"},
		{Name: "x", Host: "h", Port: "1", Type: "bad type"},
		{Name: "x", Host: "h", Port: "1", Charset: "utf 8!"},
		{Name: "x", Host: "h", Port: "1", Char: "a\nb"},
		{Name: "x", Host: "h", Port: "1", MFile: "evil.tf"},
		{Name: strings.Repeat("n", 65), Host: "h", Port: "1"},
	}
	for _, w := range bad {
		if _, perr := putWorld(t, adm, w, 0, nil); perr == nil || perr.Code != proto.ErrBadRequest {
			t.Errorf("%+v accepted: %v", w, perr)
		}
	}
	if _, perr := putWorld(t, adm, proto.TFWorld{Name: "x", Host: "h", Port: "1"}, 0, map[string]any{"setMfile": true}); perr == nil || perr.Code != proto.ErrBadRequest {
		t.Fatal("setMfile must be refused")
	}
	if _, perr := putWorld(t, adm, proto.TFWorld{Name: "x", Host: "h", Port: "1"}, 0, map[string]any{"password": "a\"b"}); perr == nil {
		t.Fatal("quote in password accepted")
	}
	// the macro file of an existing world survives an edit and is never shown
	res, perr := putWorld(t, adm, proto.TFWorld{Name: "mud", Host: "h2", Port: "4001"}, 100, nil)
	if perr != nil || r.h.db.w["mud"].MFile != "secret.tf" || strings.Contains(string(mustJSON(res)), "secret.tf") {
		t.Fatalf("%v %v", res, perr)
	}
	// a service name is a valid port; a flood is rate limited
	if _, perr := putWorld(t, adm, proto.TFWorld{Name: "svc", Host: "h", Port: "telnet"}, 0, nil); perr != nil {
		t.Fatal(perr)
	}
	rateClasses["world"] = saved
	adm = r.gtfPeer("admin", "adm2", hello(allFeatures...))
	limited := false
	for i := 0; i < 40 && !limited; i++ {
		_, perr := putWorld(t, adm, proto.TFWorld{Name: "w" + strings.Repeat("x", i%5), Host: "h", Port: "1"}, 0, nil)
		limited = perr != nil && perr.Code == proto.ErrRateLimited
	}
	if !limited {
		t.Fatal("world edits are not rate limited")
	}
}

func TestLocalWorldChangeReachesExtensionPeers(t *testing.T) {
	r := newRig(t, nil)
	a := r.gtfPeer("viewer", "A", hello(allFeatures...))
	plain, _ := r.paired("viewer", "old")
	r.h.Do(func() {
		r.sq.WorldEvent("update", "mud", "", "", &proto.TFWorld{Name: "mud", Host: "h", Port: "1", Rev: 77, MFile: "x.tf"})
	})
	m := nextExt(t, a, "world.changed", nil)
	if m["author"] != "local" || m["rev"].(float64) != 77 || strings.Contains(string(mustJSON(m)), "x.tf") {
		t.Fatalf("%v", m)
	}
	noExt(t, plain, "world.changed", 100*time.Millisecond)
}

func TestValidateWorldUnit(t *testing.T) {
	if ValidateWorld(proto.TFWorld{Name: "ok-1_a.b", Host: "[::1]", Port: "4000"}) != nil {
		t.Fatal("valid world refused")
	}
	if ValidateWorld(proto.TFWorld{Name: "ok"}) != nil {
		t.Fatal("a world without host/port is valid (like tf's)")
	}
}

// ---------------------------------------------------------------- the simple hello.offer form

func TestHelloOfferForm(t *testing.T) {
	r := newRig(t, nil)
	dial := func(name string, offer []string) *wire.Client {
		key, _ := auth.GenerateKey()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		c, err := wire.Dial(ctx, r.url, wire.ClientOptions{Key: key, PairCode: r.pair("operator"), DeviceName: name,
			PinnedFingerprint: r.fp, ExpectKind: r.expectKind(), Offer: offer})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { c.Close() })
		return c
	}
	// "gtf" alone: every feature
	c := dial("a", []string{"gtf"})
	if got := c.Negotiated(); joinLines(got) != "gtf" {
		t.Fatalf("%v", got)
	}
	if _, feats, ok := negotiated(t, c); !ok || joinLines(feats) != "author|naws|presence|typing|worlds" {
		t.Fatalf("%v %v", feats, ok)
	}
	// narrowed, prefixed spelling is answered in the same spelling; unknown names are dropped
	c = dial("b", []string{"gtf", "gtf.presence", "gtf.telepathy"})
	if got := c.Negotiated(); joinLines(got) != "gtf|gtf.presence" {
		t.Fatalf("%v", got)
	}
	// without "gtf" nothing is accepted, even when feature names are listed
	c = dial("c", []string{"presence", "other"})
	if len(c.Negotiated()) != 0 {
		t.Fatalf("%v", c.Negotiated())
	}
	if _, _, ok := negotiated(t, c); ok {
		t.Fatal("no ext without gtf")
	}
}

// ---------------------------------------------------------------- tf.fg, kick and unpair by name

func TestForegroundFlagAndAmbiguousDeviceNames(t *testing.T) {
	r := newRig(t, nil)
	s1 := r.open("alpha")
	s2 := r.open("beta")
	c, _ := r.paired("operator", "SquatClient on my host")
	r.h.Do(func() { r.sq.LocalView(s2) })
	fg := func(id string) bool {
		l, err := c.List(tctx(t))
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range l {
			if s.ID == id {
				return s.TF != nil && s.TF.Fg
			}
		}
		t.Fatal("no such session")
		return false
	}
	if fg(s1) || !fg(s2) {
		t.Fatal("tf.fg must be true for gtf's foreground world only")
	}
	r.h.Do(func() { r.sq.LocalView(s1) })
	if !fg(s1) || fg(s2) {
		t.Fatal("tf.fg must follow the foreground world")
	}
	// a state message tells peers about the change
	// names with spaces work for kick; a second device with the same name makes it ambiguous
	c2, _ := r.paired("viewer", "SquatClient on my host")
	waitFor(t, "two peers", func() bool { return r.sq.NumPeers() == 2 })
	if _, err := r.sq.Kick("SquatClient on my host"); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("%v", err)
	}
	if _, err := r.sq.Unpair("squatclient on my host"); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("%v", err)
	}
	devs, _ := r.sq.Devices()
	var id2 string
	for _, d := range devs {
		if d.Role == "viewer" {
			id2 = d.DeviceID
		}
	}
	names, err := r.sq.Unpair(id2[:6])
	if err != nil || len(names) != 1 {
		t.Fatalf("%v %v", names, err)
	}
	select {
	case <-c2.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("unpaired device still connected")
	}
	if n, err := r.sq.Kick("SquatClient on my host"); err != nil || n != 1 {
		t.Fatalf("%d %v", n, err)
	}
	if n, err := r.sq.Kick("nobody"); err != nil || n != 0 {
		t.Fatal(n, err)
	}
	if _, err := r.sq.Unpair("ab"); err != nil {
		t.Fatal("a too-short prefix just matches nothing")
	}
}
