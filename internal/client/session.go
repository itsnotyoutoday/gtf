// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package client

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/itsnotyoutoday/gtf/internal/client/data"
	"github.com/itsnotyoutoday/gtf/internal/layout"
	gnet "github.com/itsnotyoutoday/gtf/internal/net"
	"github.com/itsnotyoutoday/gtf/internal/net/telnet"
	"github.com/itsnotyoutoday/gtf/internal/ui"
	"github.com/itsnotyoutoday/gtfparser"
)

// World is a world definition (the engine's database entry).
type World = gtfparser.WorldInfo

// Session is one world connection with its own screen.
type Session struct {
	c      *Client
	World  World
	conn   *gnet.Conn
	scr    *ui.Screen
	ansi   ui.AnsiParser
	store  *data.Store
	state  gtfparser.SockState
	pend   string // partial line (no newline yet)
	prompt string // current prompt text (plain)
	opts   gtfparser.ConnectOptions
	addr   string // "ip port" being tried / connected to
	// numquiet is tf's "quiet login": how many of the server's first lines are
	// still to be hidden (/connect -q with automatic login)
	numquiet int

	cancel         context.CancelFunc // aborts a dial in progress
	alertID        uint64             // the status-line alert this world raised last (see alert.go)
	pendTimer      *time.Timer
	connectedAt    time.Time
	lastRecv       time.Time
	lastSend       time.Time
	activeNotified bool
	logfile        io.Closer
}

func (s *Session) open() bool { return s.state == gtfparser.SockConnected }
func (s *Session) dead() bool { return s.state >= gtfparser.SockZombie }

func (c *Client) newSession(w World, o gtfparser.ConnectOptions, scr *ui.Screen) *Session {
	now := c.cfg.Now()
	s := &Session{c: c, World: w, store: data.New(), scr: scr, opts: o, state: gtfparser.SockResolving, lastRecv: now, lastSend: now}
	if scr == nil {
		s.scr = ui.NewScreen(w.Name, 20000)
		c.wscreens[w.Name] = s.scr
	}
	s.scr.More = c.in.TermSettings().More
	s.scr.OnPause = func() { c.in.FireHook("MORE", "") }
	name := w.Name
	s.scr.SB.Keep = func(cells []layout.Cell) bool { return !c.in.LimitedFor(name, ui.StripCells(cells), "") }
	return s
}

// fgContext reports whether the command or event being processed belongs to
// the foreground world (tf: xsock == fsock); a /connect made from there brings
// the new connection to the foreground, one made from a background world's
// trigger leaves it in the background.
func (c *Client) fgContext() bool {
	cw := c.in.CurrentWorld()
	if c.fg == nil {
		return cw == ""
	}
	return cw == "" || cw == c.fg.World.Name
}

