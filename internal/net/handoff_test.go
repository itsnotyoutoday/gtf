// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package net_test

import (
	"bytes"
	"compress/zlib"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"io"
	"math/big"
	gonet "net"
	"strings"
	"testing"
	"time"

	gnet "github.com/itsnotyoutoday/gtf/internal/net"
	"github.com/itsnotyoutoday/gtf/internal/net/nettest"
	"github.com/itsnotyoutoday/gtf/internal/net/telnet"
)

// drain collects events until want bytes of text have arrived (or Paused).
func textOf(t *testing.T, c *gnet.Conn, want string) string {
	t.Helper()
	var got strings.Builder
	deadline := time.After(3 * time.Second)
	for got.Len() < len(want) {
		select {
		case ev := <-c.Events():
			switch e := ev.(type) {
			case gnet.Text:
				got.Write(e.Data)
			case gnet.Closed:
				t.Fatalf("closed: %v (got %q)", e.Err, got.String())
			}
		case <-deadline:
			t.Fatalf("timeout: got %q want %q", got.String(), want)
		}
	}
	return got.String()
}

// pauseAndDrain pauses c and collects the text still in flight.
func pauseAndDrain(t *testing.T, c *gnet.Conn) string {
	t.Helper()
	if err := c.Pause(); err != nil {
		t.Fatal(err)
	}
	var got strings.Builder
	for {
		select {
		case ev := <-c.Events():
			switch e := ev.(type) {
			case gnet.Text:
				got.Write(e.Data)
			case gnet.Paused:
				return got.String()
			}
		case <-time.After(3 * time.Second):
			t.Fatal("no Paused event")
		}
	}
}

func handoff(t *testing.T, c *gnet.Conn, tlsCfg *tls.Config) *gnet.Conn {
	t.Helper()
	st, blob, f, err := c.Export()
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	// the state travels as JSON, like in the state file
	js, err := json.Marshal(st)
	if err != nil {
		t.Fatal(err)
	}
	var st2 gnet.HandoffState
	if err := json.Unmarshal(js, &st2); err != nil {
		t.Fatal(err)
	}
	n, err := gnet.Adopt(st2, blob, f, gnet.Options{}, tlsCfg)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// A connection in the middle of a line and of an MCCP2 stream is handed over;
// the new connection sees the rest, the server never notices.
func TestHandoffTCPMidLineMidCompression(t *testing.T) {
	rw, srv := nettest.Pair(t)
	c := gnet.NewConn(rw, gnet.Options{})
	srv.Send(nettest.IAC(telnet.WILL, telnet.OptMCCP2), nettest.IAC(telnet.DO, telnet.OptTType))
	srv.Expect(nettest.IAC(telnet.DO, telnet.OptMCCP2))
	srv.Send(nettest.SB(telnet.OptTType, 1))
	srv.Send([]byte("first line\r\npartial "))
	srv.Send(nettest.IAC(telnet.SB, telnet.OptMCCP2), []byte{telnet.IAC, telnet.SE})
	var z bytes.Buffer
	zw := zlib.NewWriter(&z)
	zw.Write([]byte("zipped "))
	zw.Flush()
	cut := z.Len()
	zw.Write([]byte("tail of the zipped stream\r\n"))
	zw.Write(nettest.SB(telnet.OptTType, 1)) // a TTYPE SEND inside the compressed stream
	zw.Flush()
	srv.Send(z.Bytes()[:cut])
	// let it all arrive, then pause in the middle of the compressed stream
	textOf(t, c, "first line\r\npartial zipped ")
	if got := pauseAndDrain(t, c); got != "" {
		t.Fatalf("unexpected %q", got)
	}
	n := handoff(t, c, nil)
	// bytes sent while nobody reads wait in the socket buffer
	srv.Send(z.Bytes()[cut:])
	n.Resume()
	if got := textOf(t, n, "tail of the zipped stream\r\n"); got != "tail of the zipped stream\r\n" {
		t.Fatalf("got %q", got)
	}
	// the TTYPE answer cycle continues where it was: the first SEND was answered
	// by the old process, the second must give the next name
	srv.Read(len(nettest.IAC(telnet.WILL, telnet.OptTType)))
	srv.Read(len(nettest.SB(telnet.OptTType, append([]byte{0}, "TINYFUGUE"...)...)))
	srv.Expect(nettest.SB(telnet.OptTType, append([]byte{0}, "XTERM"...)...)) // the terminal type is second (MTTS order)
	// and writes go out through the new connection
	n.SendLine("look")
	srv.Expect([]byte("look\r\n"))
}

func TestPauseResumeKeepsConnection(t *testing.T) {
	rw, srv := nettest.Pair(t)
	c := gnet.NewConn(rw, gnet.Options{})
	srv.Send([]byte("a"))
	textOf(t, c, "a")
	pauseAndDrain(t, c)
	srv.Send([]byte("b"))
	c.Resume()
	textOf(t, c, "b")
	_ = io.Discard
}

func selfSigned(t *testing.T) tls.Certificate {
	k, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "localhost"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), DNSNames: []string{"localhost"}}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &k.PublicKey, k)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: k}
}

