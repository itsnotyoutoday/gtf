// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package client

import (
	"time"

	"github.com/itsnotyoutoday/gtfparser"
)

// onKeys feeds raw terminal bytes to the engine, which owns the key bindings
// (/bind, /def -b, the library's kbbind.tf), the input buffer and /dokey.
// The only thing the client adds is the input window's memory of entered
// lines (tf shows them scrolled up in the input window) and the timeout
// that gives up on a held-back prefix such as a lone ESC.
func (c *Client) onKeys(s string) {
	if c.keyTimer != nil {
		c.keyTimer.Stop()
		c.keyTimer = nil
	}
	for len(s) > 0 {
		// split after each Enter so we can see the line that was submitted
		n := len(s)
		for i := 0; i < len(s); i++ {
			if s[i] == '\r' || s[i] == '\n' {
				n = i + 1
				break
			}
		}
		seg := s[:n]
		s = s[n:]
		last := seg[len(seg)-1]
		if last == '\r' || last == '\n' {
			if len(seg) > 1 {
				c.in.KeyPressed(seg[:len(seg)-1])
			}
			seg = seg[len(seg)-1:]
		}
		before := c.in.Input().Text()
		at := c.ui.Scr.Vis()
		c.in.KeyPressed(seg)
		if (last == '\r' || last == '\n') && c.in.Input().Text() == "" {
			if c.ui.Hidden {
				before = ""
			}
			c.ui.NoteSubmittedAt(before, at)
		}
	}
	if c.in.PendingKeys() != "" {
		c.keyTimer = time.AfterFunc(250*time.Millisecond, func() {
			c.run.Post(func(*gtfparser.Interp) { c.in.FlushKeys(); c.afterTask() })
		})
	}
}
