// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package wire

// Carrying connected clients across a restart of the process (gtf's /restart). The sequence, driven
// by internal/squatter (handoff_conns.go):
//
//	old process   Quiesce   every client's reader and writer stand still at a clean point
//	              Export    each client's state and a handle on its transport (the caller exports TLS)
//	new process   Adopt     a client from its state over the inherited connection
//	rollback      Resume    the old process carries on with the clients it has
//
// A client is carried with its connection, so nothing is lost or repeated: the bytes it had sent and
// the server had read but not used (wsConn.buf), a message it had begun to send in fragments, a message
// that was read but not yet dispatched, and the frames queued for it and not yet written all travel in
// ClientState. Requests that a reader goroutine had in flight inside the backend are finished by the
// caller's pump during Quiesce, never abandoned.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/coder/websocket"

	"github.com/itsnotyoutoday/gtf/internal/squatter/proto"
)

// ClientStateVersion is the version of ClientState (adding optional members does not change it).
const ClientStateVersion = 1

// PendMsg is a message that had been read and not yet handled.
type PendMsg struct {
	Binary bool   `json:"binary,omitempty"`
	Data   []byte `json:"data"`
}

// ClientState is what a carried client needs in the new process besides its connection.
type ClientState struct {
	V       int                        `json:"v"`
	ID      uint64                     `json:"id"`
	Device  string                     `json:"device"`
	Name    string                     `json:"name"`
	Role    string                     `json:"role"`
	User    string                     `json:"user,omitempty"`
	Caps    proto.Caps                 `json:"caps"`
	Ext     map[string]json.RawMessage `json:"ext,omitempty"`
	Offer   []string                   `json:"offer,omitempty"`
	Joined  int64                      `json:"joined,omitempty"` // unix ms
	Buf     []byte                     `json:"buf,omitempty"`    // received on the wire, not yet consumed
	Frag    *wsFrag                    `json:"frag,omitempty"`   // a fragmented message in progress
	Pend    *PendMsg                   `json:"pend,omitempty"`
	Out     [][]byte                   `json:"out,omitempty"` // frames queued and not yet written
	Backend json.RawMessage            `json:"backend,omitempty"`
}

// Check validates a state before anything is adopted.
func (st *ClientState) Check() error {
	switch {
	case st.V < 1 || st.V > ClientStateVersion:
		return fmt.Errorf("client state version %d (this build reads up to %d)", st.V, ClientStateVersion)
	case st.ID == 0:
		return errors.New("client state without an id")
	case st.Device == "":
		return errors.New("client state without a device")
	case len(st.Buf) > 2*MaxFrameBytes+64:
		return errors.New("client state: too many buffered bytes")
	}
	n := 0
	for _, f := range st.Out {
		n += len(f)
	}
	if n > 64*MaxFrameBytes {
		return errors.New("client state: too much queued output")
	}
	return nil
}

// ExportedClient is one client taken out of a quiesced server.
type ExportedClient struct {
	State ClientState
	// Transport is what the transport function returned for the client's connection.
	Transport any
}

// ClientCarrier is optionally implemented by a Backend that carries its per-client state.
type ClientCarrier interface {
	// AdoptPeer registers a client that arrives with its connection already established (no hello, no
	// welcome, no PeerJoined). data is what the old process's backend recorded for it.
	AdoptPeer(p Peer, data json.RawMessage) error
}

func (c *client) isPausing() bool {
	c.gmu.Lock()
	defer c.gmu.Unlock()
	return c.pausing
}

func (c *client) beginPause() {
	c.gmu.Lock()
	if c.pausing {
		c.gmu.Unlock()
		return
	}
	c.resume = make(chan struct{})
	c.pausing = true
	c.gmu.Unlock()
	c.ws.pauseRead()
	select { // an idle writer notices
	case c.wake <- struct{}{}:
	default:
	}
}

func (c *client) endPause() {
	c.gmu.Lock()
	if !c.pausing {
		c.gmu.Unlock()
		return
	}
	c.pausing = false
	ch := c.resume
	c.gmu.Unlock()
	c.ws.resumeRead()
	close(ch)
}

