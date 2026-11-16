// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package client

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/itsnotyoutoday/gtf/internal/ui/vt"
)

// TestCmcpLiveTestgame runs the real client against the local Evennia testgame (GTF_LIVE=127.0.0.1:4101):
// definition fetch, the concourse pack, the screen. It is skipped unless GTF_LIVE is set.
func TestCmcpLiveTestgame(t *testing.T) {
	addr := os.Getenv("GTF_LIVE")
	if addr == "" {
		t.Skip("set GTF_LIVE=host:port (the local testgame) to run")
	}
	host, port, _ := strings.Cut(addr, ":")
	scr := vt.New(120, 40)
	c, err := New(Config{Term: scr, HomeDir: t.TempDir(), NoRC: true, Quiet: true, Interactive: true, NoUpdateCheck: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	exec(c, "/set cmcp_dev_loopback_http=on", "/connect "+host+" "+port)
	time.Sleep(1500 * time.Millisecond)
	exec(c, "connect bob cobalt-fern-52")
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		c.Flush()
		if strings.Contains(scr.Text(), "CONCOURSE") || strings.Contains(scr.Text(), "Concourse") {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	for _, step := range []struct{ cmd, want string }{
		{"board booty", ""}, {"man helm", "NAVIGATION"}, {"unman", ""}, {"home", "CONCOURSE"},
	} {
		exec(c, step.cmd)
		time.Sleep(1200 * time.Millisecond)
		c.Flush()
		if os.Getenv("GTF_LIVE_DUMP") != "" {
			t.Logf("after %q\n%s", step.cmd, scr.Dump())
		}
		if step.want != "" && !strings.Contains(strings.ToUpper(scr.Text()), step.want) {
			t.Errorf("after %q the screen lacks %q", step.cmd, step.want)
		}
	}
	exec(c, "/cmcp status")
	c.Flush()
	t.Log("\n" + scr.Dump())
}
