// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

// Package nettest is a fake telnet server for unit tests: it scripts
// negotiations byte for byte over an in-memory pipe.
package nettest

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"testing"
	"time"
)

// Server is the server end of a pipe.
type Server struct {
	t    testing.TB
	c    net.Conn
	buf  bytes.Buffer
	Wait time.Duration
}

// Pair (a loopback TCP pair, so writes are buffered) returns the client end (give it to net.NewConn) and the scripted server.
func Pair(t testing.TB) (io.ReadWriteCloser, *Server) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	a, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	b, err := ln.Accept()
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{t: t, c: b, Wait: 2 * time.Second}
	t.Cleanup(func() { a.Close(); b.Close() })
	return a, s
}

// Send writes raw bytes to the client.
func (s *Server) Send(p ...[]byte) {
	s.t.Helper()
	s.c.SetWriteDeadline(time.Now().Add(s.Wait))
	if _, err := s.c.Write(bytes.Join(p, nil)); err != nil {
		s.t.Fatalf("nettest: send: %v", err)
	}
}

// Expect reads until exactly want has been received (failing on a mismatch
// or timeout).
func (s *Server) Expect(want ...[]byte) {
	s.t.Helper()
	w := bytes.Join(want, nil)
	deadline := time.Now().Add(s.Wait)
	for s.buf.Len() < len(w) {
		s.c.SetReadDeadline(deadline)
		tmp := make([]byte, 4096)
		n, err := s.c.Read(tmp)
		s.buf.Write(tmp[:n])
		if err != nil {
			break
		}
	}
	if s.buf.Len() < len(w) || !bytes.Equal(s.buf.Bytes()[:len(w)], w) {
		s.t.Fatalf("nettest: expected % x, got % x", w, s.buf.Bytes())
	}
	s.buf.Next(len(w))
}

// ExpectPrefixAny reads at least n bytes and returns them (consumed).
func (s *Server) Read(n int) []byte {
	s.t.Helper()
	deadline := time.Now().Add(s.Wait)
	for s.buf.Len() < n {
		s.c.SetReadDeadline(deadline)
		tmp := make([]byte, 4096)
		k, err := s.c.Read(tmp)
		s.buf.Write(tmp[:k])
		if err != nil {
			s.t.Fatalf("nettest: read %d bytes: have % x: %v", n, s.buf.Bytes(), err)
		}
	}
	return s.buf.Next(n)
}

// Close closes the server side.
func (s *Server) Close() { s.c.Close() }

// IAC builds telnet command sequences: IAC(WILL, 1) = ff fb 01.
func IAC(cmd byte, opt ...byte) []byte { return append([]byte{255, cmd}, opt...) }

// SB builds IAC SB opt data IAC SE (data is escaped).
func SB(opt byte, data ...byte) []byte {
	b := []byte{255, 250, opt}
	for _, c := range data {
		if c == 255 {
			b = append(b, 255)
		}
		b = append(b, c)
	}
	return append(b, 255, 240)
}

// Fmt is a helper for text.
func Fmt(f string, a ...any) []byte { return []byte(fmt.Sprintf(f, a...)) }
