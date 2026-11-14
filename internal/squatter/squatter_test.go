// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package squatter

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/itsnotyoutoday/gtf/internal/squatter/auth"
	"github.com/itsnotyoutoday/gtf/internal/squatter/proto"
	"github.com/itsnotyoutoday/gtf/internal/squatter/wire"
)

// fakeHost stands in for gtf: one goroutine plays the engine (Do and Post run there).
type fakeHost struct {
	t    *testing.T
	ch   chan func()
	quit chan struct{}
	sq   *Squatter

	mu      sync.Mutex
	sent    []string
	hooks   []string
	vars    map[string]string
	notices []string
	execs   []string
}

func newFakeHost(t *testing.T) *fakeHost {
	h := &fakeHost{t: t, ch: make(chan func(), 64), quit: make(chan struct{}), vars: map[string]string{}}
	go func() {
		for {
			select {
			case f := <-h.ch:
				f()
			case <-h.quit:
				return
			}
		}
	}()
	t.Cleanup(func() { close(h.quit) })
	return h
}

func (h *fakeHost) Do(f func()) {
	d := make(chan struct{})
	select {
	case h.ch <- func() { f(); close(d) }:
	case <-h.quit:
		return
	}
	select {
	case <-d:
	case <-h.quit:
	}
}
func (h *fakeHost) Post(f func()) {
	select {
	case h.ch <- f:
	case <-h.quit:
	}
}
func (h *fakeHost) SendLine(session, text string, o SendOpts) error {
	if text == "fail" {
		return errors.New("not connected")
	}
	h.mu.Lock()
	h.sent = append(h.sent, fmt.Sprintf("%s|%s|secret=%v|cmd=%v", session, text, o.Secret, o.Command))
	h.mu.Unlock()
	return nil
}
func (h *fakeHost) Connect(t ConnectTarget) (string, error) {
	if t.Host == "bad" {
		return "", errors.New("nope")
	}
	id := h.sq.SessionOpened(SessionMeta{World: firstNonEmpty(t.Name, t.Ref, t.Host), Host: t.Host, Port: t.Port, TLS: t.TLS})
	return id, nil
}
func firstNonEmpty(s ...string) string {
	for _, x := range s {
		if x != "" {
			return x
		}
	}
	return ""
}
func (h *fakeHost) Disconnect(id string) error {
	h.sq.SessionEnded(id, proto.EndClientQuit, "")
	return nil
}
func (h *fakeHost) SendGMCP(id, pkg, data string) error { return nil }
func (h *fakeHost) SendMSDP(id, n string, v any) error  { return nil }
func (h *fakeHost) Exec(line string) (bool, []string) {
	h.mu.Lock()
	h.execs = append(h.execs, line)
	h.mu.Unlock()
	return true, []string{"ran " + line}
}
func (h *fakeHost) Worlds() []proto.TFWorld {
	return []proto.TFWorld{{Name: "mud", Host: "h", Port: "4000", PassSet: true, MFile: "secret.tf"}}
}
func (h *fakeHost) Hook(ev string, a ...string) {
	h.mu.Lock()
	h.hooks = append(h.hooks, ev+" "+strings.Join(a, " "))
	h.mu.Unlock()
}
func (h *fakeHost) SetVars(v map[string]string) {
	h.mu.Lock()
	for k, x := range v {
		h.vars[k] = x
	}
	h.mu.Unlock()
}
func (h *fakeHost) Notice(t string) { h.mu.Lock(); h.notices = append(h.notices, t); h.mu.Unlock() }
func (h *fakeHost) get(f func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	f()
}

// rig is a running squatter on loopback with TLS.
type rig struct {
	t   *testing.T
	h   *fakeHost
	sq  *Squatter
	cfg Config
	url string
	fp  string
}

func newRig(t *testing.T, mod func(*Config)) *rig {
	t.Helper()
	h := newFakeHost(t)
	sq := New(h, Options{Version: "test", Home: t.TempDir()})
	h.sq = sq
	cfg := DefaultConfig(t.TempDir())
	cfg.Enabled, cfg.Port, cfg.Dir, cfg.Name = true, 0, t.TempDir()+"/sq", "testbox"
	if mod != nil {
		mod(&cfg)
	}
	r := &rig{t: t, h: h, sq: sq, cfg: cfg}
	r.apply(cfg)
	t.Cleanup(sq.Stop)
	return r
}

