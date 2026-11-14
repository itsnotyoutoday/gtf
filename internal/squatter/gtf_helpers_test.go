// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package squatter

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/itsnotyoutoday/gtf/internal/squatter/auth"
	"github.com/itsnotyoutoday/gtf/internal/squatter/proto"
	"github.com/itsnotyoutoday/gtf/internal/squatter/wire"
)

// fakeDB is a world database with revisions, standing in for the engine's.
type fakeDB struct {
	mu     sync.Mutex
	rev    uint64
	w      map[string]proto.TFWorld
	pw     map[string]string
	mfile  map[string]string
	busy   map[string]bool
	events []string
}

func newFakeDB() *fakeDB {
	d := &fakeDB{rev: 100, w: map[string]proto.TFWorld{}, pw: map[string]string{}, mfile: map[string]string{}, busy: map[string]bool{}}
	d.w["mud"] = proto.TFWorld{Name: "mud", Host: "h", Port: "4000", PassSet: true, MFile: "secret.tf", Rev: 100}
	d.pw["mud"] = "hunter2"
	return d
}

func (d *fakeDB) next() uint64 { d.rev++; return d.rev }

func (d *fakeDB) Get(n string) (proto.TFWorld, bool) { w, ok := d.w[n]; return w, ok }
func (d *fakeDB) Put(w proto.TFWorld, set bool, pw string) (proto.TFWorld, error) {
	old, existed := d.w[w.Name]
	if set {
		d.pw[w.Name] = pw
	}
	w.PassSet = d.pw[w.Name] != ""
	w.Rev = d.next()
	if existed {
		w.MFile = old.MFile
	}
	d.w[w.Name] = w
	return w, nil
}
func (d *fakeDB) Delete(n string) error {
	if d.busy[n] {
		return ErrWorldBusy
	}
	delete(d.w, n)
	return nil
}
func (d *fakeDB) Rename(o, n string) (proto.TFWorld, error) {
	if _, ok := d.w[n]; ok {
		return proto.TFWorld{}, ErrWorldExists
	}
	w := d.w[o]
	delete(d.w, o)
	w.Name, w.Rev = n, d.next()
	d.w[n] = w
	d.pw[n] = d.pw[o]
	d.w[n] = w
	return w, nil
}

func (h *fakeHost) database() *fakeDB {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.db == nil {
		h.db = newFakeDB()
	}
	return h.db
}

func (h *fakeHost) Worlds() []proto.TFWorld {
	d := h.database()
	var out []proto.TFWorld
	for _, w := range d.w {
		out = append(out, w)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (h *fakeHost) EditWorld(e WorldEdit) (WorldResult, error) {
	d := h.database()
	res, err := ApplyWorldEdit(d, e)
	if err != nil {
		return res, err
	}
	h.mu.Lock()
	h.saves++
	h.mu.Unlock()
	res.Saved = true
	// what the client's Notify handler does: forward the event
	op := map[string]string{"put": "update", "delete": "remove", "rename": "rename"}[e.Op]
	if e.Op == "put" && res.Created {
		op = "add"
	}
	var rec *proto.TFWorld
	if e.Op != "delete" {
		r := res.World
		rec = &r
	}
	name := e.Name
	if e.Op == "rename" {
		name = e.NewName
	}
	h.sq.WorldEvent(op, name, e.Name, e.Author, rec)
	return res, nil
}

func (h *fakeHost) ApplyNAWS(session string, cols int) {
	h.mu.Lock()
	h.naws = append(h.naws, nawsCall{session, cols})
	h.mu.Unlock()
}

type nawsCall struct {
	session string
	cols    int
}

func (h *fakeHost) nawsCalls() []nawsCall {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]nawsCall(nil), h.naws...)
}

// gtfClient dials with the gtf extension announced (and a plain one with plain=true).
func (r *rig) gtfPeer(role, name string, announce any) *wire.Client {
	r.t.Helper()
	key, _ := auth.GenerateKey()
	return r.gtfPeerKey(role, name, announce, key, r.pair(role))
}

func (r *rig) gtfPeerKey(role, name string, announce any, key ed25519.PrivateKey, code string) *wire.Client {
	r.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	opts := wire.ClientOptions{Key: key, PairCode: code, DeviceName: name,
		PinnedFingerprint: r.fp, ExpectKind: r.expectKind(), InsecureLoopback: !r.cfg.TLS}
	if announce != nil {
		opts.Ext = map[string]any{GTFName: announce}
	}
	c, err := wire.Dial(ctx, r.url, opts)
	if err != nil {
		r.t.Fatal(err)
	}
	r.t.Cleanup(func() { c.Close() })
	return c
}

var allFeatures = []string{"author", "presence", "typing", "naws", "worlds"}

func hello(feats ...string) map[string]any { return map[string]any{"v": 1, "features": feats} }

// negotiated decodes the welcome's answer.
func negotiated(t *testing.T, c *wire.Client) (v int, feats []string, ok bool) {
	t.Helper()
	raw, found := c.WelcomeExt()[GTFName]
	if !found {
		return 0, nil, false
	}
	var a struct {
		V        int      `json:"v"`
		Features []string `json:"features"`
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		t.Fatal(err)
	}
	return a.V, a.Features, true
}

// next reads events until one is an Ext of type typ.
func nextExt(t *testing.T, c *wire.Client, typ string, pred func(map[string]any) bool) map[string]any {
	t.Helper()
	var found map[string]any
	collect(t, c, func(ev wire.Event) bool {
		var raw []byte
		switch e := ev.(type) {
		case wire.Ext:
			if e.T == typ {
				raw = e.Raw
			}
		case wire.WorldChanged: // a type the library knows
			if typ == "world.changed" {
				raw, _ = json.Marshal(e.WorldChangedMsg)
			}
		}
		if raw == nil {
			return false
		}
		var m map[string]any
		_ = json.Unmarshal(raw, &m)
		if pred == nil || pred(m) {
			found = m
			return true
		}
		return false
	})
	return found
}

// noExt asserts that no Ext of the type arrives within d.
func noExt(t *testing.T, c *wire.Client, typ string, d time.Duration) {
	t.Helper()
	deadline := time.After(d)
	for {
		select {
		case ev := <-c.Events():
			if e, ok := ev.(wire.Ext); ok && e.T == typ {
				t.Fatalf("unexpected %s: %s", typ, e.Raw)
			}
			if e, ok := ev.(wire.WorldChanged); ok && typ == "world.changed" {
				t.Fatalf("unexpected %s: %+v", typ, e)
			}
		case <-deadline:
			return
		}
	}
}

func presenceOf(m map[string]any) map[string]map[string]any {
	out := map[string]map[string]any{}
	for _, p := range m["peers"].([]any) {
		e := p.(map[string]any)
		out[e["name"].(string)] = e
	}
	return out
}

func fastTimers(t *testing.T) {
	a, b, c := timing.typingTTL.Load(), timing.typingMinGap.Load(), timing.nawsDebounce.Load()
	timing.typingTTL.Store(int64(250 * time.Millisecond))
	timing.typingMinGap.Store(int64(20 * time.Millisecond))
	timing.nawsDebounce.Store(int64(40 * time.Millisecond))
	t.Cleanup(func() { timing.typingTTL.Store(a); timing.typingMinGap.Store(b); timing.nawsDebounce.Store(c) })
}

func joinLines(ss []string) string { return strings.Join(ss, "|") }