func (c *client) parkReader() bool {
	c.gmu.Lock()
	c.rparked = true
	ch := c.resume
	c.gmu.Unlock()
	c.s.notify()
	select {
	case <-ch:
	case <-c.ctx.Done():
	}
	c.gmu.Lock()
	c.rparked = false
	c.gmu.Unlock()
	return c.ctx.Err() == nil
}

func (c *client) parkWriter() bool {
	c.gmu.Lock()
	c.wparked = true
	ch := c.resume
	c.gmu.Unlock()
	c.s.notify()
	select {
	case <-ch:
	case <-c.ctx.Done():
	}
	c.gmu.Lock()
	c.wparked = false
	c.gmu.Unlock()
	return c.ctx.Err() == nil
}

func (c *client) parked() bool {
	c.gmu.Lock()
	defer c.gmu.Unlock()
	return c.pausing && c.rparked && c.wparked
}

func (s *Server) notify() {
	select {
	case s.qn <- struct{}{}:
	default:
	}
}

func (s *Server) clientList() []*client {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*client, 0, len(s.clients))
	for _, c := range s.clients {
		out = append(out, c)
	}
	return out
}

// Conns returns the network connection of every authenticated client, by client id.
func (s *Server) Conns() map[uint64]net.Conn {
	out := map[uint64]net.Conn{}
	for _, c := range s.clientList() {
		out[c.id] = c.ws.nc
	}
	return out
}

// Quiesce stops accepting new clients and brings every client's reader and writer to a standstill:
// the writer after it has written what is queued, the reader after the request it is handling has
// been answered. A reader may be waiting for the backend to run something on a goroutine the caller
// occupies: pump is called repeatedly while waiting, and must run whatever the backend has queued
// there. Clients that do not stand still within timeout are dropped (they reconnect and resume by
// sequence number) and their ids are returned.
func (s *Server) Quiesce(timeout time.Duration, pump func()) (dropped []uint64) {
	s.mu.Lock()
	s.quiesced = true
	s.mu.Unlock()
	list := s.clientList()
	for _, c := range list {
		c.beginPause()
	}
	deadline := time.Now().Add(timeout)
	for {
		if pump != nil {
			pump()
		}
		all := true
		for _, c := range list {
			if !c.parked() {
				all = false
			}
		}
		if all {
			return nil
		}
		if time.Now().After(deadline) {
			break
		}
		select {
		case <-s.qn:
		case <-time.After(2 * time.Millisecond):
		}
	}
	for _, c := range list {
		if !c.parked() {
			dropped = append(dropped, c.id)
			c.Close("handover: the connection did not stand still")
		}
	}
	return dropped
}

// Export takes the state of every client that stands still. transport is called for each with its
// network connection and returns what the caller needs of it (it may fail: that client is then not
// exported, and stays here); it must not read or write the connection. After Export a client
// belongs to the handover: closing it says nothing on the wire, and Resume gives it back.
func (s *Server) Export(transport func(id uint64, nc net.Conn) (any, error)) []ExportedClient {
	var out []ExportedClient
	for _, c := range s.clientList() {
		if !c.parked() {
			continue
		}
		c.gmu.Lock()
		already := c.carried
		c.gmu.Unlock()
		if already {
			continue
		}
		tr, err := transport(c.id, c.ws.nc)
		if err != nil {
			s.log.Info("wire: client not carried over", "device", c.devID, "why", err)
			continue
		}
		buf, frag := c.ws.readState()
		c.mu.Lock()
		caps := c.caps
		c.mu.Unlock()
		st := ClientState{V: ClientStateVersion, ID: c.id, Device: c.devID, Name: c.devName, Role: c.role, User: c.user,
			Caps: caps, Ext: c.ext, Offer: c.offer, Joined: c.joined.UnixMilli(), Buf: buf}
		if frag.On {
			st.Frag = &frag
		}
		c.gmu.Lock()
		if c.pend != nil {
			st.Pend = &PendMsg{Binary: c.pend.typ == websocket.MessageBinary, Data: c.pend.data}
		}
		c.carried = true
		c.gmu.Unlock()
		for drained := false; !drained; {
			select {
			case f := <-c.out:
				st.Out = append(st.Out, f)
			default:
				drained = true
			}
		}
		c.gmu.Lock()
		c.exported = st.Out
		c.gmu.Unlock()
		out = append(out, ExportedClient{State: st, Transport: tr})
	}
	return out
}

