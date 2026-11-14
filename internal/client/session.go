// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package client

import (
	"context"
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

// Dialer opens a connection to a world.
type Dialer interface {
	Dial(ctx context.Context, w World) (*gnet.Conn, error)
}

// NetDialer dials over TCP/TLS.
type NetDialer struct{ Env func() *telnet.Env }

// Dial implements Dialer.
func (d NetDialer) Dial(ctx context.Context, w World) (*gnet.Conn, error) {
	var t gnet.Transport = gnet.TCP{Timeout: 15 * time.Second}
	if w.SSL {
		t = gnet.TLS{Timeout: 15 * time.Second}
	}
	env := telnet.NewEnv()
	if d.Env != nil {
		env = d.Env()
	}
	return gnet.Dial(ctx, t, w.Host, w.Port, gnet.Options{Env: env})
}

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

	localClose     bool // closed by /dc (no DISCONNECT message)
	pendTimer      *time.Timer
	connectedAt    time.Time
	lastRecv       time.Time
	lastSend       time.Time
	activeNotified bool
	logfile        io.Closer
}

func (s *Session) open() bool { return s.state == gtfparser.SockConnected }
func (s *Session) dead() bool { return s.state >= gtfparser.SockZombie }

func (c *Client) newSession(w World, o gtfparser.ConnectOptions) *Session {
	s := &Session{c: c, World: w, store: data.New(), scr: ui.NewScreen(w.Name, 20000), opts: o, state: gtfparser.SockConnecting}
	s.scr.More = c.in.TermSettings().More
	s.scr.OnPause = func() { c.in.FireHook("MORE", "") }
	name := w.Name
	s.scr.SB.Keep = func(cells []layout.Cell) bool { return !c.in.LimitedFor(name, ui.StripCells(cells), "") }
	return s
}

// connect starts a connection to w; events arrive via the runner.
func (c *Client) connect(w World, o gtfparser.ConnectOptions) error {
	if old := c.sessions[w.Name]; old != nil && !old.dead() {
		c.foreground(old)
		return nil
	}
	s := c.newSession(w, o)
	c.sessions[w.Name] = s
	c.order = append(c.order, s)
	c.withWorld(s, func() {
		c.in.FireHookEvent(gtfparser.HookEvent{Name: "PENDING", Args: []string{w.Name, w.Host + " " + w.Port}, NoMessage: o.Quiet, World: w.Name})
	})
	if !o.Background {
		c.foreground(s)
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		conn, err := c.cfg.Dialer.Dial(ctx, w)
		c.run.Post(func(*gtfparser.Interp) {
			if err != nil {
				c.withWorld(s, func() { c.in.FireHookArgs("CONFAIL", w.Name, err.Error()) })
				c.removeSession(s)
				c.afterTask()
				return
			}
			s.conn = conn
			s.state = gtfparser.SockConnected
			s.connectedAt = c.cfg.Now()
			conn.Resize(c.ui.W, c.ui.H)
			c.setEnv(conn)
			c.withWorld(s, func() {
				c.in.FireHookEvent(gtfparser.HookEvent{Name: "CONNECT", Args: []string{w.Name}, NoMessage: o.Quiet, World: w.Name})
				if !c.cfg.NoLogin && !o.NoLogin && w.Character != "" && w.Password != "" && loginOn(c.in.Var("login")) {
					c.in.FireHookArgs("LOGIN", w.Name, w.Character, w.Password)
				}
			})
			c.afterTask()
			go c.pump(s, conn)
		})
	}()
	return nil
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
		c.run.Post(func(*gtfparser.Interp) { c.onNet(s, ev); c.afterTask() })
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
		s.state = gtfparser.SockDead
		s.flushPartial()
		if !s.localClose {
			c.withWorld(s, func() { c.in.FireHookArgs("DISCONNECT", s.World.Name) })
		}
		c.removeSession(s)
	}
}

func (s *Session) close() {
	s.localClose = true
	if s.conn != nil {
		s.conn.Close()
	}
	if s.logfile != nil {
		s.logfile.Close()
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
			c.run.Post(func(*gtfparser.Interp) {
				if s.pend != "" {
					s.promptEnd()
					c.afterTask()
				}
			})
		})
	}
}

func (s *Session) setPrompt(text string) {
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

func (c *Client) removeSession(s *Session) {
	s.state = gtfparser.SockDead
	s.close()
	if c.sessions[s.World.Name] == s {
		delete(c.sessions, s.World.Name)
	}
	for i, o := range c.order {
		if o == s {
			c.order = append(c.order[:i], c.order[i+1:]...)
			break
		}
	}
	c.in.WorldClosed(s.World.Name)
	if c.fg == s {
		var next *Session
		if len(c.order) > 0 {
			next = c.order[len(c.order)-1]
		}
		c.foreground(next)
	}
}

// foreground makes s (or the base screen when nil) the displayed world.
func (c *Client) foreground(s *Session) {
	if c.fg == s {
		return
	}
	c.fg = s
	if s == nil {
		c.ui.SetScreen(c.base)
		c.in.SetWorld("")
		c.in.FireHookEvent(gtfparser.HookEvent{Name: "WORLD", Message: "---- No world ----", HasMsg: true})
		return
	}
	s.scr.Unseen = 0
	s.activeNotified = false
	c.ui.SetScreen(s.scr)
	c.in.SetWorld(s.World.Name)
	c.in.FireHookEvent(gtfparser.HookEvent{Name: "WORLD", Args: []string{s.World.Name}, World: s.World.Name, Message: "---- World " + s.World.Name + " ----", HasMsg: true})
	c.panels.worldChanged(c)
}
