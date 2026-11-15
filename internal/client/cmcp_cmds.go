// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package client

import (
	"strconv"
	"strings"

	"github.com/itsnotyoutoday/cmcp/session"
	"github.com/itsnotyoutoday/gtfparser"
)

const cmcpUsage = `usage: /cmcp [status | packs | screen [id] | send <verb> [k=v ...] | focus | on | off]
  status        the CMCP state of the foreground world: handshake, ship, console, screen, subscriptions
  packs         layout packs known for this world and their consent state
  screen [id]   show the console screen (id: std.operations, <pack>/<screen>); no id: say which is shown
  send V k=v..  send a catalogue verb (like clicking its button); the equivalent text is echoed
  focus         toggle keyboard focus on the screen (also F2)
Settings (variables): %cmcp (on/off) %cmcp_theme %cmcp_ship_theme (off) %cmcp_bell (off) %cmcp_era (tng)
%cmcp_sounds_dir (stored, unused) %cmcp_pack_consent (ask|always|never) %cmcp_debug`

func (c *Client) cmdCmcp(in *gtfparser.Interp, args string) gtfparser.Value {
	args = strings.TrimSpace(args)
	sub, rest, _ := strings.Cut(args, " ")
	rest = strings.TrimSpace(rest)
	s := c.fg
	switch sub {
	case "", "status":
		for _, l := range c.cmcpStatus(s) {
			in.Host.Echo(l)
		}
	case "help", "?":
		for _, l := range strings.Split(cmcpUsage, "\n") {
			in.Host.Echo(l)
		}
	case "on", "off":
		in.SetVar("cmcp", sub)
		in.Host.Echo("% cmcp=" + sub + " (applies to connections opened from now on)")
		if sub == "off" && s != nil && s.cm != nil {
			s.cm.Disable(c.cfg.Now())
			c.cmcpPump(s)
		}
	default:
		if !c.cmcpSubcommand(in, s, sub, rest) {
			in.Host.Echo("% /cmcp: unknown subcommand " + sub)
			return gtfparser.Int(0)
		}
	}
	return gtfparser.Int(1)
}

// cmcpReply shows the outcome of a command that failed (a success needs no line: the game's own
// text output and the data change show it).
func (c *Client) cmcpReply(s *Session, n session.Note) {
	r := n.Result
	if r == nil || (r.OK && !r.TimedOut) {
		return
	}
	txt := r.Hint
	if txt == "" {
		txt = r.Err
	}
	if r.TimedOut {
		txt = "no answer"
	}
	line := "% " + r.Cmd + ": " + txt
	if r.HintCmd != "" {
		line += " (try: " + r.HintCmd + ")"
	}
	c.echoTo(s.scr, "", line)
}

// cmcpSounds: gtf plays nothing; with %cmcp_bell on it rings the terminal bell for the events
// of spec 9.9 (and for the Cobra.Event alert types, since a client without audio is not sent Sound.*).
func (c *Client) cmcpSounds(s *Session, reqs []session.SoundRequest) {
	for _, r := range reqs {
		if r.Kind == session.SoundPlay && r.Play != nil && bellEvents[r.Play.Event] {
			c.cmcpBell()
		}
	}
}

var bellEvents = map[string]bool{"red_alert": true, "yellow_alert": true, "proximity_warning": true, "comm_beep": true, "hail": true}

func (c *Client) cmcpBell() {
	switch strings.ToLower(c.in.Var("cmcp_bell")) {
	case "on", "1", "yes", "true":
		c.ui.Beep()
	}
}

func (c *Client) cmcpTheme() string {
	if t := strings.TrimSpace(c.in.Var("cmcp_theme")); t != "" {
		return t
	}
	return "plain"
}

func cmcpThemeNames() []string {
	return []string{"plain", "tng", "klingon", "romulan", "ferengi"}
}

func (c *Client) cmcpSubcommand(in *gtfparser.Interp, s *Session, sub, rest string) bool {
	echo := in.Host.Echo
	switch sub {
	case "packs":
		for _, l := range c.cmcpPacksList(s) {
			echo(l)
		}
	case "pack":
		verb, id, _ := strings.Cut(rest, " ")
		id = strings.TrimSpace(id)
		if s == nil || s.cm == nil {
			echo("% /cmcp pack: no CMCP connection in the foreground")
			break
		}
		switch verb {
		case "allow", "deny":
			if !c.packDecide(s, id, verb == "allow") {
				echo("% no pack is waiting for an answer")
			}
		default:
			echo("% usage: /cmcp pack allow|deny [id]")
		}
	case "screen":
		if s == nil || s.cm == nil {
			echo("% /cmcp screen: no CMCP connection in the foreground")
			break
		}
		cs := s.cmScr()
		if rest == "" {
			u := s.cm.UI()
			shown := u.Screen.Resolved
			if cs.override != "" {
				shown = cs.override
			}
			echo("% screen: server " + u.Screen.ID + ", showing " + shown)
			break
		}
		cs.override = rest
		c.scheduleRender()
	case "send":
		return c.cmcpSend(in, s, rest)
	case "focus":
		c.cmcpToggleFocus(s)
	default:
		return false
	}
	return true
}

// cmcpSend is /cmcp send <verb> [k=v ...]: the same path as a button (validated against the
// catalogue and the console's verbs, echoed, sent).
func (c *Client) cmcpSend(in *gtfparser.Interp, s *Session, rest string) bool {
	f := strings.Fields(rest)
	if len(f) == 0 || s == nil || s.cm == nil {
		in.Host.Echo("% usage: /cmcp send <verb> [name=value ...]")
		return true
	}
	args := map[string]any{}
	for _, kv := range f[1:] {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			in.Host.Echo("% /cmcp send: " + kv + " is not name=value")
			return true
		}
		if n, err := strconv.ParseFloat(v, 64); err == nil {
			args[k] = n
		} else if v == "true" || v == "false" {
			args[k] = v == "true"
		} else {
			args[k] = v
		}
	}
	t, err := s.cm.Command(c.cfg.Now(), f[0], args)
	if err != nil {
		in.Host.Echo("% /cmcp send: " + err.Error())
		return true
	}
	if t.Mutating && t.Echo != "" {
		c.cmcpEcho(s, t.Echo)
	}
	c.cmcpPump(s)
	return true
}
