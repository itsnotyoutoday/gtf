// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package client

// cmcp.go attaches the shared CMCP core (github.com/itsnotyoutoday/cmcp/go/go) to gtf: one
// session.Client per game connection, fed from the connection's GMCP messages, its output
// written back through the gmcp handler. Protocol logic lives in the core; this file and
// cmcpui.go only do platform glue (variables, commands, the terminal bell, drawing).

import (
	"encoding/json"
	"fmt"
	gmcpproto "github.com/itsnotyoutoday/gtf/internal/net/proto/gmcp"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/itsnotyoutoday/cmcp/go/frame"
	"github.com/itsnotyoutoday/cmcp/go/msg"
	"github.com/itsnotyoutoday/cmcp/go/pack"
	"github.com/itsnotyoutoday/cmcp/go/session"
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
// handler reads when GMCP turns on (it then adds the gate "Cobra 2" to Core.Supports.Set).
func (c *Client) cmcpEnv() *telnet.Env {
	e := telnet.NewEnv()
	on := c.cmcpWanted.Load() // set on the client goroutine at connect: the dialer runs elsewhere
	e.Do(func(i *telnet.EnvInfo) {
		if on {
			i.Extra["cmcp"] = "1"
		}
	})
	return e
}

// cmcpConfig describes this client to the server (Cobra.Hello).
func (c *Client) cmcpConfig(s *Session) session.Config {
	cfg := session.Config{
		Name: "GTF", Version: Version, Kind: "tty",
		Protos:         []int{2}, // CMCP 2 only (spec 20.2): never "Cobra 1"
		CmcpWait:       c.cmcpWaitFor(),
		LevelCap:       c.cmcpLevelCap(),
		Surfaces:       []string{"screen", "panel"}, // spec 20.14.2: gtf draws screens and cell panels, no docks or floats
		Render:         &msg.Render{Kind: "cell", FPS: 4},
		Themes:         cmcpThemeNames(),
		Theme:          c.cmcpTheme(),
		MaxBody:        65536, // the Welcome with the whole catalogue is about 20 KiB
		PackConsent:    c.consentMode(),
		ScreenCatalog:  pack.SessionCatalog, // the screen menu: the 14 built-in screens and the packages each needs
		LayerSendsGate: true,                // the gmcp handler sends Core.Hello and Core.Supports.Set
		Packs:          &msg.HelloPacks{Formats: []int{1}, Inline: true, HTTPS: true, Cache: c.cachedPacks(s.World.Host)},
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
		c.cmcpHoldCachedDefs(s)
	}
	now := c.cfg.Now()
	if on {
		s.cmScr().waitFrom = now
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
	c.cmcpDefPacks(s)
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
	case session.NoteReady:
		// one quiet line, so the player knows the keys; everything else lives behind /cmcp status
		if !s.cmScr().told && c.cmcpScreensOn() {
			s.cmScr().told = true
			c.echoTo(s.scr, "", "% CMCP: interface available. Tab: screen keys, F3: log view, F4: screen off, /cmcp status.")
		}
	case session.NotePackWanted:
		if n.PackRef != nil {
			c.cmcpPackWanted(s, *n.PackRef)
		}
	case session.NoteBye:
		// spec 20.2.2: every negotiation outcome is visible, never only under cmcp_debug
		c.cmcpTextMode(s, byeNotice(n.Text))
	case session.NoteDefFetch:
		if n.Blob != nil {
			c.cmcpDefFetch(s, *n.Blob)
		}
	case session.NoteDefApplied:
		c.cmcpDefApplied(s, n)
	case session.NoteDefRejected:
		c.cmcpTextMode(s, defRejectedNotice(n.Text))
	case session.NoteError:
		c.cmcpDebug("server error: %s", n.Text)
	case session.NoteState, session.NoteLog:
		c.cmcpDebug("%s", n.Text)
	}
}

// byeNotice is the player-facing line for a Cobra.Bye reason (spec 20.2.2, 20.3.4).
func byeNotice(reason string) string {
	switch reason {
	case msg.ByeNoCommonVersion:
		return "this game speaks CMCP 1 only: text mode"
	case msg.ByeProtocolError:
		return "the game and this client disagree about the protocol: text mode"
	}
	if reason == "" {
		return "the game ended CMCP: text mode"
	}
	return "the game ended CMCP (" + reason + "): text mode"
}

// cmcpTextMode tells the player once per connection that CMCP is not in use and the game stays a
// plain text session. The connection is never touched.
func (c *Client) cmcpTextMode(s *Session, why string) {
	cs := s.cmScr()
	if cs.textNote {
		return
	}
	cs.textNote = true
	c.cmcpDebug("text mode: %s", why)
	c.echoTo(s.scr, "", "% CMCP: "+why+".")
}

// cmcpLevelCap is %cmcp_level: the most gtf draws (1 text, 2 widgets, 3 full console look; default 3, spec 20.14.9).
func (c *Client) cmcpLevelCap() int {
	if n, err := strconv.Atoi(strings.TrimSpace(c.in.Var("cmcp_level"))); err == nil && n >= 1 && n <= 3 {
		return n
	}
	return 0
}

// defRejectedNotice is the line for a definition the client cannot use (spec 20.3.4).
func defRejectedNotice(why string) string {
	switch why {
	case "format":
		return "this game needs a newer client: text mode"
	case "hash":
		return "the game's definition failed its hash check: text mode"
	case "fetch":
		return "the game's definition could not be fetched: text mode"
	}
	return "the game's definition is not usable (" + why + "): text mode"
}

// cmcpWaitFor is %cmcp_wait: how long a game that stays silent after GMCP came on is given to
// answer Cobra.Server before gtf says it is staying a plain MUD client (default 3 s).
func (c *Client) cmcpWaitFor() time.Duration {
	if v := strings.TrimSpace(c.in.Var("cmcp_wait")); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f >= 0 {
			return time.Duration(f * float64(time.Second))
		}
	}
	return 3 * time.Second
}

