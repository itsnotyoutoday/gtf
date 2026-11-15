// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package squatter

// Carrying the squatter across a restart of gtf that keeps the game connections (/restart).
//
// This file has the squatter's own state and no dependency on the restart code
// (internal/client/restart_squatter.go is the carrier; docs/RESTART.md, "The squatter"):
//
//	old process:  blob, _ := sq.Export()            // sessions, scrollback, acks, GMCP/MSDP state, NAWS
//	new process:  st, err := DecodeState(blob)      // validates everything, touches nothing
//	              sq.Activate(); sq.ImportState(st, ring)   // before the first Apply: no seeding then
//
// The listener and the connected clients are carried by handoff_conns.go; without them, clients
// reconnect and resume from the stored sequence numbers.

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"

	"github.com/itsnotyoutoday/gtf/internal/squatter/proto"
	"github.com/itsnotyoutoday/gtf/internal/squatter/scrollback"
	"github.com/itsnotyoutoday/gtf/internal/squatter/wire"
)

// stateVersion is the version of the Export format. Adding optional members never changes it; a
// reader refuses a newer one (ErrStateTooNew) and reads every older one.
const stateVersion = 1

// Errors from DecodeState (use errors.Is).
var (
	ErrStateTooNew  = errors.New("squatter state is from a newer gtf")
	ErrStateCorrupt = errors.New("squatter state is corrupt")
)

// maxStateSessions and maxStateEntries bound what a state file may claim.
const (
	maxStateSessions = 4096
	maxStateEntries  = 2000000
)

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
	// added without a version bump (optional):
	NAWS     map[string]int `json:"naws,omitempty"`     // the width last told to the game, per session
	Announce []string       `json:"announce,omitempty"` // device ids already announced as freshly paired
	Port     int            `json:"port,omitempty"`     // the port that was bound (a hint when squatter_port=0)
}

// State is a decoded, validated squatter state (DecodeState).
type State struct{ ex exported }

// SessionIDs lists the sessions in the state, in order.
func (st *State) SessionIDs() []string {
	ids := make([]string, len(st.ex.Sessions))
	for i, ss := range st.ex.Sessions {
		ids[i] = ss.ID
	}
	return ids
}

// Worlds maps each session id to the world name it was recorded with.
func (st *State) Worlds() map[string]string {
	m := make(map[string]string, len(st.ex.Sessions))
	for _, ss := range st.ex.Sessions {
		m[ss.ID] = ss.Meta.World
	}
	return m
}

// Export serialises the sessions. It does not stop anything.
func (s *Squatter) Export() ([]byte, error) {
	s.mu.Lock()
	serial := s.serial
	port := 0
	if s.run != nil {
		if _, p, err := net.SplitHostPort(s.run.addr); err == nil {
			port, _ = strconv.Atoi(p)
		}
	}
	var ann []string
	for id, on := range s.announce {
		if on {
			ann = append(ann, id)
		}
	}
	s.mu.Unlock()
	ex := exported{Version: stateVersion, Serial: serial, Port: port, Announce: ann}
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
	s.nmu.Lock()
	if len(s.nsent) > 0 {
		ex.NAWS = make(map[string]int, len(s.nsent))
		for k, v := range s.nsent {
			ex.NAWS[k] = v
		}
	}
	s.nmu.Unlock()
	return json.Marshal(ex)
}

