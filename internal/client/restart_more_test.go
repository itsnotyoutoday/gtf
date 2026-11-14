// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

//go:build darwin || dragonfly || freebsd || netbsd || openbsd || linux

package client

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net"
	"os"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/itsnotyoutoday/gtf/internal/handoff"
	gnet "github.com/itsnotyoutoday/gtf/internal/net"
	"github.com/itsnotyoutoday/gtfparser"
)

// tlsDialer connects every world to a local TLS server. migratable chooses the
// vendored client (carried across a restart) or the standard library's (not).
type tlsDialer struct {
	t          *testing.T
	ln         net.Listener
	migratable bool
	mu         sync.Mutex
	conns      []*tls.Conn
}

func newTLSDialer(t *testing.T, migratable bool) *tlsDialer {
	k, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "localhost"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
	der, _ := x509.CreateCertificate(rand.Reader, tpl, tpl, &k.PublicKey, k)
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: k}}})
	if err != nil {
		t.Fatal(err)
	}
	d := &tlsDialer{t: t, ln: ln, migratable: migratable}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				c.(*tls.Conn).Handshake()
				d.mu.Lock()
				d.conns = append(d.conns, c.(*tls.Conn))
				d.mu.Unlock()
			}()
		}
	}()
	return d
}

func (d *tlsDialer) Dial(ctx context.Context, w World, tr *DialTrace) (*gnet.Conn, error) {
	host, port, _ := net.SplitHostPort(d.ln.Addr().String())
	cfg := &tls.Config{InsecureSkipVerify: true}
	if d.migratable {
		return gnet.Dial(ctx, gnet.TLS{Timeout: 3 * time.Second, Config: cfg}, host, port, gnet.Options{})
	}
	raw, err := net.Dial("tcp", net.JoinHostPort(host, port))
	if err != nil {
		return nil, err
	}
	c := tls.Client(raw, cfg)
	if err := c.Handshake(); err != nil {
		return nil, err
	}
	return gnet.NewConn(c, gnet.Options{}), nil
}

func (d *tlsDialer) server(n int) *tls.Conn {
	for i := 0; i < 300; i++ {
		d.mu.Lock()
		if len(d.conns) >= n {
			c := d.conns[n-1]
			d.mu.Unlock()
			return c
		}
		d.mu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}
	d.t.Fatal("no TLS connection")
	return nil
}

func tlsRig(t *testing.T, migratable bool) (*rig, *tlsDialer) {
	r := newRig(t)
	d := newTLSDialer(t, migratable)
	r.a.cfg.Dialer = d
	r.dialer = d
	return r, d
}

func readN(t *testing.T, c io.Reader, want string) {
	t.Helper()
	buf := make([]byte, len(want))
	if cc, ok := c.(interface{ SetReadDeadline(time.Time) error }); ok {
		cc.SetReadDeadline(time.Now().Add(5 * time.Second))
	}
	if _, err := io.ReadFull(c, buf); err != nil || string(buf) != want {
		t.Fatalf("read %q err %v, want %q", buf, err, want)
	}
}

func TestRestartCarriesTLS(t *testing.T) {
	r, d := tlsRig(t, true)
	exec(r.a, "/addworld w localhost 4000", "/connect w")
	srv := d.server(1)
	srv.Write([]byte("secure hello\r\nhalf a l"))
	r.wait("secure hello")
	var plan RestartPlan
	r.a.Do(func(*gtfparser.Interp) { plan = r.a.RestartPlan() })
	if len(plan.Ends) != 0 || len(plan.Carried) != 1 || plan.Carried[0].Kind != "tls" {
		t.Fatalf("%+v", plan)
	}
	var seen []byte
	r.probe = func(req ProbeRequest) error {
		seen, _ = os.ReadFile(req.StatePath)
		return r.rehearse(req)
	}
	if err := r.restart(false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(seen), `"kind":"tls"`) {
		t.Fatal("the state file does not describe a TLS session")
	}
	srv.Write([]byte("ine\r\nafter restart\r\n"))
	r.wait("after restart")
	mustContain(t, r.scr, "half a line")
	b := r.newClient()
	b.Do(func(*gtfparser.Interp) { b.onKeys("look\r") })
	readN(t, srv, "look\r\n")
}

