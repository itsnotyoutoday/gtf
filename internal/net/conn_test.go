// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package net_test

import (
	"bytes"
	"compress/zlib"
	"strings"
	"testing"
	"time"

	gnet "github.com/itsnotyoutoday/gtf/internal/net"
	"github.com/itsnotyoutoday/gtf/internal/net/nettest"
	"github.com/itsnotyoutoday/gtf/internal/net/telnet"
)

func next(t *testing.T, c *gnet.Conn) gnet.Event {
	t.Helper()
	select {
	case e := <-c.Events():
		return e
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for event")
	}
	return nil
}

// nextNonOpt skips OptionChange events.
func nextNonOpt(t *testing.T, c *gnet.Conn) gnet.Event {
	for {
		e := next(t, c)
		if _, ok := e.(gnet.OptionChange); !ok {
			return e
		}
	}
}

func newConn(t *testing.T) (*gnet.Conn, *nettest.Server, *telnet.Env) {
	rw, srv := nettest.Pair(t)
	env := telnet.NewEnv()
	env.TermName = "xterm-256color"
	env.Color256 = true
	env.Cols, env.Rows = 100, 30
	c := gnet.NewConn(rw, gnet.Options{Env: env})
	t.Cleanup(func() { c.Close() })
	return c, srv, env
}

func collect(t *testing.T, c *gnet.Conn, until string) string {
	t.Helper()
	var s string
	for !strings.Contains(s, until) {
		if tx, ok := nextNonOpt(t, c).(gnet.Text); ok {
			s += string(tx.Data)
		}
	}
	return s
}

func TestTextAndIACEscape(t *testing.T) {
	c, srv, _ := newConn(t)
	srv.Send([]byte("hi \xff\xffthere\r\n"))
	if got := collect(t, c, "there"); got != "hi \u00ffthere\r\n" {
		t.Fatalf("got %q", got)
	}
	c.Write("a\xffb")
	srv.Expect([]byte("a\xc3\xbf\xc2\x83b")[:0]) // outgoing text is UTF-8; IAC bytes never appear raw
}

func TestNAWSAndTType(t *testing.T) {
	_, srv, _ := newConn(t)
	srv.Send(nettest.IAC(telnet.DO, telnet.OptNAWS))
	srv.Expect(nettest.IAC(telnet.WILL, telnet.OptNAWS), nettest.SB(telnet.OptNAWS, 0, 100, 0, 30))
	srv.Send(nettest.IAC(telnet.DO, telnet.OptTType))
	srv.Expect(nettest.IAC(telnet.WILL, telnet.OptTType))
	ask := nettest.SB(telnet.OptTType, 1)
	srv.Send(ask)
	srv.Expect(nettest.SB(telnet.OptTType, append([]byte{0}, "TINYFUGUE"...)...))
	srv.Send(ask)
	srv.Expect(nettest.SB(telnet.OptTType, append([]byte{0}, "XTERM-256COLOR"...)...)) // MTTS order: terminal type second
	srv.Send(ask)
	// ANSI|VT100|UTF8|256 = 15, third
	srv.Expect(nettest.SB(telnet.OptTType, append([]byte{0}, "MTTS 15"...)...))
	srv.Send(ask)
	srv.Expect(nettest.SB(telnet.OptTType, append([]byte{0}, "GTF"...)...)) // our own name, last
	srv.Send(ask)
	srv.Expect(nettest.SB(telnet.OptTType, append([]byte{0}, "GTF"...)...)) // the last answer repeats: that is all
}

func TestNAWSResize(t *testing.T) {
	c, srv, _ := newConn(t)
	srv.Send(nettest.IAC(telnet.DO, telnet.OptNAWS))
	srv.Expect(nettest.IAC(telnet.WILL, telnet.OptNAWS), nettest.SB(telnet.OptNAWS, 0, 100, 0, 30))
	c.Resize(120, 40)
	srv.Expect(nettest.SB(telnet.OptNAWS, 0, 120, 0, 40))
}

