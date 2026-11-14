// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package client

import (
	"fmt"
	"os"
	"time"

	"github.com/itsnotyoutoday/gtf/internal/ui"
	"github.com/itsnotyoutoday/gtfparser"
)

// The new process's side of /restart (restart.go has the old process's).

// startRestored begins the restored client: the sockets are resumed (they
// were adopted paused, so nothing has been read yet), a partial line gets its
// prompt timer back, sessions the old process could not carry are connected
// again, and the carried alert gets its remaining time.
func (c *Client) startRestored() {
	c.Post(func(in *gtfparser.Interp) {
		for _, s := range c.order {
			if s.conn == nil {
				continue
			}
			go c.pump(s, s.conn)
			if s.resizeAfterRestore {
				s.resizeAfterRestore = false
				s.conn.Resize(c.ui.W, c.ui.H) // the window changed size while we were away
			}
			s.conn.Resume()
			s.rearmPartial()
		}
		for _, rc := range c.reconnect {
			w, ok := in.World(rc.world)
			if !ok {
				continue
			}
			o := rc.opts
			o.Foreground, o.Background = rc.wasFg, !rc.wasFg
			c.echoTo(c.sessions[rc.world].scr, "", "% Reconnecting "+rc.world+" after /restart ("+rc.reason+").")
			c.withWorld(c.sessions[rc.world], func() { _ = c.connect(w, o) })
		}
		c.reconnect = nil
		if c.alert != nil && c.pendingAlert > 0 {
			id := c.alertSeq
			time.AfterFunc(c.pendingAlert, func() {
				c.Post(func(*gtfparser.Interp) {
					if c.alertSeq == id && c.alert != nil {
						c.clearAlert()
						c.scheduleRender()
					}
				})
			})
		}
		c.ui.Redraw()
		c.afterTask()
	})
}

// discardRestored drops a client that was built only to rehearse a restart:
// adopted sockets are released without ending anything.
func (c *Client) discardRestored() {
	c.doneOnce.Do(func() { close(c.done) })
	c.closed = true
	for _, s := range c.order {
		if s.conn != nil {
			s.conn.Discard()
		}
	}
	c.run.Close()
}

// Abandon stops this client WITHOUT closing its connections or touching the
// terminal: what exec does to the old process. A test hook for the exec uses it.
func (c *Client) Abandon() {
	c.doneOnce.Do(func() { close(c.done) })
	c.closed = true
	for _, s := range c.order {
		if s.cancel != nil {
			s.cancel()
		}
	}
	go c.run.Close() // asynchronous: it may be called from the runner goroutine itself
}

// ProbeMain is the rehearsal child (see spawnProbe): it reads the state file,
// builds the whole client from it with the sockets adopted paused and a
// terminal that discards everything, lets all of it go, and reports "ok" (or
// "error: ...") on the report descriptor. It returns the exit status.
func ProbeMain(spec *RestoreSpec, cfg Config) int {
	report := func(msg string) {
		if spec.ReadyFD >= 0 {
			f := os.NewFile(uintptr(spec.ReadyFD), "ready")
			fmt.Fprintln(f, msg)
			f.Close()
		}
	}
	r, err := LoadRestoration(*spec)
	if err != nil {
		report("error: " + err.Error())
		return 1
	}
	cfg.Term, cfg.Keys, cfg.Resized, cfg.Suspend = ui.Sink{W: max(r.term.W, 20), H: max(r.term.H, 5)}, nil, nil, nil
	cfg.Restore = r
	c, err := New(cfg)
	if err != nil {
		report("error: " + err.Error())
		return 1
	}
	c.discardRestored()
	report("ok")
	return 0
}
