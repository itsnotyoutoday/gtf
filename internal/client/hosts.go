// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package client

import (
	"strings"
	"time"

	"github.com/itsnotyoutoday/gtf/internal/layout"
	"github.com/itsnotyoutoday/gtf/internal/ui"
	"github.com/itsnotyoutoday/gtfparser"
)

// The Client is the engine's Host and implements its optional capability
// interfaces; the engine's own commands (/connect, /fg, /limit, /recall,
// /status_add, /dokey, ...) then drive this client's connections and display.
var (
	_ gtfparser.Host            = (*Client)(nil)
	_ gtfparser.EchoHost        = (*Client)(nil)
	_ gtfparser.WorldHost       = (*Client)(nil)
	_ gtfparser.ConnectHost     = (*Client)(nil)
	_ gtfparser.FgHost          = (*Client)(nil)
	_ gtfparser.SocketHost      = (*Client)(nil)
	_ gtfparser.IdleHost        = (*Client)(nil)
	_ gtfparser.TermHost        = (*Client)(nil)
	_ gtfparser.KeyHost         = (*Client)(nil)
	_ gtfparser.Notifier        = (*Client)(nil)
	_ gtfparser.HelpHost        = (*Client)(nil)
	_ gtfparser.MoreHost        = (*Client)(nil)
	_ gtfparser.WindowHost      = (*Client)(nil)
	_ gtfparser.LineHost        = (*Client)(nil)
	_ gtfparser.PromptHost      = (*Client)(nil)
	_ gtfparser.LimitHost       = (*Client)(nil)
	_ gtfparser.AttrEchoHost    = (*Client)(nil)
	_ gtfparser.HookDisplayHost = (*Client)(nil)
	_ gtfparser.WorldInfoHost   = (*Client)(nil)
)

// Echo implements gtfparser.Host: text for the user.
func (c *Client) Echo(text string) { c.echoLine(text) }

// EchoTo implements gtfparser.EchoHost (/echo -w -a -p).
func (c *Client) EchoTo(o gtfparser.EchoOpts, text string) {
	scr := c.screenFor()
	if o.World != "" {
		if s := c.sessions[o.World]; s != nil {
			scr = s.scr
		}
	}
	if o.Markup {
		var base gtfparser.Attr
		if o.Attrs != "" {
			base, _ = c.in.ParseAttrs(o.Attrs)
		}
		for _, ln := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
			as, _ := c.attrCtx().DecodeAttr(gtfparser.AttrString{Text: ln}, base, 0)
			c.addLine(scr, c.attrCells(as))
		}
		c.scheduleRender()
		return
	}
	c.echoTo(scr, o.Attrs, text)
}

// EchoAttr implements gtfparser.AttrEchoHost: attributed text from the engine.
func (c *Client) EchoAttr(s gtfparser.AttrString) {
	if c.addLine(c.screenFor(), c.attrCells(s)) {
		return
	}
	c.in.RecordLocalLine(s.Text)
	c.scheduleRender()
}

// HookMessage implements gtfparser.HookDisplayHost: tf's default messages for
// hook events (connected, closed, activity, ...), placed by class. A message
// about a world is output in that world (as if the server had sent it); when
// that world is not the foreground one it is also an alert, so it shows on the
// status line. Alerts are drawn over the status row (alert.go).
func (c *Client) HookMessage(m gtfparser.HookMessage) {
	if c.capture != nil { // a remote tf.exec: these events are failures of the command
		switch m.Event {
		case "NOMACRO", "LOADFAIL", "CONFAIL", "ICONFAIL":
			c.captureWarn = true
		}
	}
	switch m.Class {
	case gtfparser.HookWorld:
		s := c.sessions[m.World]
		if s == nil {
			c.echoFg(m.Text)
			break
		}
		c.worldOutput(s, m.Text)
		if s != c.fg {
			c.alertOrEcho(m.Text)
		}
	case gtfparser.HookAlert:
		c.alertOrEcho(m.Text)
	default:
		c.echoFg(m.Text)
	}
	if m.XSock {
		if s := c.sessions[m.World]; s != nil {
			s.alertID = c.alertSeq
		}
	}
}