func TestEchoPasswordModeAndRefusals(t *testing.T) {
	c, srv, _ := newConn(t)
	srv.Send(nettest.IAC(telnet.WILL, telnet.OptEcho))
	srv.Expect(nettest.IAC(telnet.DO, telnet.OptEcho))
	oc := next(t, c).(gnet.OptionChange)
	if oc.Opt != telnet.OptEcho || !oc.On || oc.Side != telnet.Them {
		t.Fatalf("%+v", oc)
	}
	srv.Send(nettest.IAC(telnet.WONT, telnet.OptEcho))
	srv.Expect(nettest.IAC(telnet.DONT, telnet.OptEcho))
	if oc = next(t, c).(gnet.OptionChange); oc.On {
		t.Fatal("echo should be off")
	}
	// unknown option refused
	srv.Send(nettest.IAC(telnet.DO, 200+99-99+0))
	srv.Send(nettest.IAC(telnet.WILL, 99))
	srv.Expect(nettest.IAC(telnet.WONT, 200), nettest.IAC(telnet.DONT, 99))
}

func TestPromptEvents(t *testing.T) {
	c, srv, _ := newConn(t)
	srv.Send([]byte("HP> "), nettest.IAC(telnet.GA), []byte("MP> "), nettest.IAC(telnet.EORc))
	var got []string
	for len(got) < 4 {
		switch e := next(t, c).(type) {
		case gnet.Text:
			got = append(got, "T:"+string(e.Data))
		case gnet.Prompt:
			got = append(got, "P:"+e.Kind)
		}
	}
	if strings.Join(got, "|") != "T:HP> |P:GA|T:MP> |P:EOR" {
		t.Fatalf("%v", got)
	}
}

func TestSubnegUnhandledAndCharset(t *testing.T) {
	c, srv, env := newConn(t)
	srv.Send(nettest.SB(77, 1, 2, 255, 3))
	s := nextNonOpt(t, c).(gnet.Subneg)
	if s.Opt != 77 || !bytes.Equal(s.Data, []byte{1, 2, 255, 3}) {
		t.Fatalf("%+v", s)
	}
	srv.Send(nettest.IAC(telnet.DO, telnet.OptCharset))
	srv.Expect(nettest.IAC(telnet.WILL, telnet.OptCharset))
	srv.Send(nettest.SB(telnet.OptCharset, append([]byte{1, ';'}, "ISO-8859-1;UTF-8"...)...))
	srv.Expect(nettest.SB(telnet.OptCharset, append([]byte{2}, "UTF-8"...)...))
	if !env.Snapshot().UTF8 {
		t.Fatal("utf8 expected")
	}
}

func TestLatin1AndInvalidTolerance(t *testing.T) {
	c, srv, _ := newConn(t)
	srv.Send([]byte("caf\xe9 \xe2\x82\xac\r\n")) // invalid lone e9, then valid euro
	e := nextNonOpt(t, c).(gnet.Text)
	if string(e.Data) != "café €\r\n" {
		t.Fatalf("%q", e.Data)
	}
	// multibyte split across reads
	srv.Send([]byte("\xe2\x82"))
	srv.Send([]byte("\xac!"))
	var s string
	for !strings.Contains(s, "!") {
		s += string(nextNonOpt(t, c).(gnet.Text).Data)
	}
	if s != "€!" {
		t.Fatalf("%q", s)
	}
}

func TestMCCP2(t *testing.T) {
	c, srv, _ := newConn(t)
	srv.Send(nettest.IAC(telnet.WILL, telnet.OptMCCP2))
	srv.Expect(nettest.IAC(telnet.DO, telnet.OptMCCP2))
	var z bytes.Buffer
	zw := zlib.NewWriter(&z)
	zw.Write([]byte("compressed line\r\n"))
	zw.Close()
	srv.Send(nettest.SB(telnet.OptMCCP2), z.Bytes(), []byte("after\r\n"))
	var s string
	for !strings.Contains(s, "after") {
		s += string(nextNonOpt(t, c).(gnet.Text).Data)
	}
	if s != "compressed line\r\nafter\r\n" {
		t.Fatalf("%q", s)
	}
}

