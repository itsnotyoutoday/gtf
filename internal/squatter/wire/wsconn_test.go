// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package wire

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// pair returns a server-side wsConn and the raw client end of a real TCP connection.
func wsPair(t *testing.T) (*wsConn, net.Conn) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	type res struct {
		c   net.Conn
		err error
	}
	ch := make(chan res, 1)
	go func() { c, err := ln.Accept(); ch <- res{c, err} }()
	cl, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	r := <-ch
	if r.err != nil {
		t.Fatal(r.err)
	}
	t.Cleanup(func() { cl.Close(); r.c.Close() })
	return newWSConn(r.c, nil, wsFrag{}), cl
}

// cframe builds a masked client frame.
func cframe(op byte, fin bool, p []byte) []byte {
	b0 := op
	if fin {
		b0 |= 0x80
	}
	out := []byte{b0}
	switch {
	case len(p) < 126:
		out = append(out, 0x80|byte(len(p)))
	case len(p) <= 0xffff:
		out = append(out, 0x80|126, byte(len(p)>>8), byte(len(p)))
	default:
		out = append(out, 0x80|127)
		out = binary.BigEndian.AppendUint64(out, uint64(len(p)))
	}
	mask := []byte{0x11, 0x22, 0x33, 0x44}
	out = append(out, mask...)
	for i, c := range p {
		out = append(out, c^mask[i&3])
	}
	return out
}

// sframe reads one unmasked server frame.
func sframe(t *testing.T, c net.Conn) (op byte, p []byte) {
	t.Helper()
	c.SetReadDeadline(time.Now().Add(5 * time.Second))
	var h [2]byte
	if _, err := io.ReadFull(c, h[:]); err != nil {
		t.Fatalf("reading a frame: %v", err)
	}
	n := int(h[1] & 0x7f)
	switch n {
	case 126:
		var e [2]byte
		io.ReadFull(c, e[:])
		n = int(binary.BigEndian.Uint16(e[:]))
	case 127:
		var e [8]byte
		io.ReadFull(c, e[:])
		n = int(binary.BigEndian.Uint64(e[:]))
	}
	p = make([]byte, n)
	if _, err := io.ReadFull(c, p); err != nil {
		t.Fatal(err)
	}
	return h[0] & 0x0f, p
}

func readMsg(t *testing.T, ws *wsConn) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, d, err := ws.Read(ctx)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	return string(d)
}

func TestWSMessagesOfEverySizeFragmentsAndControlFrames(t *testing.T) {
	ws, cl := wsPair(t)
	for _, n := range []int{0, 1, 125, 126, 127, 65535, 65536, 300000} {
		msg := strings.Repeat("x", n)
		cl.Write(cframe(opText, true, []byte(msg)))
		if got := readMsg(t, ws); got != msg {
			t.Fatalf("%d bytes: got %d", n, len(got))
		}
	}
	// fragments with a ping in the middle: one message, one pong with the ping's payload
	cl.Write(cframe(opText, false, []byte("hel")))
	cl.Write(cframe(opPing, true, []byte("are you there")))
	cl.Write(cframe(opCont, false, []byte("lo ")))
	cl.Write(cframe(opCont, true, []byte("world")))
	if got := readMsg(t, ws); got != "hello world" {
		t.Fatalf("fragments: %q", got)
	}
	if op, p := sframe(t, cl); op != opPong || string(p) != "are you there" {
		t.Fatalf("pong: %d %q", op, p)
	}
	// a frame delivered a byte at a time
	one := cframe(opText, true, []byte("slowly"))
	go func() {
		for i := range one {
			cl.Write(one[i : i+1])
		}
	}()
	if got := readMsg(t, ws); got != "slowly" {
		t.Fatalf("byte at a time: %q", got)
	}
	// the server's messages are unmasked frames
	if err := ws.Write(context.Background(), websocket.MessageText, []byte(strings.Repeat("y", 70000))); err != nil {
		t.Fatal(err)
	}
	if op, p := sframe(t, cl); op != opText || len(p) != 70000 {
		t.Fatalf("server frame: %d %d", op, len(p))
	}
}

func TestWSProtocolViolationsAreAnswered(t *testing.T) {
	for name, tc := range map[string]struct {
		frame []byte
		limit int64
		code  websocket.StatusCode
	}{
		"unmasked":        {[]byte{0x81, 0x02, 'h', 'i'}, 0, websocket.StatusProtocolError},
		"reserved bit":    {append([]byte{0xC1}, cframe(opText, true, []byte("x"))[1:]...), 0, websocket.StatusProtocolError},
		"too big":         {cframe(opText, true, bytes.Repeat([]byte("x"), 200)), 100, websocket.StatusMessageTooBig},
		"bad utf-8":       {cframe(opText, true, []byte{0xff, 0xfe}), 0, websocket.StatusInvalidFramePayloadData},
		"stray cont":      {cframe(opCont, true, []byte("x")), 0, websocket.StatusProtocolError},
		"fragmented ping": {cframe(opPing, false, nil), 0, websocket.StatusProtocolError},
		"long ping":       {cframe(opPing, true, bytes.Repeat([]byte("x"), 126)), 0, websocket.StatusProtocolError},
		"unknown opcode":  {cframe(0x3, true, nil), 0, websocket.StatusProtocolError},
	} {
		ws, cl := wsPair(t)
		if tc.limit > 0 {
			ws.SetReadLimit(tc.limit)
		}
		cl.Write(tc.frame)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_, _, err := ws.Read(ctx)
		cancel()
		var ce CloseError
		if !errors.As(err, &ce) || ce.Code != tc.code {
			t.Errorf("%s: %v, want close %d", name, err, tc.code)
			continue
		}
		if op, p := sframe(t, cl); op != opClose || binary.BigEndian.Uint16(p) != uint16(tc.code) {
			t.Errorf("%s: the client was sent %d %v", name, op, p)
		}
	}
}