// A TLS session the vendored client did not make cannot be carried: Restart
// refuses unless forced, and then closes it and reconnects the world.
func TestRestartTLSNotCarriedIsRefusedThenReconnected(t *testing.T) {
	r, d := tlsRig(t, false)
	exec(r.a, "/addworld w localhost 4000", "/connect w")
	srv := d.server(1)
	srv.Write([]byte("hello\r\n"))
	r.wait("hello")
	err := r.restart(false)
	var ref *RestartRefused
	if !errors.As(err, &ref) || len(ref.Plan.Ends) != 1 || ref.Plan.Ends[0].Name != "w" {
		t.Fatalf("err = %v", err)
	}
	if r.newClient() != nil {
		t.Fatal("restarted")
	}
	srv.Write([]byte("still here\r\n")) // refusing changed nothing
	r.wait("still here")
	if err := r.restart(true); err != nil {
		t.Fatal(err)
	}
	r.wait("Reconnecting w after /restart")
	srv2 := d.server(2) // a second connection to the game
	srv2.Write([]byte("fresh connection\r\n"))
	r.wait("fresh connection")
	mustContain(t, r.scr, "hello") // the screen kept its history
}

// Layouts, panels and canvases come back; the screen is the same.
func TestRestartKeepsPanelsAndCanvases(t *testing.T) {
	r := newRig(t)
	srv := r.connect("w", "4000")
	srv.Send([]byte("some text\r\n"))
	exec(r.a, "/panel -dock left -w 20 -border single -title Info info = hello panel", "/canvas new cv 10 4")
	r.wait("hello panel")
	before := r.settled()
	if err := r.restart(false); err != nil {
		t.Fatal(err)
	}
	if after := r.settled(); after != before {
		t.Fatalf("before:\n%s\nafter:\n%s", before, after)
	}
	b := r.newClient()
	exec(b, "/canvas list", "/layout list")
	b.Flush()
	mustContain(t, r.scr, "canvas cv: 10x4", "default")
}

// A carrier registered by another component rides along: its data and its
// descriptor reach the new process; an unknown optional section is skipped and
// an unknown required one makes the new binary refuse, so the old one carries on.
type testCarrier struct {
	mu       sync.Mutex
	saved    int
	restored []string
	ends     bool
	active   bool
	rolled   int
}

func (c *testCarrier) Version() int   { return 2 }
func (c *testCarrier) Required() bool { return false }
func (c *testCarrier) RestartPlan(*Client) (car, ends []RestartItem) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.active {
		return nil, nil
	}
	if c.ends {
		return nil, []RestartItem{{Name: "xtest-listener", Reason: "cannot be carried today"}}
	}
	return []RestartItem{{Name: "xtest-listener", Kind: "pipe"}}, nil
}
func (c *testCarrier) RestartQuiesce(*Client) error { return nil }
func (c *testCarrier) RestartSave(_ *Client, w *RestartWriter) (any, error) {
	c.mu.Lock()
	act := c.active
	c.mu.Unlock()
	if !act {
		return nil, nil
	}
	rd, wr, _ := os.Pipe()
	wr.Write([]byte("through the pipe"))
	wr.Close()
	slot := w.AddFile("listener", rd)
	c.mu.Lock()
	c.saved++
	c.mu.Unlock()
	return map[string]any{"slot": slot, "n": 42}, nil
}
func (c *testCarrier) RestartRollback(*Client) { c.mu.Lock(); c.rolled++; c.mu.Unlock() }
func (c *testCarrier) RestartRestore(_ *Client, ctx *RestoreContext, v int, data json.RawMessage) error {
	if string(data) == "null" {
		return nil
	}
	var d struct{ Slot, N int }
	if err := json.Unmarshal(data, &d); err != nil {
		return err
	}
	f, err := ctx.File(d.Slot)
	if err != nil {
		return err
	}
	b := make([]byte, 16)
	n := 0
	if !ctx.Probe { // a rehearsal must not consume anything
		n, _ = f.Read(b)
	}
	c.mu.Lock()
	c.restored = append(c.restored, string(b[:n])+"/"+string(rune('0'+d.N/10))+"/probe="+map[bool]string{true: "y", false: "n"}[ctx.Probe])
	c.mu.Unlock()
	return nil
}

var xtest = &testCarrier{}
var xtestOnce sync.Once

