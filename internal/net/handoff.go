// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package net

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"github.com/itsnotyoutoday/gtf/internal/net/telnet"
	"github.com/itsnotyoutoday/gtf/internal/net/tlsmigrate"
)

// Handing a live connection to another process (/restart, docs/RESTART.md).
// The sequence in the old process is Pause (the reader stops), Export (state
// plus a duplicate of the socket, nothing is destroyed), and then either the
// process execs (the duplicate descriptor survives, everything else goes) or
// the restart is abandoned and Resume carries on as if nothing had happened.
// The new process builds the connection with Adopt and starts it with Resume.
// The game never sees the connection end: the kernel keeps the socket and
// whatever arrives in the meantime waits in its buffer.

// HandoffStateVersion is the version of HandoffState written by this build.
// Fields may be added without changing it (an older reader ignores them, so
// they must be optional); anything else bumps it and CheckHandoff refuses a
// state it does not know.
const HandoffStateVersion = 1

// ErrNotHandoffable is returned for a connection that cannot be handed over:
// not plain TCP, not a migratable TLS session, or a raw stream.
var ErrNotHandoffable = errors.New("net: this connection cannot be handed over")

// ErrStateTooNew is returned for a HandoffState written by a newer build.
var ErrStateTooNew = errors.New("net: handoff state written by a newer build")

// EnvState is the part of the terminal environment that belongs to the
// connection (what was negotiated or reported to the server).
type EnvState struct {
	UTF8    bool   `json:"utf8"`
	Charset string `json:"charset,omitempty"`
	Cols    int    `json:"cols"`
	Rows    int    `json:"rows"`
	TLS     bool   `json:"tls,omitempty"`
}

// HandoffState is everything about a live connection except the socket (and
// the TLS key material, which travels separately as an opaque blob).
type HandoffState struct {
	V      int                 `json:"v"`
	Telnet telnet.MachineState `json:"telnet"`
	Carry  []byte              `json:"carry,omitempty"` // incomplete multibyte sequence
	Env    EnvState            `json:"env"`
}

func (c *Conn) tcpConn() *net.TCPConn {
	switch x := c.rwc.(type) {
	case *net.TCPConn:
		return x
	case *tlsmigrate.Conn:
		tc, _ := x.NetConn().(*net.TCPConn)
		return tc
	}
	return nil
}

func migratableSuite(version, suite uint16) bool {
	switch version {
	case tls.VersionTLS13:
		return true // every TLS 1.3 suite is AEAD
	case tls.VersionTLS12:
		n := tls.CipherSuiteName(suite)
		return strings.Contains(n, "_GCM_") || strings.Contains(n, "CHACHA20_POLY1305")
	}
	return false
}

// HandoffKind says how a connection can be carried: "tcp", "tls", or "" when
// it cannot (the reason is in the second result).
func (c *Conn) HandoffKind() (kind, why string) {
	switch {
	case c.raw:
		return "", "raw stream"
	case c.closing.Load():
		return "", "closed"
	}
	switch x := c.rwc.(type) {
	case *net.TCPConn:
		return "tcp", ""
	case *tlsmigrate.Conn:
		if x.NetConn() == nil || c.tcpConn() == nil {
			return "", "TLS over a non-TCP transport"
		}
		cs := x.ConnectionState()
		if !cs.HandshakeComplete {
			return "", "TLS handshake not finished"
		}
		if !migratableSuite(cs.Version, cs.CipherSuite) {
			return "", "TLS " + tls.VersionName(cs.Version) + " with " + tls.CipherSuiteName(cs.CipherSuite) + " cannot be exported"
		}
		return "tls", ""
	}
	return "", "TLS connection made without the migratable TLS client"
}