func copyAcks(m map[string]uint64) map[string]uint64 {
	out := make(map[string]uint64, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// DecodeState parses and checks a state written by Export. Nothing is adopted: a refused state
// leaves everything as it was. A truncated or damaged state is ErrStateCorrupt, a newer format
// ErrStateTooNew.
func DecodeState(blob []byte) (*State, error) {
	var head struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(blob, &head); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrStateCorrupt, err)
	}
	if head.Version > stateVersion {
		return nil, fmt.Errorf("%w (format %d, this gtf reads up to %d)", ErrStateTooNew, head.Version, stateVersion)
	}
	if head.Version < 1 {
		return nil, fmt.Errorf("%w: no format version", ErrStateCorrupt)
	}
	var ex exported
	if err := json.Unmarshal(blob, &ex); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrStateCorrupt, err)
	}
	if len(ex.Sessions) > maxStateSessions {
		return nil, fmt.Errorf("%w: %d sessions", ErrStateCorrupt, len(ex.Sessions))
	}
	seen := map[string]bool{}
	total := 0
	for _, ss := range ex.Sessions {
		if ss.ID == "" || seen[ss.ID] {
			return nil, fmt.Errorf("%w: session id %q is empty or repeated", ErrStateCorrupt, ss.ID)
		}
		seen[ss.ID] = true
		switch ss.Meta.State {
		case "", proto.StateConnecting, proto.StateConnected, proto.StateEnded:
		default:
			return nil, fmt.Errorf("%w: session %s has state %q", ErrStateCorrupt, ss.ID, ss.Meta.State)
		}
		total += len(ss.Entries)
		if total > maxStateEntries {
			return nil, fmt.Errorf("%w: too many scrollback entries", ErrStateCorrupt)
		}
		var prev uint64
		for i, e := range ss.Entries {
			seq := e.Seq()
			if seq == 0 || (i > 0 && seq != prev+1) {
				return nil, fmt.Errorf("%w: session %s: scrollback is not contiguous at entry %d", ErrStateCorrupt, ss.ID, i)
			}
			prev = seq
		}
	}
	return &State{ex: ex}, nil
}

// ImportState restores the sessions of a decoded state into a squatter that has none yet. It must
// be active (Activate, or after the first Apply). ring is the scrollback size to give the sessions
// (the configured squatter_scrollback; the squatter's own configuration is not applied yet when a
// restarted gtf imports). All or nothing.
func (s *Squatter) ImportState(st *State, ring int) error {
	ex := &st.ex
	if ring <= 0 {
		ring = DefaultScrollback
	}
	s.mu.Lock()
	if len(s.sessions) != 0 {
		s.mu.Unlock()
		return errors.New("squatter already has sessions")
	}
	s.mu.Unlock()
	type built struct {
		id string
		ss *session
	}
	var list []built
	closeAll := func() {
		for _, b := range list {
			b.ss.close()
		}
	}
	for _, x := range ex.Sessions {
		ss, err := s.newSession(x.ID, x.Meta, ring)
		if err != nil {
			closeAll()
			return err
		}
		list = append(list, built{x.ID, ss})
		ss.endedAt, ss.endReason, ss.endDetail, ss.lastText, ss.remoteEcho = x.EndedAt, x.EndReason, x.EndDetail, x.LastText, x.RemoteEcho
		for k, v := range x.Acks {
			ss.acks[k] = v
		}
		for k, v := range x.Gmcp {
			ss.gmcpLatest[k] = v
		}
		for k, v := range x.Msdp {
			ss.msdpLatest[k] = v
		}
		if err := ss.store.Seed(x.Entries); err != nil {
			closeAll()
			return fmt.Errorf("session %s: %w", x.ID, err)
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.sessions) != 0 {
		closeAll()
		return errors.New("squatter already has sessions")
	}
	if ex.Serial > s.serial {
		s.serial = ex.Serial
	}
	for _, b := range list {
		s.sessions[b.id] = b.ss
		s.order = append(s.order, b.id)
	}
	for _, id := range ex.Announce {
		s.announce[id] = true
	}
	s.hintPort = ex.Port
	s.nmu.Lock()
	for k, v := range ex.NAWS {
		s.nsent[k] = v
	}
	s.nmu.Unlock()
	return nil
}

// Import is DecodeState + ImportState with the configured scrollback size; it returns the session ids.
func (s *Squatter) Import(blob []byte) ([]string, error) {
	st, err := DecodeState(blob)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	ring := s.cfg.Scrollback
	s.mu.Unlock()
	if err := s.ImportState(st, ring); err != nil {
		return nil, err
	}
	return st.SessionIDs(), nil
}

// SessionLastSeq is the newest sequence number a session holds (0 for none or an unknown session).
func (s *Squatter) SessionLastSeq(id string) uint64 {
	if ss := s.session(id); ss != nil {
		_, last := ss.store.Bounds()
		return last
	}
	return 0
}

// Activate turns the taps on without a listener, for ImportState before the first Apply.
func (s *Squatter) Activate() { s.active.Store(true) }

// EndSession marks a session as ended with the given reason (a session whose connection did not
// survive a restart); it does nothing for an unknown or already ended one.
func (s *Squatter) EndSession(id, reason, detail string) { s.SessionEnded(id, reason, detail) }

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