func TestWSCloseHandshake(t *testing.T) {
	ws, cl := wsPair(t)
	cl.Write(cframe(opClose, true, closePayload(websocket.StatusNormalClosure, "bye")))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _, err := ws.Read(ctx)
	var ce CloseError
	if !errors.As(err, &ce) || ce.Code != websocket.StatusNormalClosure || ce.Reason != "bye" {
		t.Fatalf("%v", err)
	}
	if op, _ := sframe(t, cl); op != opClose {
		t.Fatalf("no close frame back: %d", op)
	}
	// the server initiating: the close frame goes out, the client hangs up, Close returns
	ws2, cl2 := wsPair(t)
	done := make(chan error, 1)
	go func() { done <- ws2.Close(websocket.StatusPolicyViolation, "go away") }()
	op, p := sframe(t, cl2)
	if op != opClose || binary.BigEndian.Uint16(p) != uint16(websocket.StatusPolicyViolation) || string(p[2:]) != "go away" {
		t.Fatalf("close frame %d %q", op, p)
	}
	cl2.Close()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not return")
	}
}

// countConn counts the bytes the reader has taken from the connection.
type countConn struct {
	net.Conn
	n atomic.Int64
}

func (c *countConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	c.n.Add(int64(n))
	return n, err
}

// The property a handover rests on: a reader stopped at ANY byte of the stream gives back every byte
// it holds, and a new wsConn built from them (over the same connection) delivers exactly the messages
// the uninterrupted one would have.
func TestWSPausedAtEveryBytePositionLosesNothing(t *testing.T) {
	want := []string{"first", strings.Repeat("m", 200), "hello", "last"}
	var stream []byte
	var ends []int // where each data message is complete
	add := func(b []byte, completes bool) {
		stream = append(stream, b...)
		if completes {
			ends = append(ends, len(stream))
		}
	}
	add(cframe(opText, true, []byte(want[0])), true)
	add(cframe(opText, true, []byte(want[1])), true)
	add(cframe(opText, false, []byte("hel")), false)
	add(cframe(opPing, true, []byte("p")), false)
	add(cframe(opCont, true, []byte("lo")), true)
	add(cframe(opText, true, []byte(want[3])), true)
	for cut := 0; cut <= len(stream); cut++ {
		base, cl := wsPair(t)
		cc := &countConn{Conn: base.nc}
		ws := newWSConn(cc, nil, wsFrag{})
		cl.Write(stream[:cut])
		done := 0
		for _, e := range ends {
			if e <= cut {
				done++
			}
		}
		var got []string
		for i := 0; i < done; i++ {
			got = append(got, readMsg(t, ws))
		}
		// the reader is now at a point where it can only wait for more bytes: stop it once all of the cut has arrived
		rc := make(chan error, 1)
		go func() { _, _, err := ws.Read(context.Background()); rc <- err }()
		for dl := time.Now().Add(5 * time.Second); cc.n.Load() < int64(cut); {
			if time.Now().After(dl) {
				t.Fatalf("cut %d: only %d bytes arrived", cut, cc.n.Load())
			}
			runtime.Gosched()
		}
		ws.pauseRead()
		if err := <-rc; !errors.Is(err, errWSPaused) {
			t.Fatalf("cut %d: the stopped reader returned %v", cut, err)
		}
		buf, frag := ws.readState()
		// the "new process": the same socket, the state handed over
		ws2 := newWSConn(base.nc, buf, frag)
		base.nc.SetReadDeadline(time.Time{})
		cl.Write(stream[cut:])
		for len(got) < len(want) {
			got = append(got, readMsg(t, ws2))
		}
		if strings.Join(got, "|") != strings.Join(want, "|") {
			t.Fatalf("cut %d: got %q want %q", cut, got, want)
		}
		cl.Close()
	}
}

// Through a real HTTP server and the library's client: handshake, messages both ways, a refused
// request that is not a WebSocket upgrade.
func TestWSHandshakeWithTheLibraryClient(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := acceptWS(w, r)
		if err != nil {
			return
		}
		defer ws.CloseNow()
		ctx := r.Context()
		for {
			typ, d, err := ws.Read(ctx)
			if err != nil {
				return
			}
			if ws.Write(ctx, typ, append([]byte("echo:"), d...)) != nil {
				return
			}
		}
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range []string{"a", strings.Repeat("b", 20000)} {
		if err := c.Write(ctx, websocket.MessageText, []byte(m)); err != nil {
			t.Fatal(err)
		}
		_, d, err := c.Read(ctx)
		if err != nil || string(d) != "echo:"+m {
			t.Fatalf("echo: %v len %d", err, len(d))
		}
	}
	if err := c.Close(websocket.StatusNormalClosure, "done"); err != nil {
		t.Fatalf("closing handshake: %v", err)
	}
	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUpgradeRequired {
		t.Fatalf("a plain GET got %d", resp.StatusCode)
	}
}
