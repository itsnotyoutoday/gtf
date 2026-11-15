// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package client

// cmcp.go attaches the shared CMCP core (github.com/itsnotyoutoday/cmcp/go) to gtf: one
// session.Client per game connection, fed from the connection's GMCP messages, its output
// written back through the gmcp handler. Protocol logic lives in the core; this file and
// cmcpui.go only do platform glue (variables, commands, the terminal bell, drawing).

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/itsnotyoutoday/cmcp/frame"
	"github.com/itsnotyoutoday/cmcp/msg"
	"github.com/itsnotyoutoday/cmcp/session"
	"github.com/itsnotyoutoday/gtf/internal/net/telnet"
	"github.com/itsnotyoutoday/gtfparser"
)

// cmcpEnabled is the %cmcp setting (default on; "off", "0", "no" disable).
func (c *Client) cmcpEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(c.in.Var("cmcp"))) {
	case "off", "0", "no", "false":
		return false
	}
	return true
}

// cmcpEnv is the telnet environment of a new connection: it carries the CMCP switch the gmcp
// handler reads when GMCP turns on (it then adds "Cobra 1" to Core.Supports.Set).
func (c *Client) cmcpEnv() *telnet.Env {
	e := telnet.NewEnv()
	e.Do(func(i *telnet.EnvInfo) {
		if c.cmcpEnabled() {
			i.Extra["cmcp"] = "1"
		}
	})
	return e
}

// cmcpConfig describes this client to the server (Cobra.Hello).
func (c *Client) cmcpConfig(s *Session) session.Config {
	cfg := session.Config{
		Name: "GTF", Version: Version, Kind: "tty",
		Render:         &msg.Render{Kind: "cell", FPS: 4},
		Themes:         cmcpThemeNames(),
		Theme:          c.cmcpTheme(),
		MaxBody:        65536, // the Welcome with the whole catalogue is about 20 KiB
		LayerSendsGate: true,  // the gmcp handler sends Core.Hello and Core.Supports.Set
		Packs:          &msg.HelloPacks{Formats: []int{1}, Inline: true, HTTPS: true, Cache: c.packStore().cache.Recent(s.World.Host, 16)},
		Logf: func(f string, a ...any) {
			if c.in.Var("cmcp_debug") != "" {
				c.Post(func(*gtfparser.Interp) { c.echoLine("% cmcp: " + fmt.Sprintf(f, a...)) })
			}
		},
	}
	return cfg
}

// cmcpOption reacts to telnet option 201 changing on session s.
func (c *Client) cmcpOption(s *Session, on bool) {
	if !c.cmcpEnabled() {
		return
	}
	if s.cm == nil {
		s.cm = session.NewClient(c.cmcpConfig(s))
	}
	now := c.cfg.Now()
	if on {
		s.cm.TelnetEvent(session.WillGMCP, now)
	} else {
		s.cm.TelnetEvent(session.DontGMCP, now)
	}
	c.cmcpPump(s)
}

// cmcpReceive hands one GMCP message to the session (the generic GMCP store keeps it too).
func (c *Client) cmcpReceive(s *Session, m telnet.Message) {
	if s.cm == nil {
		return
	}
	f := frame.Frame{Package: m.Name}
	if m.Raw != "" {
		f.Payload = json.RawMessage(m.Raw)
	}
	if f.Package == msg.PkgPackData || f.Package == msg.PkgPackFail {
		c.cmcpPackFrame(s, f)
		return
	}
	s.cm.Receive(c.cfg.Now(), f)
	c.cmcpPump(s)
	c.cmcpEnsurePacks(s)
}

// cmcpPump moves what the session produced to the game and to the user.
func (c *Client) cmcpPump(s *Session) {
	if s.cm == nil {
		return
	}
	out := s.cm.Drain()
	for _, f := range out.Frames {
		if s.conn == nil {
			break
		}
		if err := s.conn.SendProto("gmcp", f.Package, f.Payload); err != nil {
			c.cmcpDebug("send %s: %v", f.Package, err)
		}
	}
	for _, n := range out.Notes {
		c.cmcpNote(s, n)
	}
	c.cmcpSounds(s, out.Sounds)
	if !out.Empty() {
		c.cmcpDirty(s)
	}
}

func (c *Client) cmcpDebug(f string, a ...any) {
	if c.in.Var("cmcp_debug") != "" {
		c.echoLine("% cmcp: " + fmt.Sprintf(f, a...))
	}
}

// cmcpNote shows the notifications that need a line of text.
func (c *Client) cmcpNote(s *Session, n session.Note) {
	switch n.Kind {
	case session.NoteReply, session.NoteCmdTimeout:
		c.cmcpReply(s, n)
	case session.NoteBye:
		c.cmcpDebug("ended: %s", n.Text)
	case session.NoteError:
		c.cmcpDebug("server error: %s", n.Text)
	case session.NoteState, session.NoteLog:
		c.cmcpDebug("%s", n.Text)
	}
}

// cmcpTick drives the session timers (resync retries, command timeouts).
func (c *Client) cmcpTick() {
	now := c.cfg.Now()
	for _, s := range c.order {
		if s.cm != nil {
			s.cm.Tick(now)
			c.cmcpPump(s)
		}
	}
}

// cmcpDirty repaints when the foreground session's model changed.
func (c *Client) cmcpDirty(s *Session) {
	if s == c.fg {
		c.scheduleRender()
	}
}

// cmcpStatus is /cmcp status.
func (c *Client) cmcpStatus(s *Session) []string {
	if !c.cmcpEnabled() {
		return []string{"% CMCP is off (%cmcp=off)."}
	}
	if s == nil || s.cm == nil {
		return []string{"% CMCP: no GMCP connection in the foreground."}
	}
	u := s.cm.UI()
	l := []string{fmt.Sprintf("%% CMCP: %s (protocol %d)", u.State, u.Proto)}
	if u.Server != nil {
		l = append(l, fmt.Sprintf("%% server: %s %s %s", u.Server.Server, u.Server.Game, u.Server.Build))
	}
	if u.HaveShip {
		l = append(l, fmt.Sprintf("%% ship: %s (%s) epoch %d%s", u.Ship.Name, u.Ship.ID, u.Ship.Epoch, map[bool]string{true: ", AI", false: ""}[u.AI]))
	}
	if u.Console != nil {
		l = append(l, fmt.Sprintf("%% console: %s (manned by %s)%s", u.Console.ID, u.Console.MannedBy, map[bool]string{true: ", read-only", false: ""}[u.Readonly]))
	} else {
		l = append(l, "% console: none")
	}
	if u.Screen.ID != "" {
		x := u.Screen.ID
		if u.Screen.Fallback {
			x += " (showing " + u.Screen.Resolved + " until the pack is available)"
		}
		l = append(l, "% screen: "+x)
	}
	if len(u.Subs) > 0 {
		var ss []string
		for _, sub := range u.Subs {
			ss = append(ss, fmt.Sprintf("%s[%s,seq %d]", sub.Pkg, sub.Source, u.Seqs[sub.Pkg]))
		}
		sort.Strings(ss)
		l = append(l, "% subscriptions: "+strings.Join(ss, " "))
	}
	l = append(l, fmt.Sprintf("%% commands: %d allowed now, %d in the catalogue, %d pending", len(u.Commands), len(u.Catalogue), u.Pending))
	return l
}

// cmcpClock is the poll hook (called every 250 ms from pollLoop).
func (c *Client) cmcpClock() { c.cmcpTick() }

var _ = time.Second
