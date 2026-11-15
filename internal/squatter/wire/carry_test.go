// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package wire

import (
	"context"
	"encoding/json"
	"net"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func (r *rig) onlyPeer() Peer {
	r.t.Helper()
	var p Peer
	waitFor(r.t, "the peer to be registered", func() bool {
		r.fb.mu.Lock()
		defer r.fb.mu.Unlock()
		if len(r.fb.peers) == 1 {
			p = r.fb.peers[0]
		}
		return p != nil
	})
	return p
}

func asTransport(_ uint64, nc net.Conn) (any, error) { return nc, nil }

func sent(fb *fakeBackend) string {
	var s []string
	for _, r := range fb.records() {
		if strings.HasPrefix(r, "send ") {
			s = append(s, r)
		}
	}
	return strings.Join(s, ",")
}

// A quiesced server does not read: what the client sends meanwhile waits in the connection and is
// handled once, after Resume; what was queued for the client is written once, after Resume.
func TestQuiesceHoldsInputAndOutputUntilResume(t *testing.T) {
	r := newRig(t, nil)
	c, err := r.dial(ClientOptions{DeviceName: "t"})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	p := r.onlyPeer()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = c.Send(ctx, "s", "before")
	waitFor(t, "the first send", func() bool { return sent(r.fb) == "send s before" })
	if dropped := r.srv.Quiesce(5*time.Second, nil); len(dropped) != 0 {
		t.Fatalf("dropped %v", dropped)
	}
	_ = c.Send(ctx, "s", "during")
	p.Send(map[string]any{"v": 1, "t": "x.queued"})
	ex := r.srv.Export(asTransport)
	if len(ex) != 1 || ex[0].State.ID != p.ID() || ex[0].State.Device != p.DeviceID() {
		t.Fatalf("export: %+v", ex)
	}
	if len(ex[0].State.Out) != 1 || !strings.Contains(string(ex[0].State.Out[0]), "x.queued") {
		t.Fatalf("the queued frame is not in the state: %q", ex[0].State.Out)
	}
	// the state is JSON that survives a round trip
	b, err := json.Marshal(ex[0].State)
	if err != nil {
		t.Fatal(err)
	}
	var st ClientState
	if err := json.Unmarshal(b, &st); err != nil || st.Check() != nil {
		t.Fatalf("%v %v", err, st.Check())
	}
	// nothing moved while stopped
	if got := sent(r.fb); got != "send s before" {
		t.Fatalf("the backend heard %q while quiesced", got)
	}
	r.srv.Resume()
	waitFor(t, "the held send", func() bool { return sent(r.fb) == "send s before,send s during" })
	var sawQueued bool
	for !sawQueued {
		select {
		case ev := <-c.Events():
			_ = ev
			sawQueued = true // the frame is an unknown type: the client reports it as it can; any event is the proof here
		case <-time.After(5 * time.Second):
			t.Fatal("the frame queued before Resume never arrived")
		}
	}
	_ = c.Send(ctx, "s", "after")
	waitFor(t, "send after resume", func() bool { return sent(r.fb) == "send s before,send s during,send s after" })
}

// The client carries on over the same connection with a second server that adopts it, and the first
// server says nothing on the connection when it lets go.
func TestExportAdoptCarriesTheConnection(t *testing.T) {
	r := newRig(t, nil)
	c, err := r.dial(ClientOptions{DeviceName: "t"})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	p := r.onlyPeer()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = c.Send(ctx, "s", "one")
	waitFor(t, "one", func() bool { return sent(r.fb) == "send s one" })
	if d := r.srv.Quiesce(5*time.Second, nil); len(d) != 0 {
		t.Fatalf("dropped %v", d)
	}
	_ = c.Send(ctx, "s", "two") // sent while the old server stands still: waits in the connection
	ex := r.srv.Export(asTransport)
	if len(ex) != 1 {
		t.Fatalf("export: %d", len(ex))
	}
	st, nc := ex[0].State, ex[0].Transport.(net.Conn)
	r.srv.Abandon()
	// the new server: same backend and auth, a fresh Server; the client is adopted over the same socket
	srv2 := NewServer(ServerConfig{Backend: r.fb, Auth: r.as, ServerName: "squatterd/test"})
	defer srv2.Close()
	np, err := srv2.Adopt(st, nc)
	if err != nil {
		t.Fatal(err)
	}
	if np.ID() != p.ID() || np.DeviceID() != p.DeviceID() || np.DeviceName() != p.DeviceName() {
		t.Fatalf("identity changed: %d %s -> %d %s", p.ID(), p.DeviceName(), np.ID(), np.DeviceName())
	}
	waitFor(t, "the request that waited", func() bool { return sent(r.fb) == "send s one,send s two" })
	_ = c.Send(ctx, "s", "three")
	waitFor(t, "a request after the adoption", func() bool { return sent(r.fb) == "send s one,send s two,send s three" })
	if l, err := c.List(ctx); err != nil || len(l) != 2 {
		t.Fatalf("request/response over the adopted connection: %v %v", l, err)
	}
	np.Send(map[string]any{"v": 1, "t": "session.removed", "session": "s-9"})
	select {
	case <-c.Events():
	case <-time.After(5 * time.Second):
		t.Fatal("nothing arrived from the adopting server")
	}
	select {
	case <-c.Done():
		t.Fatal("the client saw its connection end")
	default:
	}
	// ids of clients that connect later do not collide with the carried one
	c2, err := (&rig{t: t, fb: r.fb, as: r.as, srv: srv2, url: r.url}).dialTo(srv2, ClientOptions{DeviceName: "second"})
	if err != nil {
		t.Fatal(err)
	}
	defer c2.Close()
	waitFor(t, "second client", func() bool { return srv2.NumClients() == 2 })
	for id := range srv2.Conns() {
		if id == 0 {
			t.Fatal("zero id")
		}
	}
}

func (r *rig) dialTo(s *Server, opts ClientOptions) (*Client, error) {
	ts := httptest.NewServer(s.Handler())
	r.t.Cleanup(ts.Close)
	rr := *r
	rr.url = "ws" + strings.TrimPrefix(ts.URL, "http") + "/swp"
	return rr.dial(opts)
}

// Adopt starts from what an interrupted reader and writer had: a message read and not handled, bytes of a
// frame that had begun to arrive, and frames queued and not written. They come out in that order.
func TestAdoptStartsFromPendingMessageHalfFrameAndQueuedOutput(t *testing.T) {
	r := newRig(t, nil)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	cc := make(chan net.Conn, 1)
	go func() { c, _ := ln.Accept(); cc <- c }()
	cl, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()
	srvSide := <-cc
	msg := func(text string) []byte {
		b, _ := json.Marshal(map[string]any{"v": 1, "t": "send", "session": "s", "text": text})
		return b
	}
	half := cframe(opText, true, msg("third"))
	st := ClientState{V: ClientStateVersion, ID: 7, Device: "dev", Name: "name", Role: "operator",
		Pend: &PendMsg{Data: msg("first")},
		Buf:  append(cframe(opText, true, msg("second")), half[:5]...), // a whole frame and the first bytes of the next
		Out:  [][]byte{[]byte(`{"v":1,"t":"q1"}`), []byte(`{"v":1,"t":"q2"}`)}}
	p, err := r.srv.Adopt(st, srvSide)
	if err != nil {
		t.Fatal(err)
	}
	if p.ID() != 7 {
		t.Fatalf("id %d", p.ID())
	}
	cl.Write(half[5:]) // the rest of the third frame
	waitFor(t, "the three messages in order", func() bool { return sent(r.fb) == "send s first,send s second,send s third" })
	for _, want := range []string{"q1", "q2"} {
		op, pl := sframe(t, cl)
		if op != opText || !strings.Contains(string(pl), want) {
			t.Fatalf("queued frame: %d %s", op, pl)
		}
	}
	// refused states
	for name, bad := range map[string]ClientState{
		"version":  {V: 9, ID: 1, Device: "d"},
		"no id":    {V: 1, Device: "d"},
		"no dev":   {V: 1, ID: 1},
		"too much": {V: 1, ID: 1, Device: "d", Buf: make([]byte, 3*MaxFrameBytes)},
	} {
		if err := bad.Check(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	pa, pb := net.Pipe()
	defer pa.Close()
	defer pb.Close()
	if _, err := r.srv.Adopt(ClientState{V: 1, ID: 7, Device: "d"}, pa); err == nil || !strings.Contains(err.Error(), "in use") {
		t.Errorf("an id in use: %v", err)
	}
}