// Resume undoes Quiesce (and Export): the clients carry on in this process exactly where they were.
func (s *Server) Resume() {
	s.mu.Lock()
	s.quiesced = false
	s.mu.Unlock()
	for _, c := range s.clientList() {
		c.gmu.Lock()
		c.carried = false
		back := c.exported
		c.exported = nil
		c.gmu.Unlock()
		if len(back) > 0 { // what was taken out of the queue goes in again, ahead of anything queued since
			var later [][]byte
			for drained := false; !drained; {
				select {
				case f := <-c.out:
					later = append(later, f)
				default:
					drained = true
				}
			}
			for _, f := range append(back, later...) {
				select {
				case c.out <- f:
				default:
				}
			}
		}
		c.endPause()
	}
}

// Abandon ends this server's use of its clients without a word on any connection: they belong to
// another process now. The caller closes (or discards) the transports.
func (s *Server) Abandon() {
	s.mu.Lock()
	s.closed = true
	list := make([]*client, 0, len(s.clients))
	for _, c := range s.clients {
		list = append(list, c)
	}
	s.mu.Unlock()
	for _, c := range list {
		c.gmu.Lock()
		c.carried = true
		c.gmu.Unlock()
		c.ws.abandon()
	}
	s.cancel()
	s.wg.Wait()
}

// Adopt makes a client of a carried connection. nc is the established connection (TLS included); the
// client carries on as if it had never been interrupted. The backend learns of it through
// ClientCarrier.AdoptPeer (not PeerJoined: nothing is announced).
func (s *Server) Adopt(st ClientState, nc net.Conn) (Peer, error) {
	if err := st.Check(); err != nil {
		return nil, err
	}
	if nc == nil {
		return nil, errors.New("no connection")
	}
	frag := wsFrag{}
	if st.Frag != nil {
		frag = *st.Frag
	}
	_ = nc.SetDeadline(time.Time{}) // (a connection object that was paused in this process keeps its deadline)
	ctx, stop := context.WithCancel(s.ctx)
	ws := newWSConn(nc, append([]byte(nil), st.Buf...), frag)
	c := &client{s: s, id: st.ID, ws: ws, ctx: ctx, stop: stop, out: make(chan []byte, s.cfg.QueueLimit),
		devID: st.Device, devName: st.Name, role: st.Role, user: st.User, caps: st.Caps, ext: st.Ext, offer: st.Offer,
		lastRecv: time.Now(), joined: time.UnixMilli(st.Joined), resume: make(chan struct{}), wake: make(chan struct{}, 1)}
	if st.Joined == 0 {
		c.joined = time.Now()
	}
	for _, f := range st.Out {
		select {
		case c.out <- f:
		default:
			stop()
			return nil, errors.New("client state: queued output does not fit")
		}
	}
	if st.Pend != nil {
		typ := websocket.MessageText
		if st.Pend.Binary {
			typ = websocket.MessageBinary
		}
		c.pend = &wsMsg{typ, st.Pend.Data}
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		stop()
		return nil, errors.New("server closed")
	}
	if _, dup := s.clients[c.id]; dup {
		s.mu.Unlock()
		stop()
		return nil, fmt.Errorf("client id %d is in use", c.id)
	}
	s.clients[c.id] = c
	s.wg.Add(1)
	s.mu.Unlock()
	for { // ids of clients that connect later must not collide with carried ones
		cur := s.nextID.Load()
		if cur >= c.id || s.nextID.CompareAndSwap(cur, c.id) {
			break
		}
	}
	if cc, ok := s.cfg.Backend.(ClientCarrier); ok {
		if err := cc.AdoptPeer(c, st.Backend); err != nil {
			s.mu.Lock()
			delete(s.clients, c.id)
			s.mu.Unlock()
			s.wg.Done()
			stop()
			return nil, err
		}
	}
	go func() {
		defer s.wg.Done()
		defer func() {
			s.cfg.Backend.PeerLeft(c)
			s.mu.Lock()
			delete(s.clients, c.id)
			s.mu.Unlock()
		}()
		defer ws.CloseNow()
		done := make(chan struct{})
		go func() { defer close(done); c.writeLoop() }()
		c.readLoop()
		c.Close("closed")
		<-done
	}()
	return c, nil
}