func TestRestartCarrierSectionsAndVersionRules(t *testing.T) {
	xtestOnce.Do(func() { RegisterRestartSection("xtest", xtest) })
	xtest.mu.Lock()
	xtest.active, xtest.restored = true, nil
	xtest.mu.Unlock()
	defer func() { xtest.mu.Lock(); xtest.ends, xtest.active = false, false; xtest.mu.Unlock() }()
	r := newRig(t)
	srv := r.connect("w", "4000")
	_ = srv
	// a newer gtf's extra optional section is skipped; extra descriptors are listed by section and item
	r.probe = func(req ProbeRequest) error {
		r.mutate(req.StatePath, func(f *handoff.File) {
			f.Set("fromthefuture", 9, false, map[string]int{"x": 1})
			var fds []string
			for _, fd := range f.FDs {
				fds = append(fds, fd.Section+"/"+fd.Item)
			}
			if strings.Join(fds, ",") != "sessions/w,xtest/listener" {
				t.Errorf("descriptor owners: %v", fds)
			}
		})
		if err := r.rehearse(req); err != nil {
			return err
		}
		return errors.New("stop here") // keep this process: the point was the rehearsal
	}
	if err := r.restart(false); err == nil || !strings.Contains(err.Error(), "stop here") {
		t.Fatalf("err = %v", err)
	}
	xtest.mu.Lock()
	rs := append([]string(nil), xtest.restored...)
	xtest.mu.Unlock()
	if len(rs) != 1 || !strings.HasSuffix(rs[0], "probe=y") {
		t.Fatalf("the rehearsal did not restore the carrier: %v", rs)
	}
	// 2. an unknown REQUIRED section is refused by the new binary
	r.probe = func(req ProbeRequest) error {
		r.mutate(req.StatePath, func(f *handoff.File) { f.Set("mustknow", 1, true, 1) })
		return r.rehearse(req)
	}
	err := r.restart(false)
	if err == nil || !strings.Contains(err.Error(), "mustknow") {
		t.Fatalf("err = %v", err)
	}
	xtest.mu.Lock()
	rolled := xtest.rolled
	xtest.mu.Unlock()
	if rolled == 0 {
		t.Fatal("the carrier was not told about the rollback")
	}
	// 3. a plan item from the carrier blocks an unforced restart
	xtest.mu.Lock()
	xtest.ends = true
	xtest.mu.Unlock()
	var ref *RestartRefused
	if err := r.restart(false); !errors.As(err, &ref) || ref.Plan.Ends[0].Section != "xtest" {
		t.Fatalf("err = %v", err)
	}
	xtest.mu.Lock()
	xtest.ends = false
	xtest.mu.Unlock()
	// 4. and finally a good restart carries the data and the descriptor
	r.probe = nil
	if err := r.restart(false); err != nil {
		t.Fatal(err)
	}
	xtest.mu.Lock()
	got := strings.Join(xtest.restored, ";")
	xtest.mu.Unlock()
	if !strings.Contains(got, "/4/probe=y") || !strings.Contains(got, "through the pipe/4/probe=n") {
		t.Fatalf("restored: %q", got)
	}
}

// The new process refuses a file written by a newer gtf (too new) and the old
// process carries on; a file that is not private is refused too.
func TestRestartNewBinaryRefusesTooNewFile(t *testing.T) {
	r := newRig(t)
	srv := r.connect("w", "4000")
	r.probe = func(req ProbeRequest) error {
		r.mutate(req.StatePath, func(f *handoff.File) { f.Format, f.MinReader = handoff.Format+1, handoff.Format+1 })
		return r.rehearse(req)
	}
	err := r.restart(false)
	if err == nil || !strings.Contains(err.Error(), "newer gtf") {
		t.Fatalf("err = %v", err)
	}
	srv.Send([]byte("carried on\r\n"))
	r.wait("carried on")
}

// ---- helpers ----

// rehearse is the in-process rehearsal (doProbe without the hook).
func (r *rig) rehearse(req ProbeRequest) error {
	save := r.probe
	r.probe = nil
	defer func() { r.probe = save }()
	return r.doProbe(req)
}

// mutate rewrites the state file as a different gtf would have written it.
func (r *rig) mutate(path string, f func(*handoff.File)) {
	data, err := os.ReadFile(path)
	if err != nil {
		r.t.Fatal(err)
	}
	var hf handoff.File
	if err := json.Unmarshal(data, &hf); err != nil {
		r.t.Fatal(err)
	}
	f(&hf)
	out, err := hf.Encode()
	if err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(path, out, 0o600); err != nil {
		r.t.Fatal(err)
	}
}

var _ = syscall.Dup