func TestGMCP(t *testing.T) {
	c, srv, _ := newConn(t)
	srv.Send(nettest.IAC(telnet.WILL, telnet.OptGMCP))
	srv.Expect(nettest.IAC(telnet.DO, telnet.OptGMCP))
	hello := srv.Read(len(nettest.SB(telnet.OptGMCP, []byte(`Core.Hello {"client":"GTF","version":"0.1"}`)...)))
	if !strings.Contains(string(hello), "Core.Hello") {
		t.Fatalf("%q", hello)
	}
	srv.Read(len(nettest.SB(telnet.OptGMCP, []byte(`Core.Supports.Set ["Char 1","Room 1","Comm 1","Core 1","Group 1","Client.Media 1"]`)...)))
	srv.Send(nettest.SB(telnet.OptGMCP, []byte(`Char.Vitals {"hp":10,"maxhp":20}`)...))
	var m gnet.Message
	for {
		if mm, ok := next(t, c).(gnet.Message); ok {
			m = mm
			break
		}
	}
	v := m.Value.(map[string]any)
	if m.Proto != "gmcp" || m.Name != "Char.Vitals" || v["hp"].(float64) != 10 {
		t.Fatalf("%+v", m)
	}
	if err := c.SendProto("gmcp", "Char.Vitals.Get", nil); err != nil {
		t.Fatal(err)
	}
	srv.Expect(nettest.SB(telnet.OptGMCP, []byte("Char.Vitals.Get")...))
}

func TestMSDPAndMSSP(t *testing.T) {
	c, srv, _ := newConn(t)
	srv.Send(nettest.IAC(telnet.WILL, telnet.OptMSDP), nettest.IAC(telnet.WILL, telnet.OptMSSP))
	srv.Expect(nettest.IAC(telnet.DO, telnet.OptMSDP), nettest.IAC(telnet.DO, telnet.OptMSSP))
	srv.Send(nettest.SB(telnet.OptMSDP, 1, 'H', 'P', 2, '9', '5', 1, 'R', 'O', 'O', 'M', 2, 3, 1, 'N', 2, 'x', 1, 'E', 2, 5, 2, 'n', 2, 's', 6, 4))
	srv.Send(nettest.SB(telnet.OptMSSP, 1, 'N', 'A', 'M', 'E', 2, 'T', 'e', 's', 't', 1, 'P', 2, '3'))
	var got []gnet.Message
	for len(got) < 4 {
		if m, ok := next(t, c).(gnet.Message); ok {
			got = append(got, m)
		}
	}
	if got[0].Name != "HP" || got[0].Value != "95" {
		t.Fatalf("%+v", got[0])
	}
	room := got[1].Value.(map[string]any)
	if room["N"] != "x" || len(room["E"].([]any)) != 2 {
		t.Fatalf("%+v", got[1])
	}
	if got[2].Proto != "mssp" || got[2].Name != "NAME" || got[3].Value != "3" {
		t.Fatalf("%+v %+v", got[2], got[3])
	}
	if err := c.SendProto("msdp", "REPORT", []any{"HEALTH", "MANA"}); err != nil {
		t.Fatal(err)
	}
	srv.Expect(nettest.SB(telnet.OptMSDP, 1, 'R', 'E', 'P', 'O', 'R', 'T', 2, 'H', 'E', 'A', 'L', 'T', 'H', 2, 'M', 'A', 'N', 'A'))
}

func TestClosed(t *testing.T) {
	c, srv, _ := newConn(t)
	srv.Close()
	for {
		if _, ok := next(t, c).(gnet.Closed); ok {
			return
		}
	}
}
