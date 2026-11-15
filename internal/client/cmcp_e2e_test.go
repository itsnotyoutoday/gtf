// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package client

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/itsnotyoutoday/cmcp/session"
	"github.com/itsnotyoutoday/gtf/internal/ui/vt"
	"github.com/itsnotyoutoday/gtfparser"
)

// TestCmcpEvenniaE2E drives gtf against the Evennia PoC on 127.0.0.1:4101 (GTF_E2E_EVENNIA=1;
// never the owner's CobraMUSH game). Throwaway account alice.
func TestCmcpEvenniaE2E(t *testing.T) {
	if os.Getenv("GTF_E2E_EVENNIA") == "" {
		t.Skip("set GTF_E2E_EVENNIA=1 with the Evennia PoC running on 127.0.0.1:4101")
	}
	term := &recTerm{Screen: vt.New(120, 40)}
	c, err := New(Config{Term: term, HomeDir: t.TempDir(), NoRC: true, Quiet: true, Interactive: true, NoUpdateCheck: true})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	f := &screenFixture{t: t, c: c, term: term}
	exec(c, "/set cmcp_pack_consent=always", "/connect 127.0.0.1 4101")
	cmEventually(t, "handshake", f.clientReady)
	exec(c, "connect alice ember-tide-91")
	time.Sleep(1500 * time.Millisecond)
	for _, cmd := range []string{"board resolute", "man opera"} {
		exec(c, cmd)
		time.Sleep(1500 * time.Millisecond)
	}
	var id string
	cmEventually(t, "an operations screen", func() bool {
		c.Do(func(*gtfparser.Interp) { id = c.fg.cm.UI().Screen.Resolved })
		return strings.Contains(id, "operations")
	})
	time.Sleep(2 * time.Second)
	f.render()
	t.Logf("screen %s\n%s", id, term.Dump())
	f.dump("evennia-ops")
	exec(c, "/cmcp status")
	f.render()
	var st session.State
	c.Do(func(*gtfparser.Interp) { st = c.fg.cm.UI().State })
	if st != session.StateReady {
		t.Fatal(st)
	}
	exec(c, "unman")
	time.Sleep(time.Second)
}
