// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package client

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/itsnotyoutoday/gtf/internal/cmcpui"
)

// The width of the game output in the side layout. %cmcp_text_width is `auto` (the 80-column cap of
// the spec) or a number of columns; `/cmcp width` and the narrow/widen keys (Alt+, and Alt+. by
// default, %cmcp_key_narrow and %cmcp_key_widen) change it by 4 columns at a time and remember it in
// ~/.gtf/cmcp/prefs.json. It is clamped to what leaves the widget column readable; the notice line
// says why when a limit is reached. (TinyFugue's /wrap is untouched: it wraps the text, this sizes the
// pane the text is in.)

const widthStep = 4

func (c *Client) prefsFile() string {
	return filepath.Join(c.cfg.HomeDir, ".gtf", "cmcp", "prefs.json")
}

func (c *Client) loadWidthPref() int {
	if c.cmWidthLoaded {
		return c.cmWidthPref
	}
	c.cmWidthLoaded = true
	var p struct {
		TextWidth int `json:"text_width"`
	}
	if b, err := os.ReadFile(c.prefsFile()); err == nil && json.Unmarshal(b, &p) == nil {
		c.cmWidthPref = p.TextWidth
	}
	return c.cmWidthPref
}

func (c *Client) saveWidthPref(n int) {
	c.cmWidthLoaded, c.cmWidthPref = true, n
	b, _ := json.Marshal(map[string]int{"text_width": n})
	if os.MkdirAll(filepath.Dir(c.prefsFile()), 0o700) == nil {
		_ = os.WriteFile(c.prefsFile(), b, 0o600)
	}
}

// cmcpTextWidth is the wanted output width: the variable if it is set, else the remembered one;
// 0 means auto.
func (c *Client) cmcpTextWidth() int {
	switch v := strings.ToLower(strings.TrimSpace(c.in.Var("cmcp_text_width"))); v {
	case "", "auto":
		if v == "auto" {
			return 0
		}
		return c.loadWidthPref()
	default:
		n, _ := strconv.Atoi(v)
		return max(n, 0)
	}
}

// cmcpSetWidth applies `/cmcp width <cols|auto|+N|-N>` and the keys: arg is the text typed, or a signed
// step. It reports what happened on the notice line.
func (c *Client) cmcpSetWidth(s *Session, arg string) {
	cs := s.cmScr()
	cur := 0
	if cs.frame != nil {
		cur = cs.frame.Text.W
	}
	if cs.frame == nil || !cs.frame.Side {
		c.cmcpNotice(s, "The text width applies in the side layout (a window of 120 columns or more)")
		if arg == "" {
			return
		}
	}
	arg = strings.ToLower(strings.TrimSpace(arg))
	want := 0
	switch {
	case arg == "":
		c.cmcpNotice(s, fmt.Sprintf("Text width: %s (%d columns now); %d to %s, /cmcp width <cols|auto|+N|-N>", c.widthLabel(), cur, cmcpui.MinTextCols, "what the window allows"))
		return
	case arg == "auto":
		c.saveWidthPref(0)
		c.in.SetVar("cmcp_text_width", "auto")
		c.cmcpNotice(s, "Text width: auto (80 columns)")
		c.scheduleRender()
		return
	case arg[0] == '+' || arg[0] == '-':
		n, err := strconv.Atoi(arg)
		if err != nil {
			c.cmcpNotice(s, "usage: /cmcp width <cols|auto|+N|-N>")
			return
		}
		want = cur + n
	default:
		n, err := strconv.Atoi(arg)
		if err != nil {
			c.cmcpNotice(s, "usage: /cmcp width <cols|auto|+N|-N>")
			return
		}
		want = n
	}
	// inner width of the area: the frame's text rect plus the widget column is what the layout clamps by;
	// ask the same function the layout uses
	area := c.ui.W
	if cs.frame != nil {
		area = cs.frame.Area
	}
	eff, note := cmcpui.ClampTextWidth(area, want)
	c.saveWidthPref(eff)
	c.in.SetVar("cmcp_text_width", strconv.Itoa(eff))
	if note == "" {
		note = fmt.Sprintf("Text width: %d columns", eff)
	}
	c.cmcpNoteKey(s, "textwidth", note, cmcpui.NoticeInfo)
	c.scheduleRender()
}

func (c *Client) widthLabel() string {
	if n := c.cmcpTextWidth(); n > 0 {
		return strconv.Itoa(n)
	}
	return "auto"
}
