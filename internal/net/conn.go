// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package net

import (
	"context"
	"errors"
	"io"
	"sync"

	"github.com/itsnotyoutoday/gtf/internal/net/telnet"

	// Every protocol extension registers itself; adding one is one new
	// package under internal/net/proto plus one line here.
	_ "github.com/itsnotyoutoday/gtf/internal/net/proto/charset"
	_ "github.com/itsnotyoutoday/gtf/internal/net/proto/eor"
	_ "github.com/itsnotyoutoday/gtf/internal/net/proto/gmcp"
	_ "github.com/itsnotyoutoday/gtf/internal/net/proto/mccp2"
	_ "github.com/itsnotyoutoday/gtf/internal/net/proto/msdp"
	_ "github.com/itsnotyoutoday/gtf/internal/net/proto/mssp"
	_ "github.com/itsnotyoutoday/gtf/internal/net/proto/naws"
	_ "github.com/itsnotyoutoday/gtf/internal/net/proto/sga"
	_ "github.com/itsnotyoutoday/gtf/internal/net/proto/ttype"
)

// Event types a Conn yields (aliases of the telnet layer's).
type (
	Event        = telnet.Event
	Text         = telnet.Text
	Prompt       = telnet.Prompt
	Subneg       = telnet.Subneg
	OptionChange = telnet.OptionChange
	Message      = telnet.Message
)

// Closed is the last event of a Conn.
type Closed struct{ Err error }

// Options configure Dial / NewConn.
type Options struct {
	Env      *telnet.Env
	Handlers []telnet.Handler // nil = all registered
	// Raw disables the telnet layer (plain byte stream, no negotiation).
	Raw bool
}

// Conn is one server connection: transport + telnet machine + charset.
type Conn struct {
	rwc    io.ReadWriteCloser
	m      *telnet.Machine
	env    *telnet.Env
	events chan Event
	raw    bool

	closeOnce sync.Once
	wmu       sync.Mutex
}

// NewConn wraps an established stream and starts reading. Events arrive on
// Events(); the final one is Closed.
func NewConn(rwc io.ReadWriteCloser, o Options) *Conn {
	env := o.Env
	if env == nil {
		env = telnet.NewEnv()
	}
	c := &Conn{rwc: rwc, env: env, events: make(chan Event, 256), raw: o.Raw}
	c.m = telnet.NewMachine(rwc, env, o.Handlers)
	go c.run()
	return c
}

// Dial opens a transport and returns a Conn.
func Dial(ctx context.Context, t Transport, host, port string, o Options) (*Conn, error) {
	nc, err := t.Dial(ctx, host, port)
	if err != nil {
		return nil, err
	}
	if t.Name() == "tls" {
		if o.Env == nil {
			o.Env = telnet.NewEnv()
		}
		o.Env.Do(func(e *telnet.EnvInfo) { e.TLS = true })
	}
	return NewConn(nc, o), nil
}

// Events is the stream of typed events.
func (c *Conn) Events() <-chan Event { return c.events }

// Env returns the shared terminal environment.
func (c *Conn) Env() *telnet.Env { return c.env }

func (c *Conn) run() {
	var carry []byte
	emit := func(e telnet.Event) {
		if t, ok := e.(telnet.Text); ok {
			u := c.env.Snapshot().UTF8
			var out []byte
			out, carry = decode(u, carry, t.Data)
			if len(out) == 0 {
				return
			}
			e = telnet.Text{Data: out}
		}
		c.events <- e
	}
	var err error
	if c.raw {
		buf := make([]byte, 4096)
		for {
			n, rerr := c.rwc.Read(buf)
			if n > 0 {
				emit(telnet.Text{Data: append([]byte(nil), buf[:n]...)})
			}
			if rerr != nil {
				err = rerr
				break
			}
		}
	} else {
		err = c.m.Run(c.rwc, emit)
	}
	if errors.Is(err, io.EOF) {
		err = nil
	}
	c.events <- Closed{Err: err}
	close(c.events)
	c.rwc.Close()
}

// SendLine sends text followed by CR LF (charset-encoded, IAC-escaped).
func (c *Conn) SendLine(s string) error {
	return c.Write(s + "\r\n")
}

// Write sends text unmodified except for charset encoding and IAC escaping.
func (c *Conn) Write(s string) error {
	b := encode(c.env.Snapshot().UTF8, s)
	if c.raw {
		_, err := c.rwc.Write(b)
		return err
	}
	return c.m.WriteData(b)
}

// Resize records a new window size and informs the server if NAWS is on.
func (c *Conn) Resize(cols, rows int) {
	c.env.Do(func(e *telnet.EnvInfo) { e.Cols, e.Rows = cols, rows })
	if c.raw {
		return
	}
	for _, name := range []string{"naws"} {
		if h := c.m.HandlerByName(name); h != nil {
			if r, ok := h.(interface {
				Resize(*telnet.Session, int, int)
			}); ok {
				r.Resize(c.m.Session(), cols, rows)
			}
		}
	}
}

// SendProto sends structured data through the named protocol handler
// ("gmcp", "msdp").
func (c *Conn) SendProto(proto, name string, value any) error {
	h := c.m.HandlerByName(proto)
	s, ok := h.(telnet.Sender)
	if !ok {
		return errors.New(proto + ": protocol not available")
	}
	return s.Send(c.m.Session(), name, value)
}

// Enabled reports whether an option is on for a side.
func (c *Conn) Enabled(opt byte, side telnet.Side) bool { return c.m.Session().Enabled(opt, side) }

// Close closes the connection.
func (c *Conn) Close() error {
	var err error
	c.closeOnce.Do(func() { err = c.rwc.Close() })
	return err
}
