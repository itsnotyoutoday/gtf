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
	_ gtfparser.Host        = (*Client)(nil)
	_ gtfparser.EchoHost    = (*Client)(nil)
	_ gtfparser.WorldHost   = (*Client)(nil)
	_ gtfparser.ConnectHost = (*Client)(nil)
	_ gtfparser.FgHost      = (*Client)(nil)
	_ gtfparser.SocketHost  = (*Client)(nil)
	_ gtfparser.IdleHost    = (*Client)(nil)
	_ gtfparser.TermHost    = (*Client)(nil)
	_ gtfparser.KeyHost     = (*Client)(nil)
	_ gtfparser.Notifier    = (*Client)(nil)
	_ gtfparser.HelpHost    = (*Client)(nil)
	_ gtfparser.MoreHost    = (*Client)(nil)
	_ gtfparser.WindowHost  = (*Client)(nil)
	_ gtfparser.LineHost    = (*Client)(nil)
	_ gtfparser.PromptHost  = (*Client)(nil)
	_ gtfparser.LimitHost   = (*Client)(nil)
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
		for _, ln := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
			c.ui.AddLine(scr, c.markupCells(ln, o.Attrs))
		}
		c.scheduleRender()
		return
	}
	c.echoTo(scr, o.Attrs, text)
}

// markupCells renders text containing @{attrs} markup.
func (c *Client) markupCells(text, base string) []layout.Cell {
	var out []layout.Cell
	cur := layout.Attr{}
	b := c.parseTfAttrs(base)
	cur = b.onto(cur)
	for len(text) > 0 {
		i := strings.Index(text, "@{")
		if i < 0 {
			out = append(out, ui.PlainCells(text, cur)...)
			break
		}
		j := strings.IndexByte(text[i:], '}')
		if j < 0 {
			out = append(out, ui.PlainCells(text, cur)...)
			break
		}
		out = append(out, ui.PlainCells(text[:i], cur)...)
		spec := text[i+2 : i+j]
		a := c.parseTfAttrs(spec)
		if a.none {
			cur = b.onto(layout.Attr{})
		}
		cur = a.onto(cur)
		text = text[i+j+1:]
	}
	return out
}

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

// Disconnect implements gtfparser.WorldHost.
func (c *Client) Disconnect(world string) error {
	s := c.sessions[world]
	if s == nil {
		return errNoWorld(world)
	}
	if s.conn == nil { // still dialing
		c.removeSession(s)
		return nil
	}
	s.close()
	return nil
}

// Foreground implements gtfparser.WorldHost.
func (c *Client) Foreground(world string) error { return c.ForegroundQuiet(world, false) }

// ForegroundQuiet implements gtfparser.FgHost.
func (c *Client) ForegroundQuiet(world string, quiet bool) error {
	if world == "" {
		c.foreground(nil)
		return nil
	}
	s := c.sessions[world]
	if s == nil {
		return errNoWorld(world)
	}
	c.foreground(s)
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

// Sockets implements gtfparser.SocketHost: the foreground first, then the
// most recently opened.
func (c *Client) Sockets() []gtfparser.SocketInfo {
	var out []gtfparser.SocketInfo
	add := func(s *Session) {
		held := s.scr.Held()
		out = append(out, gtfparser.SocketInfo{
			World: s.World.Name, Host: s.World.Host, Port: s.World.Port, State: s.state,
			SSL: s.World.SSL, Login: !s.opts.NoLogin, NewLines: held + s.scr.Unseen, OldLines: 0,
			Active: s != c.fg && s.scr.Unseen > 0, LastRecv: s.lastRecv, LastSend: s.lastSend,
		})
	}
	if c.fg != nil {
		add(c.fg)
	}
	for i := len(c.order) - 1; i >= 0; i-- {
		if c.order[i] != c.fg {
			add(c.order[i])
		}
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
	c.showWorldLine(s, s.ansi.Parse(line), line, nil)
}

// SetPrompt implements gtfparser.PromptHost.
func (c *Client) SetPrompt(world, text string) {
	if s := c.sessions[world]; s != nil {
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
	var res gtfparser.LineResult
	c.withWorld(s, func() { res = c.in.Process(plain, true) })
	if s.ansi.Bells > 0 {
		s.ansi.Bells = 0
		if c.in.TermSettings().Beep {
			c.ui.Beep() // TODO(merge): engine bell result
		}
	}
	if res.Gag {
		return
	}
	c.showWorldLine(s, cells, plain, res.Matches)
}

func (c *Client) showWorldLine(s *Session, cells []layout.Cell, plain string, ms []gtfparser.Match) {
	cells = c.applyMatches(cells, plain, ms)
	noHist, noLog, noActive := c.lineFlags(ms)
	attrs := ""
	if noHist {
		attrs += "G"
	}
	if noLog {
		attrs += "L"
	}
	c.ui.AddLine(s.scr, cells)
	c.in.RecordWorldLineAttr(s.World.Name, plain, attrs)
	if s != c.fg {
		if noActive {
			s.scr.Unseen--
			if s.scr.Unseen < 0 {
				s.scr.Unseen = 0
			}
		}
		if s.scr.Unseen > 0 && !s.activeNotified {
			s.activeNotified = true
			c.withWorld(s, func() { c.in.FireHook("ACTIVITY", s.World.Name) })
		}
		c.withWorld(s, func() { c.in.FireHook("BGTEXT", s.World.Name+" "+plain) })
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
