// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package client

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/itsnotyoutoday/gtf/internal/squatter"
	"github.com/itsnotyoutoday/gtf/internal/squatter/proto"
)

// The squatter's carrier for /restart (docs/RESTART.md, "The squatter"). The section "squatter" holds
// the squatter's sessions with their scrollback and sequence numbers (squatter.Export) and which of
// gtf's connections each session mirrors; the paired devices and the TLS certificate are on disk
// (squatter_dir) and are simply read again.

const secSquatter = "squatter"

func init() { RegisterRestartSection(secSquatter, &squatterCarrier{}) }

// squatBind says which squatter session mirrors which world's connection.
type squatBind struct {
	World string `json:"world"`
	ID    string `json:"id"`
}

type squatterSection struct {
	Active bool            `json:"active"`          // the squatter was running (switched on)
	State  json.RawMessage `json:"state,omitempty"` // squatter.Export
	Bind   []squatBind     `json:"bind,omitempty"`
	Conns  json.RawMessage `json:"conns,omitempty"` // the listener and the clients with their descriptors (restart_squatter_conns.go)
}

type squatterCarrier struct{}

func (*squatterCarrier) Version() int { return 1 }

// Required: an older binary that does not know the section would drop the sessions silently; with
// connections or a listener handed over (see below) it must refuse instead.
func (*squatterCarrier) Required() bool { return squatCarriesFiles.Load() }

// squatterOn reports whether the squatter is running in c.
func (c *Client) squatterOn() bool { return c.sq != nil && c.sq.active() }

// squatCarried says whether the connection of s goes through /restart (so that its squatter session
// can keep its id): a dead socket keeps its screen and its ended session, an open one is carried when
// its transport can be.
func (s *Session) squatCarried() bool {
	if s.dead() {
		return true
	}
	if s.conn == nil || !s.open() {
		return false
	}
	kind, _ := s.conn.HandoffKind()
	return kind != ""
}

func (*squatterCarrier) RestartPlan(c *Client) (carried, ends []RestartItem) {
	if !c.squatterOn() {
		return nil, nil
	}
	sq := c.sq.sq
	carried = append(carried, RestartItem{Name: "squatter", Kind: fmt.Sprintf("%d sessions with their scrollback", sq.NumSessions())})
	carried = append(carried, c.squatPlanConns()...)
	return carried, nil
}

func (*squatterCarrier) RestartQuiesce(c *Client) error {
	if !c.squatterOn() {
		return nil
	}
	return c.squatQuiesce()
}

func (*squatterCarrier) RestartSave(c *Client, w *RestartWriter) (any, error) {
	if !c.squatterOn() {
		squatCarriesFiles.Store(false)
		return squatterSection{}, nil
	}
	sec := squatterSection{Active: true}
	state, conns, err := c.squatSave(w)
	if err != nil {
		return nil, err
	}
	sec.State, sec.Conns = state, conns
	for _, s := range c.order {
		if s.squatID != "" && s.squatCarried() {
			sec.Bind = append(sec.Bind, squatBind{World: s.World.Name, ID: s.squatID})
		}
	}
	return sec, nil
}

func (*squatterCarrier) RestartRollback(c *Client) {
	if c.squatterOn() {
		c.squatRollback()
	}
}

func (*squatterCarrier) RestartRestore(c *Client, ctx *RestoreContext, v int, data json.RawMessage) error {
	var sec squatterSection
	if err := json.Unmarshal(data, &sec); err != nil {
		return fmt.Errorf("squatter section: %w", err)
	}
	if !sec.Active {
		return nil
	}
	st, err := squatter.DecodeState(sec.State)
	if err != nil {
		return err
	}
	ids := map[string]bool{}
	for _, id := range st.SessionIDs() {
		ids[id] = true
	}
	for _, b := range sec.Bind {
		if !ids[b.ID] {
			return errors.New("the squatter state does not have the session " + b.ID)
		}
	}
	if ctx.Probe {
		return c.squatRestoreConns(ctx, sec.Conns, true) // rehearsal: validate, import, then let go; change nothing
	}
	cfg, _ := squatter.ConfigFromVars(c.in.Var, c.cfg.HomeDir)
	sq := c.sq.sq
	sq.Activate()
	if err := sq.ImportState(st, cfg.Scrollback); err != nil {
		return err
	}
	sq.SetLocalCols(c.ui.W)
	reconnecting := map[string]bool{}
	for _, rc := range c.reconnect {
		reconnecting[rc.world] = true
	}
	claimed := map[string]bool{}
	for _, b := range sec.Bind {
		if s := c.sessions[b.World]; s != nil && !reconnecting[b.World] {
			s.squatID = b.ID
			claimed[b.ID] = true
		}
	}
	for _, id := range st.SessionIDs() {
		if !claimed[id] {
			// its connection did not come through (or was never carried): the session stays, ended,
			// and the world's new connection gets a session of its own
			sq.EndSession(id, proto.EndSquatterRestarted, "gtf was restarted and could not keep this connection")
		}
	}
	return c.squatRestoreConns(ctx, sec.Conns, false)
}
