// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

//go:build darwin || dragonfly || freebsd || netbsd || openbsd || linux

package client

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/itsnotyoutoday/gtf/internal/squatter/auth"
	"github.com/itsnotyoutoday/gtf/internal/squatter/proto"
	"github.com/itsnotyoutoday/gtf/internal/squatter/wire"
)

func (p *proc) match(re *regexp.Regexp) string {
	p.t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		p.mu.Lock()
		txt := p.scr.Text()
		p.mu.Unlock()
		if m := re.FindStringSubmatch(txt); m != nil {
			return m[1]
		}
		time.Sleep(10 * time.Millisecond)
	}
	p.t.Fatalf("the screen never matched %v:\n%s", re, p.text())
	return ""
}

func (p *proc) pairingInfo() (code, fp, url string) {
	p.keys("/pair\r")
	code = p.match(regexp.MustCompile(`Pairing code:\s+(\S+)`))
	fp = strings.Join(strings.Fields(p.match(regexp.MustCompile(`(?s)TLS fingerprint:\s+([0-9A-F:\s]+?)\s*Connect to:`))), "") // wrapped over two rows
	url = p.match(regexp.MustCompile(`Connect to:\s+(wss://\S+)`))
	return
}

// The real thing, with real processes: a client of the squatter is connected over TLS when gtf does
// /restart (a real exec after a real rehearsal child), and it stays connected: same process id, same
// socket, the TLS session continues without a handshake, and the listener is the one inherited.
func TestRestartRealExecKeepsSquatterClientConnected(t *testing.T) {
	ln, port := game(t)
	// the squatter first, so that the connection is registered when it opens ("/squatter on" starts it at once)
	cmds := "/set squatter_port=0\n/set squatter_bind=127.0.0.1\n/squatter on\n/addworld w localhost " + port + "\n/connect w"
	p := startProc(t, "GTF_TEST_CMDS="+cmds)
	g := acceptConn(t, ln)
	g.Write([]byte("hello\r\n"))
	p.waitFor("hello")
	code, fp, url := p.pairingInfo()
	key, _ := auth.GenerateKey()
	pc, err := wire.Dial(bg(t), url, wire.ClientOptions{Key: key, PairCode: code, DeviceName: "phone", PinnedFingerprint: fp, ExpectKind: proto.KindTFSquatterd})
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()
	var id string
	for _, s := range pc.Sessions() {
		if s.Name == "w" {
			id = s.ID
		}
	}
	if id == "" {
		t.Fatalf("no session for w: %+v", pc.Sessions())
	}
	tp := newTape(pc)
	if err := pc.Attach(bg(t), id, nil, 0); err != nil {
		t.Fatal(err)
	}
	eventually(t, "hello over the squatter", func() bool { return tp.has("game:hello") })
	pid := p.cmd.Process.Pid
	p.keys("/restart\r")
	g.Write([]byte("while restarting\r\n"))
	deadline := time.Now().Add(10 * time.Second)
	for !tp.has("game:while restarting") {
		if time.Now().After(deadline) {
			t.Fatalf("nothing arrived through the carried connection:\n%s", p.text())
		}
		time.Sleep(10 * time.Millisecond)
	}
	select {
	case err := <-p.done:
		t.Fatalf("the process ended: %v\n%s", err, p.text())
	case <-pc.Done():
		t.Fatalf("the client was disconnected:\n%s", p.text())
	default:
	}
	if p.cmd.Process.Pid != pid {
		t.Fatal("pid changed")
	}
	if strings.Contains(p.text(), "keeps running") {
		t.Fatalf("the restart was abandoned:\n%s", p.text())
	}
	// both directions work over the same connections
	g.Write([]byte("after the exec\r\n"))
	eventually(t, "a line after the exec", func() bool { return tp.has("game:after the exec") })
	if err := pc.Send(bg(t), id, "from the phone"); err != nil {
		t.Fatal(err)
	}
	readLine(t, g, "from the phone\r\n")
	seqs, _ := tp.snapshot()
	consecutive(t, seqs)
	// a second restart carries it again (the carried client is itself exportable), and a new client can connect
	p.keys("/restart\r")
	g.Write([]byte("second generation\r\n"))
	deadline = time.Now().Add(10 * time.Second)
	for !tp.has("game:second generation") {
		if time.Now().After(deadline) {
			t.Fatalf("nothing arrived after the second restart:\n%s", p.text())
		}
		time.Sleep(10 * time.Millisecond)
	}
	select {
	case <-pc.Done():
		t.Fatal("the client was disconnected by the second restart")
	default:
	}
	pc2, err := wire.Dial(bg(t), url, wire.ClientOptions{Key: key, DeviceName: "phone", PinnedFingerprint: fp, ExpectKind: proto.KindTFSquatterd})
	if err != nil {
		t.Fatalf("a new connection to the inherited listener: %v", err)
	}
	pc2.Close()
	seqs, _ = tp.snapshot()
	consecutive(t, seqs)
	p.keys("/quit\r")
}