// cmcpSilence checks the cmcp_wait timer of one session: GMCP is on, CMCP was offered, nothing
// came back (a v1-only server never answers "Cobra 2").
func (c *Client) cmcpSilence(s *Session, now time.Time) {
	cs := s.cmScr()
	if cs.textNote || cs.waitFrom.IsZero() {
		return
	}
	if u := s.cm.UI(); u.Server != nil || u.State != session.StateDeclared {
		cs.waitFrom = time.Time{} // answered (or closed): the timer is done
		return
	}
	if now.Sub(cs.waitFrom) >= c.cmcpWaitFor() {
		cs.waitFrom = time.Time{}
		c.cmcpTextMode(s, "this game does not answer CMCP 2 (it may speak CMCP 1 only): text mode")
	}
}

// cmcpTick drives the session timers (resync retries, command timeouts).
func (c *Client) cmcpTick() {
	now := c.cfg.Now()
	for _, s := range c.order {
		if s.cm != nil {
			s.cm.Tick(now)
			c.cmcpSilence(s, now)
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
	if u.V2 {
		l = append(l, c.cmcpStatusV2(u)...)
	} else {
		l = append(l, "% (protocol 1 state: no longer spoken by this build)")
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

// cachedPacks lists the packs held for a host (Hello.packs.cache) without creating the cache
// directory: a player who never met a pack leaves no files behind.
func (c *Client) cachedPacks(host string) []msg.CacheEntry {
	if c.cmPacks == nil {
		if _, err := os.Stat(filepath.Join(c.cfg.HomeDir, ".gtf", "cmcp", "packs")); err != nil {
			return nil
		}
	}
	return c.packStore().cache.Recent(host, 16)
}

// cmcpAfterRestore re-handshakes CMCP on the connections a /restart carried over: the new process
// has no session state, so it declares afresh (Core.Supports.Remove then Set, which a server treats
// as a new offer) and the server answers with Cobra.Server, Welcome, the console and full snapshots.
func (c *Client) cmcpAfterRestore() {
	if !c.cmcpEnabled() {
		return
	}
	for _, s := range c.order {
		if s.conn == nil || !s.open() || !s.conn.Enabled(telnet.OptGMCP, telnet.Them) {
			continue
		}
		c.cmcpWanted.Store(true)
		s.cm = nil
		c.cmcpOption(s, true)
		s.conn.SendProto("gmcp", "Core.Supports.Remove", []string{"Cobra"})
		s.conn.SendProto("gmcp", "Core.Supports.Set", append(append([]string(nil), gmcpproto.DefaultSupports...), gmcpproto.CobraGate))
	}
}

// cmcpKeyDefs gives the keys the CMCP screen consumes a harmless TF definition, so the engine's
// undefined-key error ("The key shift_tab is undefined") never appears when no screen is up. A
// definition the player made is left alone.
func (c *Client) cmcpKeyDefs(in *gtfparser.Interp) {
	for _, k := range []string{"shift_tab", "f2", "f3", "f4", "f5", "f6"} {
		if in.Var("cmcp_nodef_"+k) == "" {
			in.Exec("/def -q key_" + k + " = /test 1")
		}
	}
}

// cmcpStatusV2 is the protocol 2 part of /cmcp status: the definition, the scopes with their
// owners, the context stack with the types and the active layout (spec 20.4).
func (c *Client) cmcpStatusV2(u *session.UI) []string {
	var l []string
	switch {
	case u.Def != nil:
		l = append(l, fmt.Sprintf("%% definition: %s %s (%s...)", u.Def.ID, u.Def.Version, short(u.Def.SHA256)))
	case u.Server != nil && u.Server.Def != nil:
		l = append(l, fmt.Sprintf("%% definition: %s %s announced, not held yet", u.Server.Def.ID, u.Server.Def.Version))
	default:
		l = append(l, "% definition: none")
	}
	if u.TextClient {
		l = append(l, "% text client: the definition was refused ("+u.TextReason+")")
	}
	var names []string
	for n := range u.Scopes {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		sv := u.Scopes[n]
		o := "nobody"
		if sv.Owner != nil {
			o = sv.Owner.ID
			if sv.Owner.Name != "" {
				o = sv.Owner.Name + " (" + sv.Owner.ID + ")"
			}
			o += fmt.Sprintf(" epoch %d", sv.Owner.Epoch)
			if sv.Owner.AI {
				o += ", AI"
			}
		}
		l = append(l, fmt.Sprintf("%% scope %s: %s", n, o))
	}
	for i := len(u.Stack2) - 1; i >= 0; i-- {
		e := u.Stack2[i]
		mark := " "
		if e.Active {
			mark = "*"
		}
		x := fmt.Sprintf("%% context %s %s %s priority %d layout %s surface %s", mark, e.Key, e.Type, e.Priority, e.EffectiveLayout, e.Surface)
		if e.Readonly {
			x += " read-only"
		}
		if e.Surface != "" && e.Surface != "screen" {
			x += " (pane)"
		}
		l = append(l, x)
	}
	if len(u.Stack2) == 0 {
		l = append(l, "% contexts: none")
	}
	return l
}

func short(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}