// echoFg prints a message on the foreground screen.
func (c *Client) echoFg(t gtfparser.AttrString) {
	if c.addLine(c.fgScreen(), c.attrCells(t)) {
		return
	}
	c.in.RecordLocalLine(t.Text)
	c.scheduleRender()
}

// alertOrEcho shows msg as an alert, or as a line when there is no status line.
func (c *Client) alertOrEcho(t gtfparser.AttrString) {
	if !c.in.TermSettings().Visual {
		c.echoFg(t)
		return
	}
	c.raiseAlert(t.Text)
}

// worldOutput displays text in s's world as a line of that world's output
// (tf: world_output): history, log, the screen, and the activity bookkeeping
// when the world is in the background. Triggers do not see it.
func (c *Client) worldOutput(s *Session, t gtfparser.AttrString) {
	c.showWorldLine(s, c.attrCells(t), t.Text, nil)
	c.scheduleRender()
}

// WorldInfoHost (for /def -w and -T and hook world context).

// CurrentWorld is the world whose text or event is being processed.
func (c *Client) CurrentWorld() string {
	if c.xworld != "" {
		return c.xworld
	}
	return c.ForegroundWorld()
}

// WorldType returns a world's type.
func (c *Client) WorldType(world string) string {
	if w, ok := c.in.World(world); ok {
		return w.Type
	}
	return ""
}

// WorldExists reports whether a world is defined.
func (c *Client) WorldExists(world string) bool { _, ok := c.in.World(world); return ok }

// Send implements gtfparser.Host: send a line to a world.
func (c *Client) Send(world, text string) bool {
	s := c.pick(world)
	if s == nil || !s.open() {
		return false
	}
	return s.sendLine(text) == nil
}

// pick resolves a world name ("" = the interpreter's current world, else the
// foreground one).
func (c *Client) pick(world string) *Session {
	if world == "" {
		world = c.in.CurrentWorld()
	}
	if world == "" {
		return c.fg
	}
	return c.sessions[world]
}

// ---- WorldHost, ConnectHost, FgHost, SocketHost, IdleHost ----

// Connect implements gtfparser.WorldHost.
func (c *Client) Connect(world string) error {
	return c.ConnectWith(world, gtfparser.ConnectOptions{})
}

// ConnectWith implements gtfparser.ConnectHost.
func (c *Client) ConnectWith(world string, o gtfparser.ConnectOptions) error {
	w, ok := c.in.World(world)
	if !ok {
		return errNoWorld(world)
	}
	return c.connect(w, o)
}

type errNoWorld string

func (e errNoWorld) Error() string { return "No world " + string(e) }

// Disconnect implements gtfparser.WorldHost (/dc): tf reports the close in
// the world (and on the foreground screen when that is another world), and the
// socket becomes a zombie. No DISCONNECT hook runs.
func (c *Client) Disconnect(world string) error {
	s := c.sessions[world]
	if s == nil {
		return errNoWorld(world)
	}
	s.flushPartial()
	msg := gtfparser.AttrString{Text: "% Connection to " + s.World.Name + " closed."}
	c.worldOutput(s, msg)
	if s != c.fg {
		c.echoFg(msg)
	}
	s.endReason = "client_quit"
	c.zombie(s)
	return nil
}

// Foreground implements gtfparser.WorldHost.
func (c *Client) Foreground(world string) error { return c.ForegroundQuiet(world, false) }

// ForegroundQuiet implements gtfparser.FgHost.
func (c *Client) ForegroundQuiet(world string, quiet bool) error {
	if world == "" {
		c.foreground(nil, quiet)
		return nil
	}
	s := c.sessions[world]
	if s == nil {
		return errNoWorld(world)
	}
	c.foreground(s, quiet)
	return nil
}

// ForegroundWorld implements gtfparser.WorldHost.
func (c *Client) ForegroundWorld() string {
	if c.fg == nil {
		return ""
	}
	return c.fg.World.Name
}