func tlsPair(t *testing.T, scfg, ccfg *tls.Config) (*gnet.Conn, *tls.Conn) {
	t.Helper()
	scfg.Certificates = []tls.Certificate{selfSigned(t)}
	ln, err := tls.Listen("tcp", "127.0.0.1:0", scfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	ch := make(chan *tls.Conn, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		c.(*tls.Conn).Handshake()
		ch <- c.(*tls.Conn)
	}()
	host, port, _ := gonet.SplitHostPort(ln.Addr().String())
	c, err := gnet.Dial(context.Background(), gnet.TLS{Timeout: 3 * time.Second, Config: ccfg}, host, port, gnet.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	srv := <-ch
	t.Cleanup(func() { srv.Close() })
	return c, srv
}

func TestHandoffTLS(t *testing.T) {
	for name, cfgs := range map[string][2]*tls.Config{
		"tls13":    {{}, {}},
		"tls12gcm": {{MaxVersion: tls.VersionTLS12}, {MaxVersion: tls.VersionTLS12, CipherSuites: []uint16{tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256}}},
	} {
		t.Run(name, func(t *testing.T) {
			c, srv := tlsPair(t, cfgs[0], cfgs[1])
			if k, why := c.HandoffKind(); k != "tls" {
				t.Fatalf("kind %q (%s)", k, why)
			}
			srv.Write([]byte("hello tls\r\nhalf a li"))
			textOf(t, c, "hello tls\r\nhalf a li")
			pauseAndDrain(t, c)
			n := handoff(t, c, &tls.Config{InsecureSkipVerify: true})
			srv.Write([]byte("ne\r\n")) // arrives while nobody reads
			n.Resume()
			if got := textOf(t, n, "ne\r\n"); got != "ne\r\n" {
				t.Fatalf("got %q", got)
			}
			if err := n.SendLine("from the new process"); err != nil {
				t.Fatal(err)
			}
			buf := make([]byte, 64)
			srv.SetReadDeadline(time.Now().Add(3 * time.Second))
			k, err := srv.Read(buf)
			if err != nil || string(buf[:k]) != "from the new process\r\n" {
				t.Fatalf("server got %q, %v", buf[:k], err)
			}
		})
	}
}

func TestTLSCBCCannotBeCarried(t *testing.T) {
	c, _ := tlsPair(t, &tls.Config{MaxVersion: tls.VersionTLS12},
		&tls.Config{MaxVersion: tls.VersionTLS12, CipherSuites: []uint16{tls.TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA}})
	if k, why := c.HandoffKind(); k != "" || why == "" {
		t.Fatalf("kind %q why %q", k, why)
	}
}

func TestCheckHandoffRefuses(t *testing.T) {
	rw, _ := nettest.Pair(t)
	c := gnet.NewConn(rw, gnet.Options{})
	pauseAndDrain(t, c)
	st, _, f, err := c.Export()
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	if err := gnet.CheckHandoff(st, nil); err != nil {
		t.Fatal(err)
	}
	bad := st
	bad.V = gnet.HandoffStateVersion + 1
	if err := gnet.CheckHandoff(bad, nil); err == nil {
		t.Fatal("too new accepted")
	}
	bad = st
	bad.Telnet.PS = 200
	if err := gnet.CheckHandoff(bad, nil); err == nil {
		t.Fatal("corrupt accepted")
	}
	if err := gnet.CheckHandoff(st, []byte("junk")); err == nil {
		t.Fatal("tls blob for a plain connection accepted")
	}
}
