// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package client

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Mouse wheel (and only the wheel) in a CMCP screen. While a screen is up gtf asks the terminal for
// mouse reports (xterm 1000 with SGR 1006 coordinates; %cmcp_mouse off disables it). The wheel scrolls
// what is under the pointer: over the game-output pane the scrollback (3 lines a notch), over a list,
// table or log widget that widget. The frame, the strips, the notice row and the hint row never move.
// A terminal that sends no mouse events changes nothing.
//
// Trade-off: while mouse reporting is on the terminal's own text selection needs Shift (Option in
// iTerm and Terminal.app).

var sgrMouse = regexp.MustCompile(`^\x1b\[<(\d+);(\d+);(\d+)([Mm])`)

// wheelLines is how many lines of scrollback one wheel notch moves.
const wheelLines = 3

// mouseEvents consumes the SGR mouse reports at the start of s, acts on them and returns the rest.
// Mouse reports are swallowed even when reporting is off (a late one must not be typed as text).
func (c *Client) mouseEvents(s string) (string, bool) {
	took := false
	for {
		m := sgrMouse.FindStringSubmatch(s)
		if m == nil {
			return s, took
		}
		took = true
		s = s[len(m[0]):]
		b, _ := strconv.Atoi(m[1])
		x, _ := strconv.Atoi(m[2])
		y, _ := strconv.Atoi(m[3])
		if m[4] == "M" {
			c.mouseEvent(b, x-1, y-1)
		}
	}
}

// mouseEvent handles one press or wheel report at the 0-based cell x, y.
func (c *Client) mouseEvent(b, x, y int) {
	if b&64 == 0 {
		return // buttons and drags: nothing yet (the terminal's selection needs Shift while this is on)
	}
	dir := 1 // 64: wheel up (back in the history), 65: wheel down
	if b&3 == 0 {
		dir = -1
	} else if b&3 != 1 {
		return
	}
	s := c.fg
	if s == nil || s.cs == nil || s.cs.frame == nil || c.ui.Surf == nil {
		c.historyScroll(dir * wheelLines)
		return
	}
	switch h := s.cs.frame.HitTest(x, y); {
	case h.Text || h.Item < 0:
		// the output pane, the empty parts of the screen and (a room's) strips alike: the history
		// between the fixed frame scrolls, the frame is never redrawn for it
		c.historyScroll(dir * wheelLines)
	default:
		if s.cs.frame.Wheel(h.Item, dir) {
			c.scheduleRender()
		}
	}
}

// setMouse switches terminal mouse reporting; it is on only while a CMCP screen is shown.
func (c *Client) setMouse(on bool) {
	if on && strings.EqualFold(strings.TrimSpace(c.in.Var("cmcp_mouse")), "off") {
		on = false
	}
	if on == c.mouseOn {
		return
	}
	c.mouseOn = on
	if m, ok := c.cfg.Term.(interface{ SetMouse(bool) }); ok {
		m.SetMouse(on)
		return
	}
	if c.cfg.Term != nil {
		seq := "\x1b[?1006l\x1b[?1000l"
		if on {
			seq = "\x1b[?1000h\x1b[?1006h"
		}
		fmt.Fprint(c.cfg.Term, seq)
	}
}

// scrolledBack says how many lines of history the player has scrolled back (0 at the live bottom).
func (c *Client) scrolledBack() int {
	if c.ui == nil || c.ui.Scr == nil {
		return 0
	}
	return max(c.ui.Scr.SB.Len()-c.ui.Scr.Vis(), 0)
}

// historyScroll scrolls the history by n lines (negative: back). The view stays where the player put
// it while new output arrives, and returns to the live bottom when scrolled down to it, or on typing.
func (c *Client) historyScroll(n int) {
	c.MoreScroll(n)
	c.wheelHold = c.ui.Scr.Paused
}

// cmcpPageKeys: PageUp and PageDown scroll the history while a CMCP screen is up and the keyboard is
// not inside the screen (in focus mode they move the screen menu's highlight).
func (c *Client) cmcpPageKeys(s string) bool {
	if s != "\x1b[5~" && s != "\x1b[6~" || c.ui.Surf == nil {
		return false
	}
	if f := c.fg; f != nil && f.cs != nil && f.cs.st != nil && f.cs.st.Active {
		return false
	}
	page := max(c.ui.Winlines()-1, 1)
	if s == "\x1b[5~" {
		page = -page
	}
	c.historyScroll(page)
	return true
}
