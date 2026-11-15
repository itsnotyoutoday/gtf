// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package client

import (
	"encoding/json"
	"sync/atomic"
	"time"

	"github.com/itsnotyoutoday/gtf/internal/squatter"
)

// The listening socket and the connected clients of the squatter ride along with /restart (phase two
// of the squatter's carrier, restart_squatter.go): the squatter stops accepting and brings every
// client to a standstill (QuiesceConns), the sockets go in the handoff next to the game connections,
// and the new process listens on the same socket and adopts the clients without a new handshake.
// Whatever cannot be carried is dropped by the exec and reconnects, resuming from the state the
// carrier saved; a handoff that fails as a whole falls back to that for every client.

// squatCarryConns says whether /restart tries to carry the listener and the connected clients
// (default). Tests of the state-only behaviour (which is also what a failed handoff falls back to)
// switch it off.
var squatCarryConns atomic.Bool

func init() { squatCarryConns.Store(true) }

// squatCarriesFiles is set while a save has handed descriptors over (then the section is required: an
// older binary that did not know it would leave the inherited sockets open and unserved).
var squatCarriesFiles atomic.Bool

// squatQuiesceTime bounds how long the clients get to stand still.
const squatQuiesceTime = 3 * time.Second

func (c *Client) squatPlanConns() []RestartItem {
	if !squatCarryConns.Load() {
		return nil
	}
	listening, peers := c.sq.sq.PlanConns()
	if !listening {
		return nil
	}
	items := []RestartItem{{Name: "squatter listener", Kind: "listening socket"}}
	for _, p := range peers {
		it := RestartItem{Name: "squatter client " + p.Name, Kind: p.Kind}
		if !p.Carry {
			it.Kind = "reconnects: " + p.Why // it is not carried and does not block the restart: it reconnects and resumes
		}
		items = append(items, it)
	}
	return items
}

func (c *Client) squatQuiesce() error {
	if !squatCarryConns.Load() {
		return nil
	}
	// a failure here (not listening, the socket cannot be duplicated) is not a reason to refuse the
	// restart: nothing has been stopped, and the clients simply reconnect
	if err := c.sq.sq.QuiesceConns(squatQuiesceTime, c.sq.q.drain); err == nil {
		c.sq.quiesced = true
	}
	return nil
}

// squatSave returns the squatter's state and, when the connections are carried, theirs (their
// descriptors are added to w).
func (c *Client) squatSave(w *RestartWriter) (state, conns json.RawMessage, err error) {
	squatCarriesFiles.Store(false)
	sq := c.sq.sq
	state, err = sq.Export()
	if err != nil {
		return nil, nil, err
	}
	if c.sq.quiesced {
		ex, xerr := sq.ExportConns()
		if xerr != nil {
			// carry the state only: the clients stand again and are dropped by the exec like in every restart
			sq.ResumeConns()
			c.sq.quiesced = false
		} else {
			cs := ex.State
			cs.Listener = w.AddFile("listener", ex.Listener)
			for i := range cs.Peers {
				cs.Peers[i].Slot = w.AddFile("client-"+cs.Peers[i].Name, ex.PeerFiles[i])
			}
			conns, err = json.Marshal(cs)
			for i := range cs.Peers {
				clear(cs.Peers[i].TLS) // the saved copy has the key material; the old process needs it no more
			}
			if err != nil {
				return nil, nil, err
			}
			squatCarriesFiles.Store(true)
		}
	}
	if conns == nil {
		sq.ReloadNotice() // the clients reconnect at once and resume
	}
	return state, conns, nil
}

func (c *Client) squatRollback() {
	if c.sq.quiesced {
		c.sq.quiesced = false
		c.sq.sq.ResumeConns()
	}
}

// squatRestoreConns adopts (or, in the rehearsal, only checks) the listener and the clients. When they
// cannot be used (a newer format, a descriptor that is not what it should be) it does not refuse the
// restart: it lets the descriptors go, and the squatter comes up from its saved state alone, its
// clients reconnecting and resuming.
func (c *Client) squatRestoreConns(ctx *RestoreContext, raw json.RawMessage, probe bool) error {
	if len(raw) == 0 {
		return nil
	}
	fall := func(err error) error {
		ctx.CloseOwned(secSquatter)
		if !probe {
			c.privateLine("% squatter: the clients could not be carried over (" + err.Error() + "); they reconnect")
		}
		return nil
	}
	cs, err := squatter.DecodeConns(raw)
	if err != nil {
		return fall(err)
	}
	pr, err := c.sq.sq.PrepareConns(cs, ctx.File)
	if err != nil {
		return fall(err)
	}
	if probe {
		pr.Release() // the old process still owns every socket: let go without a byte
		return nil
	}
	c.sq.sq.AdoptConns(pr)
	return nil
}