// Connected implements gtfparser.WorldHost.
func (c *Client) Connected(world string) bool {
	s := c.sessions[world]
	return s != nil && s.open()
}

// Sockets implements gtfparser.SocketHost: tf's socket list, oldest first
// (what "/fg" without an argument selects is the first).
func (c *Client) Sockets() []gtfparser.SocketInfo {
	out := make([]gtfparser.SocketInfo, 0, len(c.order))
	for _, s := range c.order {
		held := s.scr.Held()
		host, port := s.World.Host, s.World.Port
		ip := ""
		if i := strings.IndexByte(s.addr, ' '); i > 0 {
			ip = s.addr[:i]
		}
		if s.dead() {
			// tf frees the dead socket's host and port strings but keeps its address
			host, port = "(null)", "(null)"
		}
		out = append(out, gtfparser.SocketInfo{
			World: s.World.Name, Host: host, Port: port, Addr: ip, State: s.state,
			SSL: s.World.SSL, Login: !s.opts.NoLogin, NewLines: held + s.scr.Unseen, OldLines: 0,
			Active: s != c.fg && s.scr.Unseen > 0, LastRecv: s.lastRecv, LastSend: s.lastSend,
		})
	}
	return out
}

// LastKeyboard implements gtfparser.IdleHost.
func (c *Client) LastKeyboard() time.Time {
	if t := c.in.LastKeyTime(); !t.IsZero() {
		return t
	}
	return c.started
}

// ---- terminal facts ----

// Size implements gtfparser.TermHost (columns and lines of the screen).
func (c *Client) Size() (int, int) { return c.ui.W, c.ui.H }

// KeyCode implements gtfparser.KeyHost for xterm-compatible terminals.
func (c *Client) KeyCode(name string) string { return keycode(name) }

// WinLines implements gtfparser.WindowHost.
func (c *Client) WinLines() int { return c.ui.Winlines() }

func keycode(name string) string {
	m := map[string]string{
		"Up": "\x1b[A", "Down": "\x1b[B", "Right": "\x1b[C", "Left": "\x1b[D",
		"Insert": "\x1b[2~", "Delete": "\x1b[3~", "Home": "\x1b[H", "End": "\x1b[F",
		"PgUp": "\x1b[5~", "PgDn": "\x1b[6~",
		"F1": "\x1bOP", "F2": "\x1bOQ", "F3": "\x1bOR", "F4": "\x1bOS", "F5": "\x1b[15~",
		"F6": "\x1b[17~", "F7": "\x1b[18~", "F8": "\x1b[19~", "F9": "\x1b[20~", "F10": "\x1b[21~",
		"F11": "\x1b[23~", "F12": "\x1b[24~",
	}
	return m[name]
}

// ---- paging ----

func (c *Client) screenOf(world string) *ui.Screen {
	if world == "" {
		return c.ui.Scr
	}
	if s := c.sessions[world]; s != nil {
		return s.scr
	}
	return c.ui.Scr
}

// MorePaused implements gtfparser.MoreHost.
func (c *Client) MorePaused(world string) bool { return c.screenOf(world).Paused }

// MoreSize implements gtfparser.MoreHost.
func (c *Client) MoreSize(flags, world string) int { return c.screenOf(world).Held() }

// MoreScroll implements gtfparser.MoreHost (morescroll).
func (c *Client) MoreScroll(n int) int {
	scr := c.ui.Scr
	before := scr.Vis()
	scr.Scroll(n, c.ui.Winlines())
	d := scr.Vis() - before
	if d < 0 {
		d = -d
	}
	c.scheduleRender()
	return d
}

// ---- lines, prompts, limits ----

// DisplayLine implements gtfparser.LineHost (fake_recv after triggers).
func (c *Client) DisplayLine(world, line string) {
	s := c.sessions[world]
	if s == nil {
		c.echoLine(line)
		return
	}
	c.gameLine = true
	c.showWorldLine(s, s.ansi.Parse(line), line, nil)
	c.gameLine = false
}