// connect starts a connection to w; events arrive via the runner. As in tf
// (opensock) the new world is brought to the foreground first, so the
// progress messages appear on its own, empty screen; a failed connection
// stays there as a dead socket until another world replaces it.
func (c *Client) connect(w World, o gtfparser.ConnectOptions) error {
	old := c.sessions[w.Name]
	if old != nil && !old.dead() {
		return fmt.Errorf("socket to %s already exists", w.Name)
	}
	toFg := o.Foreground || (!o.Background && c.fgContext())
	// a dead socket of the same world is recycled: same place in the list, same screen
	var scr *ui.Screen
	if old != nil {
		scr = old.scr
	} else if ws := c.wscreens[w.Name]; ws != nil {
		scr = ws
	}
	s := c.newSession(w, o, scr)
	if o.Quiet && !o.NoLogin && !c.cfg.NoLogin && w.Character != "" {
		s.numquiet = maxQuiet
	}
	c.sessions[w.Name] = s
	if old != nil {
		for i, x := range c.order {
			if x == old {
				c.order[i] = s
			}
		}
		if c.fg == old {
			c.fg = s
		}
	} else {
		c.order = append(c.order, s)
	}
	if toFg {
		c.foreground(s, false)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	s.cancel = cancel
	tr := &DialTrace{
		Lookup: func() {
			c.Post(func(*gtfparser.Interp) {
				if s.state == gtfparser.SockResolving {
					c.fireWorld(s, gtfparser.HookEvent{Name: "PENDING", Args: []string{w.Name}, World: w.Name})
					c.afterTask()
				}
			})
		},
		Try: func(addr string) {
			c.Post(func(*gtfparser.Interp) {
				if !s.dead() {
					s.state = gtfparser.SockConnecting
					s.addr = addr
					c.fireWorld(s, gtfparser.HookEvent{Name: "PENDING", Args: []string{w.Name, addr}, World: w.Name})
					c.afterTask()
				}
			})
		},
		Failed: func(addr, why string) {
			c.Post(func(*gtfparser.Interp) {
				if !s.dead() {
					c.fireWorld(s, gtfparser.HookEvent{Name: "ICONFAIL", Args: []string{w.Name, addr + ":", why}, World: w.Name})
					c.afterTask()
				}
			})
		},
	}
	go func() {
		defer cancel()
		conn, err := c.cfg.Dialer.Dial(ctx, w, tr)
		c.Post(func(*gtfparser.Interp) {
			defer c.afterTask()
			if s.dead() { // /dc while connecting
				if conn != nil {
					conn.Close()
				}
				return
			}
			if err != nil {
				c.connectFailed(s, err)
				return
			}
			s.conn = conn
			s.state = gtfparser.SockConnected
			s.connectedAt = c.cfg.Now()
			conn.Resize(c.ui.W, c.ui.H)
			c.setEnv(conn)
			c.fireWorld(s, gtfparser.HookEvent{Name: "CONNECT", Args: []string{w.Name}, World: w.Name})
			if !c.cfg.NoLogin && !o.NoLogin && w.Character != "" && w.Password != "" && loginOn(c.in.Var("login")) {
				c.withWorld(s, func() { c.in.FireHookArgs("LOGIN", w.Name, w.Character, w.Password) })
			}
			go c.pump(s, conn)
		})
	}()
	return nil
}

// maxQuiet is tf's MAXQUIET: lines hidden by a quiet login.
const maxQuiet = 25

// isQuiet is tf's is_quiet: whether this line of the server's login output is
// hidden. The count ends early at the end of a typical login banner.
func (s *Session) isQuiet(line string) bool {
	if s.numquiet == 0 {
		return false
	}
	s.numquiet--
	if s.numquiet == 0 {
		return true
	}
	if strings.HasPrefix(line, "Use the WHO command") || line == "### end of messages ###" {
		s.numquiet = 0
	}
	return true
}

// fireWorld fires an event on behalf of s's world.
func (c *Client) fireWorld(s *Session, ev gtfparser.HookEvent) *gtfparser.HookResult {
	var r *gtfparser.HookResult
	c.withWorld(s, func() { r = c.in.FireHookEvent(ev) })
	return r
}

// connectFailed is tf's CONFAIL: the socket becomes a zombie and the failure
// is reported in its world.
func (c *Client) connectFailed(s *Session, err error) {
	name := s.World.Name
	c.zombie(s)
	var de *DialError
	if !errors.As(err, &de) {
		de = &DialError{Addr: s.World.Host + " " + s.World.Port, Why: err.Error()}
	}
	if de.Lookup {
		// tf's CONFAILHP wording
		c.fireWorld(s, gtfparser.HookEvent{Name: "CONFAIL", World: name,
			Args:    []string{name, s.World.Host, s.World.Port + ":", de.Why},
			Message: "% Unable to connect to " + name + ": " + s.World.Host + " " + s.World.Port + ": " + de.Why, HasMsg: true})
		return
	}
	c.fireWorld(s, gtfparser.HookEvent{Name: "CONFAIL", Args: []string{name, de.Addr + ":", de.Why}, World: name})
}

// withWorld runs f with the interpreter acting for s's world.
func (c *Client) withWorld(s *Session, f func()) {
	old, oldx := c.in.CurrentWorld(), c.xworld
	c.in.SetWorld(s.World.Name)
	c.xworld = s.World.Name
	defer func() { c.in.SetWorld(old); c.xworld = oldx }()
	f()
}

func (c *Client) setEnv(conn *gnet.Conn) {
	conn.Env().Do(func(e *telnet.EnvInfo) {
		e.TermName = strings.ToLower(envOr("TERM", "xterm"))
		e.Color256 = strings.Contains(e.TermName, "256") || c.ui.Depth >= 256
		ct := envOr("COLORTERM", "")
		e.TrueColor = strings.Contains(ct, "truecolor") || strings.Contains(ct, "24bit")
		e.Cols, e.Rows = c.ui.W, c.ui.H
		e.ClientName, e.ClientVer = "GTF", Version
	})
}

// pump forwards connection events to the runner.
func (c *Client) pump(s *Session, conn *gnet.Conn) {
	for ev := range conn.Events() {
		ev := ev
		c.Post(func(*gtfparser.Interp) { c.onNet(s, ev); c.afterTask() })
	}
}

func (c *Client) onNet(s *Session, ev gnet.Event) {
	switch e := ev.(type) {
	case gnet.Text:
		s.lastRecv = c.cfg.Now()
		s.feed(string(e.Data))
	case gnet.Prompt:
		s.promptEnd()
	case gnet.OptionChange:
		if e.Opt == telnet.OptEcho && e.Side == telnet.Them {
			// the server will echo: do not echo locally (password entry)
			c.in.SetLocalEcho(s.World.Name, !e.On)
		}
	case gnet.Message:
		c.onMessage(s, e)
	case gnet.Subneg:
		// unhandled subnegotiation: ignored
	case gnet.Closed:
		if s.dead() || s.conn == nil { // closed by /dc, or a stale event of a recycled socket
			return
		}
		s.flushPartial()
		c.zombie(s)
		c.fireWorld(s, gtfparser.HookEvent{Name: "DISCONNECT", Args: []string{s.World.Name}, World: s.World.Name})
	}
}

// close releases the socket's resources (the state is the caller's business).
func (s *Session) close() {
	if s.cancel != nil {
		s.cancel()
	}
	if s.conn != nil {
		s.conn.Close()
	}
	if s.logfile != nil {
		s.logfile.Close()
		s.logfile = nil
	}
}

func (s *Session) resize(w, h int) {
	if s.conn != nil {
		s.conn.Resize(w, h)
	}
}

func (s *Session) sendLine(text string) error {
	if s.conn == nil || !s.open() {
		return fmt.Errorf("not connected")
	}
	s.lastSend = s.c.cfg.Now()
	return s.conn.SendLine(text)
}

// feed splits incoming text into lines and runs each through the pipeline.
func (s *Session) feed(txt string) {
	s.pend += txt
	for {
		i := strings.IndexByte(s.pend, '\n')
		if i < 0 {
			break
		}
		line := strings.TrimRight(s.pend[:i], "\r")
		s.pend = s.pend[i+1:]
		s.setPrompt("")
		s.c.processLine(s, line)
	}
	if s.pendTimer != nil {
		s.pendTimer.Stop()
		s.pendTimer = nil
	}
	if s.pend != "" {
		c := s.c
		s.pendTimer = time.AfterFunc(250*time.Millisecond, func() {
			c.Post(func(*gtfparser.Interp) {
				if s.pend != "" {
					s.promptEnd()
					c.afterTask()
				}
			})
		})
	}
}

func (s *Session) setPrompt(text string) {
	if text != "" {
		s.numquiet = 0
	}
	s.prompt = text
	s.c.in.SetPrompt(s.World.Name, text)
}

// promptEnd turns the partial line into a prompt (GA/EOR/timeout).
func (s *Session) promptEnd() {
	if s.pend == "" {
		return
	}
	line := strings.TrimRight(s.pend, "\r")
	s.pend = ""
	var p ui.AnsiParser
	plain := ui.StripCells(p.Parse(line))
	s.c.withWorld(s, func() {
		if r := s.c.in.FireHookArgs("PROMPT", plain); r.Count == 0 {
			s.setPrompt(plain)
		}
	})
}

// flushPartial shows any partial line (at disconnect).
func (s *Session) flushPartial() {
	if s.pend != "" {
		line := s.pend
		s.pend = ""
		s.c.processLine(s, line)
	}
	s.prompt = ""
}

// zombie ends the connection but keeps the socket (tf: zombiesock): its
// screen stays where it is, the status line shows the world as dead, and it
// is forgotten once it has been in the foreground and left it.
func (c *Client) zombie(s *Session) {
	if s.dead() {
		return
	}
	s.state = gtfparser.SockZombie
	s.close()
	s.setPrompt("")
	s.pend = ""
}

// reap forgets sockets that are dead (tf: nuke_dead_socks); a temporary
// world goes with its socket, a defined one keeps its screen.
func (c *Client) reap() {
	for i := 0; i < len(c.order); {
		s := c.order[i]
		if s.state != gtfparser.SockDead || s == c.fg {
			i++
			continue
		}
		c.order = append(c.order[:i], c.order[i+1:]...)
		if c.sessions[s.World.Name] == s {
			delete(c.sessions, s.World.Name)
		}
		if s.alertID != 0 && s.alertID == c.alertSeq {
			c.clearAlert()
		}
		c.in.WorldClosed(s.World.Name)
		if !c.WorldExists(s.World.Name) {
			delete(c.wscreens, s.World.Name)
		}
	}
}

// foreground makes s (or the base screen when nil) the displayed world
// (tf: fg_sock).
func (c *Client) foreground(s *Session, quiet bool) {
	if c.fg == s {
		return
	}
	if old := c.fg; old != nil && old.state == gtfparser.SockZombie {
		old.state = gtfparser.SockDead // its text has been seen
	}
	c.fg = s
	virt := c.in.TermSettings().Virtscreen
	if s == nil {
		c.ui.SetScreen(c.base)
		c.in.SetWorld("")
		// with %virtscreen on every world has its own screen, so there is no
		// "switch" line to print
		c.in.FireHookEvent(gtfparser.HookEvent{Name: "WORLD", Message: "---- No world ----", HasMsg: virt == false, NoMessage: virt})
		c.reap()
		return
	}
	if s.alertID != 0 && s.alertID == c.alertSeq {
		c.clearAlert()
	}
	s.alertID = 0
	if virt && s.scr.Unseen > 0 && s.scr.SB.Logical() > s.scr.Unseen {
		// (a divider only separates new lines from lines that were shown before)
		c.textDivider(s.scr)
	}
	s.scr.Unseen = 0
	s.activeNotified = false
	c.ui.SetScreen(s.scr)
	c.in.SetWorld(s.World.Name)
	text := "---- World " + s.World.Name + " ----"
	if s.state >= gtfparser.SockZombie {
		text = "---- World " + s.World.Name + " (dead) ----"
	}
	c.in.FireHookEvent(gtfparser.HookEvent{Name: "WORLD", Args: []string{s.World.Name}, World: s.World.Name, Message: text, HasMsg: !virt, NoMessage: virt})
	if quiet {
		s.scr.Flush(c.ui.Winlines())
	}
	c.panels.worldChanged(c)
	c.reap()
}

// textDivider marks where the lines that arrived in the background begin
// (the %textdiv and %textdiv_str settings).
func (c *Client) textDivider(scr *ui.Screen) {
	if c.in.Var("textdiv") == "off" || c.in.Var("textdiv") == "clear" {
		return
	}
	str := c.in.Var("textdiv_str")
	if str == "" {
		return
	}
	scr.InsertDivider(scr.Unseen, ui.PlainCells(str, layout.Attr{}))
}