// Export captures the state of a paused connection and a duplicate of its
// socket (the caller owns the file). tlsState is non-nil for TLS: it is key
// material for a live session, so it must be kept in a 0600 file for as short
// a time as possible and wiped afterwards. Export changes nothing: Resume
// continues the connection exactly where it stopped.
func (c *Conn) Export() (st HandoffState, tlsState []byte, f *os.File, err error) {
	kind, why := c.HandoffKind()
	if kind == "" {
		return st, nil, nil, fmt.Errorf("%w: %s", ErrNotHandoffable, why)
	}
	c.runMu.Lock()
	running := c.running
	c.runMu.Unlock()
	if running {
		return st, nil, nil, errors.New("net: Export needs a paused connection")
	}
	tc := c.tcpConn()
	if kind == "tls" {
		mc := c.rwc.(*tlsmigrate.Conn)
		var ms *tlsmigrate.State
		// "not at a record boundary right now" is transient: retry for a moment
		for i := 0; ; i++ {
			ms, err = mc.ExportState()
			if err == nil || tlsmigrate.IsUnsupported(err) || i >= 150 {
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
		if err != nil {
			return st, nil, nil, err
		}
		tlsState, err = ms.MarshalBinary()
		ms.Zero()
		if err != nil {
			return st, nil, nil, err
		}
	}
	f, err = tc.File()
	if err != nil {
		clear(tlsState)
		return st, nil, nil, err
	}
	e := c.env.Snapshot()
	st = HandoffState{
		V: HandoffStateVersion, Telnet: c.m.Snapshot(), Carry: append([]byte(nil), c.carry...),
		Env: EnvState{UTF8: e.UTF8, Charset: e.Charset, Cols: e.Cols, Rows: e.Rows, TLS: e.TLS},
	}
	return st, tlsState, f, nil
}

// CheckHandoff verifies that Adopt would accept st (and the TLS blob) without
// touching any socket, so that a new process can refuse a whole handoff before
// it has adopted anything.
func CheckHandoff(st HandoffState, tlsState []byte) error {
	if st.V > HandoffStateVersion {
		return fmt.Errorf("%w (state version %d, this build understands %d)", ErrStateTooNew, st.V, HandoffStateVersion)
	}
	if st.V < 1 || len(st.Carry) > 4 {
		return errors.New("net: invalid connection state")
	}
	if err := telnet.NewMachine(nil, telnet.NewEnv(), nil).CheckState(st.Telnet); err != nil {
		return err
	}
	if st.Env.TLS != (tlsState != nil) {
		return errors.New("net: TLS state does not match the connection")
	}
	if tlsState != nil {
		ms, err := tlsmigrate.Unmarshal(tlsState)
		if err != nil {
			return err
		}
		ms.Zero()
	}
	return nil
}

// Adopt builds a connection that continues from an exported one. f is the
// duplicated socket (the caller may close it afterwards). Nothing is read from
// the socket and nothing is sent until Resume; Discard drops an adopted
// connection without ending anything. tlsCfg is the original TLS client
// configuration (only policy matters, it may be nil).
func Adopt(st HandoffState, tlsState []byte, f *os.File, o Options, tlsCfg *tls.Config) (*Conn, error) {
	if err := CheckHandoff(st, tlsState); err != nil {
		return nil, err
	}
	nc, err := net.FileConn(f) // its own duplicate: the caller may close f
	if err != nil {
		return nil, err
	}
	tcp, ok := nc.(*net.TCPConn)
	if !ok {
		nc.Close()
		return nil, ErrNotHandoffable
	}
	_ = tcp.SetKeepAlive(true)
	_ = tcp.SetKeepAlivePeriod(30 * time.Second)
	var rwc interface {
		Read([]byte) (int, error)
		Write([]byte) (int, error)
		Close() error
	} = tcp
	if tlsState != nil {
		ms, err := tlsmigrate.Unmarshal(tlsState)
		if err != nil {
			nc.Close()
			return nil, err
		}
		defer ms.Zero()
		mc, err := tlsmigrate.Import(tcp, tlsCfg, ms)
		if err != nil {
			nc.Close()
			return nil, err
		}
		rwc = mc
	}
	env := o.Env
	if env == nil {
		env = telnet.NewEnv()
	}
	env.Do(func(e *telnet.EnvInfo) {
		e.UTF8, e.Charset, e.Cols, e.Rows, e.TLS = st.Env.UTF8, st.Env.Charset, st.Env.Cols, st.Env.Rows, st.Env.TLS
	})
	o.Env = env
	c := newConn(rwc, o)
	if err := c.m.Restore(st.Telnet); err != nil {
		nc.Close()
		return nil, err
	}
	c.carry = append([]byte(nil), st.Carry...)
	c.started = true // the offers were made by the old process
	return c, nil
}

// Discard drops an adopted connection that was never resumed WITHOUT ending
// anything: only this process's descriptor is closed (a migratable TLS
// session is dropped without close_notify) and the events channel closes
// without a Closed event. Used when a probe or a refused handoff must not
// disturb the connection that stays with the old process.
func (c *Conn) Discard() {
	c.closeOnce.Do(func() {
		c.closing.Store(true)
		if mc, ok := c.rwc.(*tlsmigrate.Conn); ok {
			_ = mc.Discard()
		} else {
			_ = c.rwc.Close()
		}
		close(c.events)
	})
}