// SetPrompt implements gtfparser.PromptHost.
func (c *Client) SetPrompt(world, text string) {
	if s := c.sessions[world]; s != nil {
		if s.prompt != text {
			s.promptCells = nil // a script changed the prompt: no colours to keep
		}
		s.prompt = text
	}
}

// Redraw implements gtfparser.LimitHost: a /limit filter changed.
func (c *Client) Redraw(world string) int {
	scr := c.screenOf(world)
	n := scr.Refilter()
	c.ui.Redraw()
	c.scheduleRender()
	return n
}

// HelpHost lives in help.go.

// processLine is the pipeline for one line received from a world:
// triggers and gags, attributes, display, history and log.
func (c *Client) processLine(s *Session, line string) {
	cells := s.ansi.Parse(line)
	plain := ui.StripCells(cells)
	if s.ansi.Bells > 0 {
		s.ansi.Bells = 0
		c.bell()
	}
	var res *gtfparser.LineResult
	c.withWorld(s, func() { res = c.in.ProcessText(gtfparser.LineInput{Text: plain, World: s.World.Name}) })
	if res.Bell {
		c.bell()
	}
	if res.Gag || s.isQuiet(plain) {
		return
	}
	c.showWorldLine(s, c.overlay(cells, plain, res), res.Line.Text, res)
}

func (c *Client) bell() {
	if c.in.TermSettings().Beep {
		c.ui.Beep()
	}
}

func (c *Client) showWorldLine(s *Session, cells []layout.Cell, plain string, res *gtfparser.LineResult) {
	attrs := ""
	noActive := false
	if res != nil {
		if res.NoHistory {
			attrs += "G"
		}
		if res.NoLog {
			attrs += "L"
		}
		noActive = res.NoActivity
	}
	fgNow := s == c.fg
	if !fgNow && s.scr.Unseen == 0 && !noActive {
		c.withWorld(s, func() { c.in.FireHookArgs("PREACTIVITY", s.World.Name) })
	}
	c.tapGame = res != nil || c.gameLine
	c.addLine(s.scr, cells)
	c.tapGame = false
	c.in.RecordWorldLineAttr(s.World.Name, plain, attrs)
	if !fgNow {
		if noActive {
			s.scr.Unseen = max(s.scr.Unseen-1, 0)
		}
		if s.scr.Unseen > 0 && !s.activeNotified {
			s.activeNotified = true
			c.withWorld(s, func() { c.in.FireHookArgs("ACTIVITY", s.World.Name) })
		}
		c.withWorld(s, func() { c.in.FireHookArgs("BGTEXT", s.World.Name) })
		if res != nil && res.Count > 0 {
			c.withWorld(s, func() { c.in.FireHookArgs("BGTRIG", s.World.Name, plain) })
		}
	}
}

// Notify implements gtfparser.Notifier: the engine tells the client about
// state changes the display or terminal must act on.
func (c *Client) Notify(e gtfparser.Event) {
	switch e.Kind {
	case "beep":
		n := 1
		if len(e.Args) > 0 {
			n = atoi(e.Args[0], 1)
		}
		for i := 0; i < n; i++ {
			c.ui.Beep()
		}
	case "localecho":
		// the engine already recorded the setting; a real telnet client
		// would negotiate ECHO here
	case "suspend":
		if c.cfg.Suspend == nil {
			c.echoLine("% /suspend: not supported by this terminal")
			return
		}
		c.cfg.Suspend()
		c.ui.Resize()
		c.ui.Redraw()
	case "dokey":
		if len(e.Args) == 0 {
			return
		}
		win := c.ui.Winlines()
		switch e.Args[0] {
		case "REDRAW", "REFRESH":
			c.ui.Redraw()
		case "CLEAR":
			c.ui.Clear()
		case "PAUSE":
			c.ui.Scr.Pause()
		case "FLUSH", "SELFLUSH":
			c.ui.Scr.Flush(win)
		}
	}
	c.scheduleRender()
}
