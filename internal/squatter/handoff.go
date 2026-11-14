// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package squatter

// Carrying the squatter across a restart of gtf that keeps the game connections (/restart).
//
// The restart code is written elsewhere; this file only gives it the pieces, with no
// dependency on it. The intended use (docs/SQUATTER.md, "Surviving /restart"):
//
//	old process:  blob, _ := sq.Export()            // sessions, scrollback, acks, GMCP/MSDP state
//	              f, _ := sq.ListenerFile()         // the listening socket as an *os.File to inherit
//	new process:  sq.AdoptListener(ln)              // ln = net.FileListener(inherited file)
//	              ids, _ := sq.Import(blob)         // before the first Apply
//	              ...rebind each id to the restored connection, then Apply(cfg, nil)
//
// Peers are not carried: a SquatClient reconnects by itself and replays from its last seq
// (the old process may send {"t":"server.restarting"} first, see ReloadNotice).

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"

	"github.com/itsnotyoutoday/gtf/internal/squatter/scrollback"
	"github.com/itsnotyoutoday/gtf/internal/squatter/wire"
)

// stateVersion is the version of the Export format.
const stateVersion = 1

// SessionState is one session in an exported state.
type SessionState struct {
	ID         string                     `json:"id"`
	Meta       SessionMeta                `json:"meta"`
	EndedAt    int64                      `json:"endedAt,omitempty"`
	EndReason  string                     `json:"endReason,omitempty"`
	EndDetail  string                     `json:"endDetail,omitempty"`
	LastText   string                     `json:"lastText,omitempty"`
	RemoteEcho bool                       `json:"remoteEcho,omitempty"`
	Acks       map[string]uint64          `json:"acks,omitempty"`
	Gmcp       map[string]json.RawMessage `json:"gmcp,omitempty"`
	Msdp       map[string]any             `json:"msdp,omitempty"`
	Entries    []scrollback.Entry         `json:"entries,omitempty"`
}

type exported struct {
	Version  int            `json:"version"`
	Serial   uint64         `json:"serial"`
	Sessions []SessionState `json:"sessions"`
}

// Export serialises the sessions. It does not stop anything.
func (s *Squatter) Export() ([]byte, error) {
	s.mu.Lock()
	serial := s.serial
	s.mu.Unlock()
	ex := exported{Version: stateVersion, Serial: serial}
	for _, ss := range s.sessionList() {
		ss.mu.Lock()
		first, last := ss.store.Bounds()
		var es []scrollback.Entry
		var err error
		if last > 0 {
			es, err = ss.store.Range(first, last)
		}
		st := SessionState{ID: ss.id, Meta: SessionMeta{World: ss.world, Host: ss.host, Port: ss.port, TLS: ss.tls, StartedAt: ss.startedAt, State: ss.state},
			EndedAt: ss.endedAt, EndReason: ss.endReason, EndDetail: ss.endDetail, LastText: ss.lastText, RemoteEcho: ss.remoteEcho,
			Acks: copyAcks(ss.acks), Gmcp: map[string]json.RawMessage{}, Msdp: map[string]any{}, Entries: es}
		for k, v := range ss.gmcpLatest {
			st.Gmcp[k] = v
		}
		for k, v := range ss.msdpLatest {
			st.Msdp[k] = v
		}
		ss.mu.Unlock()
		if err != nil {
			return nil, fmt.Errorf("session %s: %w", ss.id, err)
		}
		ex.Sessions = append(ex.Sessions, st)
	}
	return json.Marshal(ex)
}

func copyAcks(m map[string]uint64) map[string]uint64 {
	out := make(map[string]uint64, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// Import restores the sessions of an Export into a squatter that has none yet (it must be
// active: call it after the first Apply, or before it with Activate). It returns the session ids.
func (s *Squatter) Import(blob []byte) ([]string, error) {
	var ex exported
	if err := json.Unmarshal(blob, &ex); err != nil {
		return nil, err
	}
	if ex.Version != stateVersion {
		return nil, fmt.Errorf("squatter state version %d, want %d", ex.Version, stateVersion)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.sessions) != 0 {
		return nil, errors.New("squatter already has sessions")
	}
	if ex.Serial > s.serial {
		s.serial = ex.Serial
	}
	var ids []string
	for _, st := range ex.Sessions {
		ss, err := s.newSession(st.ID, st.Meta, s.cfg.Scrollback)
		if err != nil {
			return ids, err
		}
		ss.endedAt, ss.endReason, ss.endDetail, ss.lastText, ss.remoteEcho = st.EndedAt, st.EndReason, st.EndDetail, st.LastText, st.RemoteEcho
		for k, v := range st.Acks {
			ss.acks[k] = v
		}
		for k, v := range st.Gmcp {
			ss.gmcpLatest[k] = v
		}
		for k, v := range st.Msdp {
			ss.msdpLatest[k] = v
		}
		if err := ss.store.Seed(st.Entries); err != nil {
			return ids, err
		}
		s.sessions[st.ID] = ss
		s.order = append(s.order, st.ID)
		ids = append(ids, st.ID)
	}
	return ids, nil
}

// Activate turns the taps on without a listener, for Import before the first Apply.
func (s *Squatter) Activate() { s.active.Store(true) }

// ListenerFile returns a duplicate of the listening socket for the next process to inherit.
func (s *Squatter) ListenerFile() (*os.File, error) {
	s.mu.Lock()
	r := s.run
	s.mu.Unlock()
	if r == nil {
		return nil, errors.New("not listening")
	}
	tl, ok := r.ln.(*net.TCPListener)
	if !ok {
		return nil, errors.New("listener is not TCP")
	}
	return tl.File()
}

// AdoptListener makes the next start use an inherited listening socket instead of binding one.
func (s *Squatter) AdoptListener(l net.Listener) {
	s.mu.Lock()
	s.inherit = l
	s.mu.Unlock()
}

// ReloadNotice tells every client the process is about to restart (they reconnect and replay).
func (s *Squatter) ReloadNotice() {
	for _, p := range s.peerList() {
		p.Send(wire.Simple("server.restarting"))
	}
}