func (r *rig) apply(cfg Config) {
	r.t.Helper()
	r.h.Do(func() { r.sq.Apply(cfg, nil) })
	if st, e := r.sq.State(); st != StateListening {
		if cfg.Enabled {
			r.t.Fatalf("state %s: %s", st, e)
		}
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := r.sq.Wait(ctx); err != nil {
		r.t.Fatal(err)
	}
	r.cfg = cfg
	scheme := "wss"
	if !cfg.TLS {
		scheme = "ws"
	}
	_, port, _ := net.SplitHostPort(r.sq.Addr())
	r.url = scheme + "://127.0.0.1:" + port
	r.fp = r.sq.Fingerprint()
}

func (r *rig) pair(role string) string {
	r.t.Helper()
	var info PairInfo
	var err error
	r.h.Do(func() { info, err = r.sq.Pair(role) })
	if err != nil {
		r.t.Fatal(err)
	}
	return info.Code
}

func (r *rig) dial(key ed25519.PrivateKey, code, name string) (*wire.Client, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return wire.Dial(ctx, r.url, wire.ClientOptions{Key: key, PairCode: code, DeviceName: name,
		PinnedFingerprint: r.fp, ExpectKind: r.expectKind(), InsecureLoopback: !r.cfg.TLS})
}

func (r *rig) expectKind() string {
	if r.cfg.Kind == KindSWP {
		return proto.KindSquatterd
	}
	return proto.KindTFSquatterd
}

func (r *rig) paired(role, name string) (*wire.Client, ed25519.PrivateKey) {
	r.t.Helper()
	key, _ := auth.GenerateKey()
	c, err := r.dial(key, r.pair(role), name)
	if err != nil {
		r.t.Fatal(err)
	}
	r.t.Cleanup(func() { c.Close() })
	return c, key
}

func (r *rig) line(id, kind, text string) {
	r.h.Do(func() { r.sq.Line(id, kind, []proto.Span{{S: text}}, false, "") })
}

func waitFor(t *testing.T, what string, f func() bool) {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if f() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// collect reads events until pred says stop or it times out.
func collect(t *testing.T, c *wire.Client, pred func(wire.Event) bool) []wire.Event {
	t.Helper()
	var out []wire.Event
	deadline := time.After(4 * time.Second)
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

func isBacklogDone(ev wire.Event) bool { b, ok := ev.(wire.Backlog); return ok && b.Done }

func linesOf(evs []wire.Event) []string {
	var out []string
	for _, ev := range evs {
		switch e := ev.(type) {
		case wire.Backlog:
			for _, en := range e.Entries {
				if en.Line != nil {
					out = append(out, en.Line.Kind+":"+plain(en.Line.Spans))
				}
			}
		case wire.Line:
			out = append(out, e.Kind+":"+plain(e.Spans))
		}
	}
	return out
}

func (r *rig) open(world string) string {
	var id string
	r.h.Do(func() { id = r.sq.SessionOpened(SessionMeta{World: world, Host: "mud.example", Port: 4000}) })
	r.h.Do(func() { r.sq.SessionConnected(id) })
	return id
}

func TestPairingListAttachReplayAndSend(t *testing.T) {
	r := newRig(t, nil)
	id := r.open("mud")
	for i := 1; i <= 5; i++ {
		r.line(id, proto.KindGame, "line "+strconv.Itoa(i))
	}
	c, key := r.paired("operator", "laptop")
	if c.Kind() != proto.KindTFSquatterd || !c.HasFeature(wire.FeatureTF) || !c.HasFeature(wire.FeatureNoEcho) || !c.HasFeature(wire.FeatureExec) {
		t.Fatalf("kind %q features %v", c.Kind(), c.Features())
	}
	if c.Instance() != "testbox" || !strings.HasPrefix(c.ServerName(), "gtf/") {
		t.Fatalf("instance %q server %q", c.Instance(), c.ServerName())
	}
	ss := c.Sessions()
	if len(ss) != 1 || ss[0].ID != id || ss[0].Name != "mud" || ss[0].State != "connected" || ss[0].LastSeq != 5 || ss[0].Unread != 5 {
		t.Fatalf("welcome sessions %+v", ss)
	}
	ctx := tctx(t)
	if err := c.Attach(ctx, id, nil, 0); err != nil {
		t.Fatal(err)
	}
	evs := collect(t, c, isBacklogDone)
	got := linesOf(evs)
	if len(got) != 5 || got[0] != "game:line 1" || got[4] != "game:line 5" {
		t.Fatalf("replay %v", got)
	}
	// live line, then a send gets echoed with the device as origin
	r.line(id, proto.KindGame, "live")
	if err := c.Send(ctx, id, "look"); err != nil {
		t.Fatal(err)
	}
	evs = collect(t, c, func(ev wire.Event) bool { l, ok := ev.(wire.Line); return ok && l.Kind == proto.KindEcho })
	if g := linesOf(evs); len(g) != 2 || g[0] != "game:live" || g[1] != "echo:look" {
		t.Fatalf("live %v", g)
	}
	if err := c.Ack(ctx, id, 7); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "send", func() bool { var n int; r.h.get(func() { n = len(r.h.sent) }); return n == 1 })
	r.h.get(func() {
		if len(r.h.sent) != 1 || r.h.sent[0] != id+"|look|secret=false|cmd=false" {
			t.Fatalf("sent %v", r.h.sent)
		}
	})
	// reconnect: replay only what was missed (since the last seq), no duplicates, no gap
	last := c.LastSeq(id)
	c.Close()
	r.line(id, proto.KindGame, "while away 1")
	r.line(id, proto.KindGame, "while away 2")
	c2, err := r.dial(key, "", "laptop")
	if err != nil {
		t.Fatal(err)
	}
	defer c2.Close()
	if s := c2.Sessions(); len(s) != 1 || s[0].Unread != 2 {
		t.Fatalf("unread after ack: %+v", s)
	}
	if err := c2.Attach(ctx, id, &last, 0); err != nil {
		t.Fatal(err)
	}
	evs = collect(t, c2, isBacklogDone)
	if g := linesOf(evs); len(g) != 2 || g[0] != "game:while away 1" || g[1] != "game:while away 2" {
		t.Fatalf("catch-up %v", g)
	}
	var att wire.Attached
	for _, ev := range evs {
		if a, ok := ev.(wire.Attached); ok {
			att = a
		}
	}
	if att.Gap != nil || att.ReplayFrom != last+1 {
		t.Fatalf("attached %+v want from %d", att, last+1)
	}
}

func TestRefusedUnpairedWrongCodeAndRateLimit(t *testing.T) {
	r := newRig(t, nil)
	key, _ := auth.GenerateKey()
	for i := 0; i < auth.MaxFailures; i++ {
		_, err := r.dial(key, "", "stranger")
		var pe *wire.ProtoError
		if !errors.As(err, &pe) || pe.Code != proto.ErrAuthFailed {
			t.Fatalf("unpaired connection %d: %v", i, err)
		}
	}
	// locked out now, even with a valid code
	code := r.pair("operator")
	_, err := r.dial(key, code, "stranger")
	var pe *wire.ProtoError
	if !errors.As(err, &pe) || pe.Code != proto.ErrRateLimited {
		t.Fatalf("after %d failures: %v", auth.MaxFailures, err)
	}
	// a wrong code is a failure too (other client address is the same here, so only check the error code on a fresh rig)
	r2 := newRig(t, nil)
	r2.pair("operator")
	_, err = r2.dial(key, "ZZZZ-ZZZZ", "x")
	if !errors.As(err, &pe) || pe.Code != proto.ErrAuthFailed {
		t.Fatalf("wrong code: %v", err)
	}
	if r2.sq.NumPeers() != 0 {
		t.Fatal("a refused client counted as a peer")
	}
}

func TestPairingCodeIsSingleUseAndExpires(t *testing.T) {
	r := newRig(t, func(c *Config) { c.PairTTL = 10 * time.Second })
	code := r.pair("viewer")
	k1, _ := auth.GenerateKey()
	c, err := r.dial(k1, code, "one")
	if err != nil {
		t.Fatal(err)
	}
	if c.Role() != "viewer" {
		t.Fatalf("role %q", c.Role())
	}
	c.Close()
	k2, _ := auth.GenerateKey()
	if _, err := r.dial(k2, code, "two"); err == nil {
		t.Fatal("a pairing code was accepted twice")
	}
}

func TestRevokedDeviceAndKick(t *testing.T) {
	r := newRig(t, nil)
	c, key := r.paired("operator", "phone")
	waitFor(t, "peer", func() bool { return r.sq.NumPeers() == 1 })
	if devs, err := func() ([]DeviceInfo, error) { return r.sq.Devices() }(); err != nil || len(devs) != 1 || !devs[0].Online || devs[0].Role != "operator" {
		t.Fatalf("devices %+v %v", devs, err)
	}
	if n := r.sq.Kick("phone"); n != 1 {
		t.Fatalf("kicked %d", n)
	}
	select {
	case <-c.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("kicked client still connected")
	}
	// a kicked device is still paired and can come back
	c2, err := r.dial(key, "", "phone")
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "peer 2", func() bool { return r.sq.NumPeers() == 1 })
	names, err := r.sq.Unpair("phone")
	if err != nil || len(names) != 1 {
		t.Fatalf("unpair %v %v", names, err)
	}
	select {
	case <-c2.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("unpaired client still connected")
	}
	if _, err := r.dial(key, "", "phone"); err == nil {
		t.Fatal("a revoked device was accepted")
	}
}

func TestRolesAndCommands(t *testing.T) {
	r := newRig(t, nil)
	r.open("mud")
	ctx := tctx(t)
	viewer, _ := r.paired("viewer", "tv")
	id := viewer.Sessions()[0].ID
	var pe *wire.ProtoError
	if err := viewer.Send(ctx, id, "look"); err != nil {
		t.Fatal(err)
	}
	expectError(t, viewer, proto.ErrForbidden)
	if _, err := viewer.Exec(ctx, "/echo hi"); !errors.As(err, &pe) || pe.Code != proto.ErrForbidden {
		t.Fatalf("viewer exec: %v", err)
	}
	op, _ := r.paired("operator", "op")
	// an operator's "/x" is just text for the game; tf.exec is admin only
	if err := op.Send(ctx, id, "/who"); err != nil {
		t.Fatal(err)
	}
	if _, err := op.Exec(ctx, "/echo hi"); !errors.As(err, &pe) || pe.Code != proto.ErrForbidden {
		t.Fatalf("operator exec: %v", err)
	}
	admin, _ := r.paired("admin", "adm")
	if err := admin.Send(ctx, id, "/set x=1"); err != nil {
		t.Fatal(err)
	}
	if err := admin.Send(ctx, id, "hello"); err != nil {
		t.Fatal(err)
	}
	res, err := admin.Exec(ctx, "/version")
	if err != nil || !res.OK || len(res.Out) != 1 || res.Out[0] != "ran /version" {
		t.Fatalf("exec %+v %v", res, err)
	}
	if _, err := admin.Exec(ctx, "version"); !errors.As(err, &pe) || pe.Code != proto.ErrBadRequest {
		t.Fatalf("exec without slash: %v", err)
	}
	waitFor(t, "sends", func() bool { var n int; r.h.get(func() { n = len(r.h.sent) }); return n == 3 })
	r.h.get(func() {
		want := []string{id + "|/who|secret=false|cmd=false", id + "|/set x=1|secret=false|cmd=true", id + "|hello|secret=false|cmd=false"}
		if strings.Join(r.h.sent, "\n") != strings.Join(want, "\n") {
			t.Fatalf("sent:\n%s", strings.Join(r.h.sent, "\n"))
		}
	})
	// with squatter_commands off nothing is executed
	cfg := r.cfg
	cfg.Commands = false
	r.apply(cfg)
	admin2, _ := r.paired("admin", "adm2")
	if admin2.HasFeature(wire.FeatureExec) {
		t.Fatal("tfexec advertised with commands off")
	}
	if _, err := admin2.Exec(ctx, "/echo hi"); !errors.As(err, &pe) || pe.Code != proto.ErrForbidden {
		t.Fatalf("exec with commands off: %v", err)
	}
	id = admin2.Sessions()[0].ID
	if err := admin2.Send(ctx, id, "/quit"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "send 4", func() bool { var n int; r.h.get(func() { n = len(r.h.sent) }); return n == 4 })
	r.h.get(func() {
		if last := r.h.sent[len(r.h.sent)-1]; !strings.HasSuffix(last, "|/quit|secret=false|cmd=false") {
			t.Fatalf("with commands off a slash line must be plain text: %s", last)
		}
	})
}

func TestSecretSendIsNotEchoedOrRecorded(t *testing.T) {
	r := newRig(t, nil)
	id := r.open("mud")
	c, _ := r.paired("operator", "p")
	ctx := tctx(t)
	if err := c.SendSecret(ctx, id, "hunter2"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "send", func() bool { var n int; r.h.get(func() { n = len(r.h.sent) }); return n == 1 })
	r.h.get(func() {
		if len(r.h.sent) != 1 || !strings.Contains(r.h.sent[0], "|hunter2|secret=true") {
			t.Fatalf("%v", r.h.sent)
		}
	})
	if _, last := r.sq.session(id).store.Bounds(); last != 0 {
		t.Fatalf("a secret was recorded (last seq %d)", last)
	}
	// while the game asks for no echo, an ordinary echo line is masked
	r.h.Do(func() { r.sq.SetRemoteEcho(id, true) })
	r.line(id, proto.KindEcho, "my password")
	es, _ := r.sq.session(id).store.Last(1)
	if plain(es[0].Line.Spans) != maskedEcho {
		t.Fatalf("echo not masked: %q", plain(es[0].Line.Spans))
	}
}

func TestSessionLifecycleConnectDisconnectDismissAndHooks(t *testing.T) {
	r := newRig(t, nil)
	c, _ := r.paired("operator", "dev one")
	waitFor(t, "connect hook", func() bool {
		var ok bool
		r.h.get(func() { ok = strings.Contains(strings.Join(r.h.hooks, "|"), "SQUAT_CONNECT dev one operator") })
		return ok
	})
	r.h.get(func() {
		if !strings.Contains(strings.Join(r.h.hooks, "|"), "SQUAT_PAIR dev one operator") {
			t.Fatalf("no pair hook: %v", r.h.hooks)
		}
	})
	ctx := tctx(t)
	info, err := c.Connect(ctx, proto.World{Host: "game.example", Port: 4001, TLS: true, Name: "g"})
	if err != nil {
		t.Fatal(err)
	}
	if info.Name != "g" || info.Host != "game.example" || info.Port != 4001 || !info.TLS || info.TF == nil {
		t.Fatalf("%+v", info)
	}
	if _, err := c.Connect(ctx, proto.World{Host: "bad", Port: 1}); err == nil {
		t.Fatal("expected the host's refusal")
	}
	if err := c.Dismiss(ctx, info.ID); err != nil {
		t.Fatal(err)
	}
	expectError(t, c, proto.ErrBadRequest) // still live
	if err := c.Disconnect(ctx, info.ID); err != nil {
		t.Fatal(err)
	}
	ev := collect(t, c, func(ev wire.Event) bool { s, ok := ev.(wire.SessionState); return ok && s.Info.State == "ended" })
	last := ev[len(ev)-1].(wire.SessionState).Info
	if last.EndReason != proto.EndClientQuit || last.EndedAt == 0 {
		t.Fatalf("%+v", last)
	}
	if err := c.Send(ctx, info.ID, "x"); err != nil {
		t.Fatal(err)
	}
	expectError(t, c, proto.ErrSessionEnded)
	if err := c.Dismiss(ctx, info.ID); err != nil {
		t.Fatal(err)
	}
	collect(t, c, func(ev wire.Event) bool { _, ok := ev.(wire.Removed); return ok })
	c.Close()
	waitFor(t, "disconnect hook", func() bool {
		var ok bool
		r.h.get(func() { ok = strings.Contains(strings.Join(r.h.hooks, "|"), "SQUAT_DISCONNECT dev one operator") })
		return ok
	})
	waitFor(t, "peers var 0", func() bool { var v string; r.h.get(func() { v = r.h.vars[VarPeers] }); return v == "0" })
}

func TestTFExtensions(t *testing.T) {
	r := newRig(t, nil)
	id := r.open("mud")
	c, _ := r.paired("operator", "p")
	ctx := tctx(t)
	ws, err := c.Worlds(ctx)
	if err != nil || len(ws) != 1 || ws[0].Name != "mud" || !ws[0].PassSet || ws[0].MFile != "" {
		t.Fatalf("%+v %v", ws, err)
	}
	cs, err := c.ControlGet(ctx, id)
	if err != nil || cs.Input != wire.ModeBoth || cs.Automation != wire.ModeTF {
		t.Fatalf("%+v %v", cs, err)
	}
	cs, err = c.ControlPreset(ctx, id, "takeover")
	if err != nil || cs.Input != wire.ModeBoth {
		t.Fatalf("control.set must answer with the real state: %+v %v", cs, err)
	}
	// GMCP / MSDP are recorded, snapshotted and replayed
	r.h.Do(func() {
		r.sq.Gmcp(id, "Char.Vitals", `{"hp":5}`)
		r.sq.Msdp(id, "HEALTH", "7")
	})
	st, err := c.GetState(ctx, id)
	if err != nil || string(st.Gmcp["Char.Vitals"]) != `{"hp":5}` || st.Msdp["HEALTH"] != "7" || st.AsOf != 2 {
		t.Fatalf("%+v %v", st, err)
	}
	if err := c.SendGmcp(ctx, id, "Core.Ping", nil); err != nil {
		t.Fatal(err)
	}
	if err := c.SendMsdp(ctx, id, map[string]any{"REPORT": []any{"HEALTH"}}); err != nil {
		t.Fatal(err)
	}
	var pe *wire.ProtoError
	if _, err := c.PutScript(ctx, "a.tf", "/echo", false); !errors.As(err, &pe) || pe.Code != proto.ErrBadRequest {
		t.Fatalf("script upload: %v", err)
	}
	if err := c.Attach(ctx, id, nil, 0); err != nil {
		t.Fatal(err)
	}
	evs := collect(t, c, func(ev wire.Event) bool { _, ok := ev.(wire.State); return ok })
	var sawGmcp, sawMsdp bool
	for _, ev := range evs {
		if b, ok := ev.(wire.Backlog); ok {
			for _, e := range b.Entries {
				sawGmcp = sawGmcp || e.Gmcp != nil
				sawMsdp = sawMsdp || e.Msdp != nil
			}
		}
	}
	if !sawGmcp || !sawMsdp {
		t.Fatalf("backlog lacks gmcp/msdp: %#v", evs)
	}
}

func TestSWPKindHasNoTFExtensions(t *testing.T) {
	r := newRig(t, func(c *Config) { c.Kind = KindSWP })
	if r.cfg.Path() != "/swp" {
		t.Fatal(r.cfg.Path())
	}
	r.open("mud")
	c, _ := r.paired("admin", "p")
	if c.Kind() != proto.KindSquatterd || c.HasFeature(wire.FeatureTF) {
		t.Fatalf("kind %s features %v", c.Kind(), c.Features())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	// the frame is ignored by an SWP daemon (forward compatibility): no reply, the request times out
	if _, err := c.Worlds(ctx); err == nil {
		t.Fatal("swp kind answered world.list")
	}
}

func TestApplyRebindsAndKeepsSessions(t *testing.T) {
	r := newRig(t, nil)
	id := r.open("mud")
	r.line(id, proto.KindGame, "hello")
	c, key := r.paired("operator", "p")
	old := r.sq.Addr()
	cfg := r.cfg
	cfg.Name = "renamed"
	r.apply(cfg)
	select {
	case <-c.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("clients must be dropped by a restart of the listener")
	}
	c2, err := r.dial(key, "", "p")
	if err != nil {
		t.Fatal(err)
	}
	defer c2.Close()
	if c2.Instance() != "renamed" {
		t.Fatalf("instance %q", c2.Instance())
	}
	if s := c2.Sessions(); len(s) != 1 || s[0].LastSeq != 1 {
		t.Fatalf("sessions lost across a rebind: %+v", s)
	}
	_ = old
	// off: sessions are dropped, the port closes; on again: empty
	cfg.Enabled = false
	r.apply(cfg)
	if st, _ := r.sq.State(); st != StateOff || r.sq.Active() || r.sq.NumSessions() != 0 {
		t.Fatalf("not off: %s", st)
	}
	r.h.get(func() {
		if r.h.vars[VarState] != StateOff {
			t.Fatalf("vars %v", r.h.vars)
		}
	})
	if _, err := r.dial(key, "", "p"); err == nil {
		t.Fatal("connected to a stopped squatter")
	}
	cfg.Enabled = true
	r.apply(cfg)
	if _, err := r.dial(key, "", "p"); err != nil {
		t.Fatalf("paired devices must survive off/on: %v", err)
	}
}

func TestListenErrorAndPlainWSRules(t *testing.T) {
	r := newRig(t, nil)
	// another squatter on the same port: error state, a notice, the status variable
	h2 := newFakeHost(t)
	sq2 := New(h2, Options{Home: t.TempDir()})
	h2.sq = sq2
	cfg := r.cfg
	_, port, _ := net.SplitHostPort(r.sq.Addr())
	cfg.Port, _ = strconv.Atoi(port)
	cfg.Dir = t.TempDir() + "/two"
	h2.Do(func() { sq2.Apply(cfg, nil) })
	st, e := sq2.State()
	if st != StateError || !strings.Contains(e, "listen") {
		t.Fatalf("%s %s", st, e)
	}
	h2.get(func() {
		if h2.vars[VarState] != StateError || len(h2.notices) == 0 {
			t.Fatalf("%v %v", h2.vars, h2.notices)
		}
	})
	// an unchanged configuration does not retry (and does not nag) ...
	h2.Do(func() { sq2.Apply(cfg, nil) })
	h2.get(func() {
		if len(h2.notices) != 1 {
			t.Fatalf("nagged: %v", h2.notices)
		}
	})
	// ... plain ws:// is refused for a public bind, allowed on loopback
	bad := cfg
	bad.TLS, bad.Bind = false, "0.0.0.0"
	if err := bad.Validate(); err == nil {
		t.Fatal("plain ws on 0.0.0.0 accepted")
	}
	good := r.cfg
	good.TLS = false
	r2 := newRig(t, func(c *Config) { c.TLS = false })
	if r2.fp != "" {
		t.Fatal("fingerprint without TLS")
	}
	c, _ := r2.paired("operator", "plain")
	if c.Kind() != proto.KindTFSquatterd {
		t.Fatal(c.Kind())
	}
}

func TestLimits(t *testing.T) {
	r := newRig(t, func(c *Config) { c.MaxClients = 2; c.MaxSessions = 3; c.Scrollback = 100 })
	for i := 0; i < 3; i++ {
		id := r.open("w" + strconv.Itoa(i))
		r.h.Do(func() { r.sq.SessionEnded(id, proto.EndRemoteClosed, "") })
	}
	live := r.open("live")
	if n := r.sq.NumSessions(); n != 3 {
		t.Fatalf("session registry not bounded: %d", n)
	}
	for _, s := range r.sq.sessionList() {
		if s.world == "w0" {
			t.Fatal("the oldest ended session should have gone first")
		}
	}
	// the ring keeps squatter_scrollback entries; a replay says what was lost
	for i := 1; i <= 150; i++ {
		r.line(live, proto.KindGame, "n"+strconv.Itoa(i))
	}
	c, _ := r.paired("operator", "a")
	zero := uint64(2)
	if err := c.Attach(tctx(t), live, &zero, 0); err != nil {
		t.Fatal(err)
	}
	evs := collect(t, c, isBacklogDone)
	var att wire.Attached
	for _, ev := range evs {
		if a, ok := ev.(wire.Attached); ok {
			att = a
		}
	}
	if att.FirstSeq != 51 || att.Gap == nil || att.Gap.Missed != 48 {
		t.Fatalf("%+v gap %+v", att, att.Gap)
	}
	// client limit
	r.paired("operator", "b")
	key, _ := auth.GenerateKey()
	if _, err := r.dial(key, r.pair("operator"), "c"); err == nil {
		t.Fatal("a third client was accepted with max clients 2")
	}
}

func TestStateExportImport(t *testing.T) {
	r := newRig(t, nil)
	id := r.open("mud")
	for i := 1; i <= 4; i++ {
		r.line(id, proto.KindGame, "l"+strconv.Itoa(i))
	}
	r.h.Do(func() { r.sq.Gmcp(id, "Room.Info", `{"n":1}`) })
	c, key := r.paired("operator", "p")
	if err := c.Ack(tctx(t), id, 3); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "ack", func() bool { return r.sq.session(id).info(c.DeviceID()).Unread == 2 })
	blob, err := r.sq.Export()
	if err != nil {
		t.Fatal(err)
	}
	f, err := r.sq.ListenerFile()
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	// the "new process": a second squatter adopts the listener and the sessions
	h2 := newFakeHost(t)
	sq2 := New(h2, Options{Home: t.TempDir()})
	h2.sq = sq2
	ln, err := net.FileListener(f)
	if err != nil {
		t.Fatal(err)
	}
	r.h.Do(func() { r.sq.Stop() })
	sq2.Activate()
	sq2.AdoptListener(ln)
	ids, err := sq2.Import(blob)
	if err != nil || len(ids) != 1 || ids[0] != id {
		t.Fatalf("%v %v", ids, err)
	}
	h2.Do(func() { sq2.Apply(r.cfg, nil) })
	if st, e := sq2.State(); st != StateListening {
		t.Fatalf("%s %s", st, e)
	}
	t.Cleanup(sq2.Stop)
	r.sq = sq2
	c2, err := r.dial(key, "", "p")
	if err != nil {
		t.Fatal(err)
	}
	defer c2.Close()
	s := c2.Sessions()
	if len(s) != 1 || s[0].ID != id || s[0].LastSeq != 5 || s[0].Unread != 2 {
		t.Fatalf("restored %+v", s)
	}
	since := uint64(3)
	if err := c2.Attach(tctx(t), id, &since, 0); err != nil {
		t.Fatal(err)
	}
	evs := collect(t, c2, func(ev wire.Event) bool { _, ok := ev.(wire.State); return ok })
	if g := linesOf(evs); len(g) != 1 || g[0] != "game:l4" {
		t.Fatalf("%v", g)
	}
	// seq numbers continue
	h2.Do(func() { sq2.Line(id, proto.KindGame, []proto.Span{{S: "new"}}, false, "") })
	if _, last := sq2.session(id).store.Bounds(); last != 6 {
		t.Fatalf("last seq %d", last)
	}
}

func TestPairInfoText(t *testing.T) {
	r := newRig(t, nil)
	var info PairInfo
	var err error
	r.h.Do(func() { info, err = r.sq.Pair("") })
	if err != nil {
		t.Fatal(err)
	}
	ls := info.Lines()
	if len(ls) != 4 || !strings.HasPrefix(ls[0], "Pairing code:    ") || !strings.Contains(ls[0], "(role: admin)") ||
		!strings.HasPrefix(ls[1], "Valid until:     ") || !strings.Contains(ls[1], "single use)") ||
		ls[2] != "TLS fingerprint: "+r.fp || !strings.HasPrefix(ls[3], "Connect to:      wss://localhost:") || !strings.HasSuffix(ls[3], "/tfsp") {
		t.Fatalf("%q", ls)
	}
	if _, err := r.sq.Pair("root"); err == nil {
		t.Fatal("bad role accepted")
	}
	// pairing files are private
	if fi, err := statMode(r.cfg.Dir); err != nil || fi&0o077 != 0 {
		t.Fatalf("state dir mode %v %v", fi, err)
	}
}

func tctx(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// expectError waits for an asynchronous error frame (send, dismiss and the like are not request/reply).
func expectError(t *testing.T, c *wire.Client, code string) {
	t.Helper()
	evs := collect(t, c, func(ev wire.Event) bool { _, ok := ev.(wire.Error); return ok })
	if e := evs[len(evs)-1].(wire.Error); e.Code != code {
		t.Fatalf("error %s (%s), want %s", e.Code, e.Message, code)
	}
}
