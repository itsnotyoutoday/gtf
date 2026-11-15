// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package squatter

// Carrying the listening socket and the connected clients across /restart (docs/RESTART.md, "The
// squatter"). The sessions travel in handoff.go; this file is the second half, so that a client is
// not even disconnected.
//
//	old process   PlanConns      what would be carried (any time)
//	              QuiesceConns   stop accepting; every client's reader and writer stand still
//	              ExportConns    each client's state + a duplicate of its socket (+ its TLS session)
//	              ResumeConns    the restart was abandoned: listen again, clients carry on
//	new process   PrepareConns   decode and import (a rehearsal releases it again)
//	              AdoptConns     the first Apply listens on the inherited socket and adopts the clients
//
// A client whose transport cannot be carried (a TLS 1.2 CBC session, a connection that is not TCP) is
// not exported: it is closed by the exec like in every restart, reconnects and resumes by sequence
// number. If the whole handoff fails, the caller falls back to that for all of them.

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"runtime"
	"sort"
	"time"

	"github.com/itsnotyoutoday/gtf/internal/squatter/wire"
)

// connsVersion is the version of ConnsState (optional additions do not change it).
const connsVersion = 1

// PeerState is one connected client carried together with its connection.
type PeerState struct {
	wire.ClientState
	Kind string `json:"kind"`          // "tcp" or "tls"
	Slot int    `json:"slot"`          // the inherited descriptor of its socket
	TLS  []byte `json:"tls,omitempty"` // KEY MATERIAL of the live TLS session (the state file is 0600)
}

// ConnsState is the listener and the clients of a squatter that is being carried.
type ConnsState struct {
	V        int         `json:"v"`
	Listener int         `json:"listener"` // slot of the listening socket, -1 for none
	TLS      bool        `json:"tls"`      // the clients speak TLS (squatter_tls)
	Port     int         `json:"port,omitempty"`
	Peers    []PeerState `json:"peers,omitempty"`
}

// DecodeConns parses and checks a ConnsState; nothing is adopted.
func DecodeConns(blob []byte) (*ConnsState, error) {
	var cs ConnsState
	if err := json.Unmarshal(blob, &cs); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrStateCorrupt, err)
	}
	if cs.V > connsVersion {
		return nil, fmt.Errorf("%w (connections format %d, this gtf reads up to %d)", ErrStateTooNew, cs.V, connsVersion)
	}
	if cs.V < 1 {
		return nil, fmt.Errorf("%w: no connections format version", ErrStateCorrupt)
	}
	seen := map[uint64]bool{}
	slots := map[int]bool{}
	if cs.Listener >= 0 {
		slots[cs.Listener] = true
	}
	for i := range cs.Peers {
		p := &cs.Peers[i]
		if err := p.Check(); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrStateCorrupt, err)
		}
		if seen[p.ID] || slots[p.Slot] || p.Slot < 0 {
			return nil, fmt.Errorf("%w: client %d repeats an id or a descriptor", ErrStateCorrupt, p.ID)
		}
		seen[p.ID], slots[p.Slot] = true, true
		switch p.Kind {
		case "tcp":
			if len(p.TLS) != 0 {
				return nil, fmt.Errorf("%w: a plain connection with a TLS session", ErrStateCorrupt)
			}
		case "tls":
			if len(p.TLS) == 0 {
				return nil, fmt.Errorf("%w: a TLS connection without its session", ErrStateCorrupt)
			}
		default:
			return nil, fmt.Errorf("%w: connection kind %q", ErrStateCorrupt, p.Kind)
		}
	}
	return &cs, nil
}

// peerBackendState is what the squatter itself keeps about a client (wire.ClientState.Backend).
type peerBackendState struct {
	Role     string    `json:"role"`
	Joined   int64     `json:"joined,omitempty"`
	Attached []string  `json:"attached,omitempty"` // the sessions it follows live
	GTF      *gtfState `json:"gtf,omitempty"`
}

// gtfState is the extension state of a client that negotiated it.
type gtfState struct {
	V         int              `json:"v"`
	Features  []string         `json:"features"`
	View      string           `json:"view,omitempty"`
	Attached  []string         `json:"attached,omitempty"`
	Widths    map[string]int   `json:"widths,omitempty"`
	LastWidth int              `json:"lastWidth,omitempty"`
	LastInput map[string]int64 `json:"lastInput,omitempty"` // unix ms
}

// ErrNotNow is what TLSMigrator.Export returns (wrapped) when the connection is busy at this instant and the
// export is worth retrying.
var ErrNotNow = errors.New("connection busy")

// carryState is the old process's state while a handover is under way.
type carryState struct {
	lf       *os.File // a duplicate of the listening socket; the socket lives on in it while no one accepts
	lfHanded bool     // ownership passed to the caller (ExportConns)
	exported map[uint64]net.Conn
	tls      bool
}

// pendingConns is what the new process inherited, until the first Apply uses it.
type pendingConns struct {
	tls   bool
	peers []pendingPeer
}

type pendingPeer struct {
	st   wire.ClientState
	conn net.Conn
}

// PeerPlan says what a restart does with one connected client.
type PeerPlan struct {
	Name, Kind string
	Carry      bool
	Why        string // when it is not carried
}

// PlanConns reports whether the listener would be carried and what each client would do.
func (s *Squatter) PlanConns() (listening bool, peers []PeerPlan) {
	s.mu.Lock()
	r := s.run
	s.mu.Unlock()
	if r == nil {
		return false, nil
	}
	if _, ok := r.ln.(*net.TCPListener); !ok {
		return false, nil
	}
	conns := r.srv.Conns()
	s.pmu.Lock()
	defer s.pmu.Unlock()
	for id, pi := range s.peers {
		pp := PeerPlan{Name: pi.p.DeviceName()}
		nc, ok := conns[id]
		if !ok {
			continue
		}
		kind, err := s.transportKind(nc)
		pp.Kind, pp.Carry = kind, err == nil
		if err != nil {
			pp.Why = err.Error()
		}
		peers = append(peers, pp)
	}
	sort.Slice(peers, func(i, j int) bool { return peers[i].Name < peers[j].Name })
	return true, peers
}

// transportKind says whether a client's connection can be carried and as what.
func (s *Squatter) transportKind(nc net.Conn) (string, error) {
	if _, ok := nc.(*net.TCPConn); ok {
		return "tcp", nil
	}
	if s.tlsm != nil {
		if mine, err := s.tlsm.Check(nc); mine {
			if err != nil {
				return "", err
			}
			return "tls", nil
		}
	}
	return "", fmt.Errorf("a %T connection (a TLS configuration the migratable TLS does not support)", nc)
}

// QuiesceConns stops accepting and brings the clients to a standstill; pump must run the work the
// squatter has queued on the host's goroutine (see wire.Server.Quiesce). On error nothing is left
// stopped.
func (s *Squatter) QuiesceConns(timeout time.Duration, pump func()) error {
	s.mu.Lock()
	r, cs := s.run, s.carry
	s.mu.Unlock()
	if cs != nil {
		return nil
	}
	if r == nil {
		return errors.New("not listening")
	}
	tl, ok := r.ln.(*net.TCPListener)
	if !ok {
		return errors.New("the listener is not TCP")
	}
	lf, err := tl.File()
	if err != nil {
		return fmt.Errorf("duplicating the listening socket: %w", err)
	}
	cs = &carryState{lf: lf, tls: r.tcfg != nil, exported: map[uint64]net.Conn{}}
	s.mu.Lock()
	s.carry = cs
	s.mu.Unlock()
	_ = r.hs.Close() // nobody accepts now; connections that arrive wait in the socket, which lf keeps open
	if dropped := r.srv.Quiesce(timeout, pump); len(dropped) > 0 {
		s.log.Warn("squatter: clients did not stand still for the restart and were dropped", "n", len(dropped))
	}
	return nil
}

// ConnsExport is what ExportConns hands to the restart code: the descriptors (the caller adds them to
// the handoff and sets the slots in State) and the state to save.
type ConnsExport struct {
	State     *ConnsState
	Listener  *os.File
	PeerFiles []*os.File // the same order as State.Peers
}

// ExportConns exports the quiesced clients and the listener. A client that cannot be exported is left
// out (it ends with the exec and reconnects).
func (s *Squatter) ExportConns() (*ConnsExport, error) {
	s.mu.Lock()
	r, cs := s.run, s.carry
	s.mu.Unlock()
	if r == nil || cs == nil {
		return nil, errors.New("the squatter is not quiesced")
	}
	cl := r.srv.Export(func(_ uint64, nc net.Conn) (any, error) { return s.exportTransport(nc) })
	ex := &ConnsExport{State: &ConnsState{V: connsVersion, Listener: -1, TLS: cs.tls}, Listener: cs.lf}
	if _, port, err := net.SplitHostPort(r.addr); err == nil {
		fmt.Sscanf(port, "%d", &ex.State.Port)
	}
	cs.lfHanded = true
	for _, c := range cl {
		t := c.Transport.(*peerTransport)
		c.State.Backend = s.exportPeerBackend(c.State.ID)
		ex.State.Peers = append(ex.State.Peers, PeerState{ClientState: c.State, Kind: t.kind, TLS: t.blob})
		ex.PeerFiles = append(ex.PeerFiles, t.file)
		cs.exported[c.State.ID] = t.nc
	}
	return ex, nil
}

// peerTransport is a client's connection prepared for the handover.
type peerTransport struct {
	kind string
	blob []byte // TLS session, key material
	file *os.File
	nc   net.Conn
}

func (s *Squatter) exportTransport(nc net.Conn) (*peerTransport, error) {
	if c, ok := nc.(*net.TCPConn); ok {
		f, err := c.File()
		if err != nil {
			return nil, err
		}
		return &peerTransport{kind: "tcp", file: f, nc: nc}, nil
	}
	if s.tlsm != nil {
		if mine, _ := s.tlsm.Check(nc); mine {
			var blob []byte
			var f *os.File
			var err error
			for try := 0; ; try++ {
				if blob, f, err = s.tlsm.Export(nc); err == nil || try >= 200 || !errors.Is(err, ErrNotNow) {
					break
				}
				runtime.Gosched()
				time.Sleep(time.Millisecond)
			}
			if err != nil {
				return nil, err
			}
			return &peerTransport{kind: "tls", blob: blob, file: f, nc: nc}, nil
		}
	}
	return nil, fmt.Errorf("a %T connection cannot be carried", nc)
}

func (s *Squatter) exportPeerBackend(id uint64) json.RawMessage {
	s.pmu.Lock()
	pi := s.peers[id]
	var st peerBackendState
	if pi != nil {
		st.Role, st.Joined = pi.role, pi.joined.UnixMilli()
		if g := pi.gtf; g != nil {
			gs := &gtfState{V: g.v, View: g.view, LastWidth: g.lastWidth}
			for f := range g.features {
				gs.Features = append(gs.Features, f)
			}
			sort.Strings(gs.Features)
			for id := range g.attached {
				gs.Attached = append(gs.Attached, id)
			}
			sort.Strings(gs.Attached)
			if len(g.widths) > 0 {
				gs.Widths = map[string]int{}
				for k, v := range g.widths {
					gs.Widths[k] = v
				}
			}
			if len(g.lastInput) > 0 {
				gs.LastInput = map[string]int64{}
				for k, v := range g.lastInput {
					gs.LastInput[k] = v.UnixMilli()
				}
			}
			st.GTF = gs
		}
	}
	s.pmu.Unlock()
	for _, ss := range s.sessionList() {
		ss.mu.Lock()
		for _, p := range ss.attached {
			if p.ID() == id {
				st.Attached = append(st.Attached, ss.id)
			}
		}
		ss.mu.Unlock()
	}
	sort.Strings(st.Attached)
	b, _ := json.Marshal(st)
	return b
}

// AdoptPeer implements wire.ClientCarrier: it registers a client that arrives with its connection.
func (b *backend) AdoptPeer(p wire.Peer, data json.RawMessage) error {
	s := b.s
	var st peerBackendState
	if len(data) > 0 {
		if err := json.Unmarshal(data, &st); err != nil {
			return err
		}
	}
	joined := time.Now()
	if st.Joined != 0 {
		joined = time.UnixMilli(st.Joined)
	}
	role := st.Role
	if role == "" {
		role = roleName(p)
	}
	pi := &peerInfo{p: p, role: role, joined: joined}
	if g := st.GTF; g != nil {
		pg := &gtfPeer{v: g.V, features: map[string]bool{}, view: g.View, attached: map[string]bool{}, widths: map[string]int{},
			lastWidth: g.LastWidth, lastInput: map[string]time.Time{}, lim: map[string]*bucket{}}
		for _, f := range g.Features {
			pg.features[f] = true
		}
		for _, id := range g.Attached {
			pg.attached[id] = true
		}
		for k, v := range g.Widths {
			pg.widths[k] = v
		}
		for k, v := range g.LastInput {
			pg.lastInput[k] = time.UnixMilli(v)
		}
		pi.gtf = pg
	}
	s.pmu.Lock()
	s.peers[p.ID()] = pi
	s.pmu.Unlock()
	for _, id := range st.Attached {
		if ss := s.session(id); ss != nil {
			ss.mu.Lock()
			ss.attached[p.ID()] = p // no backlog: the client has everything up to here
			ss.mu.Unlock()
		}
	}
	return nil
}

// ResumeConns undoes QuiesceConns/ExportConns after an abandoned restart: the squatter listens again
// and the clients carry on exactly where they were.
func (s *Squatter) ResumeConns() {
	s.mu.Lock()
	r, cs := s.run, s.carry
	s.carry = nil
	s.mu.Unlock()
	if cs == nil || r == nil {
		if cs != nil && !cs.lfHanded {
			cs.lf.Close()
		}
		return
	}
	ln, err := net.FileListener(cs.lf)
	if !cs.lfHanded {
		cs.lf.Close()
	}
	r.srv.Resume()
	if err != nil {
		s.fail(fmt.Errorf("listening again after the abandoned restart: %w", err))
		return
	}
	s.listen(r, ln)
}

// Abandon drops the listener and the clients without a word on any connection, as the exec of a
// restarted gtf does to the old process (tests that run the "new process" in the same process use it;
// a real exec needs nothing). The sessions are left alone.
func (s *Squatter) Abandon() {
	s.mu.Lock()
	r, cs := s.run, s.carry
	s.run, s.carry = nil, nil
	s.mu.Unlock()
	if r == nil {
		return
	}
	if cs != nil && len(cs.exported) > 0 {
		all := r.srv.Conns()
		r.srv.Abandon()
		for id, nc := range all {
			if _, carried := cs.exported[id]; !carried {
				_ = nc.Close() // what the exec does to a client that was not handed over
			}
		}
		for _, nc := range cs.exported {
			if _, plain := nc.(*net.TCPConn); !plain && s.tlsm != nil {
				_ = s.tlsm.Discard(nc) // no close_notify: the session lives on in the other process
			} else {
				_ = nc.Close()
			}
		}
		_ = r.hs.Close()
		return
	}
	s.closeRunning(r)
}

// ---------------------------------------------------------------- the new process

// preparedConns is a decoded handoff with its sockets turned into connections, not yet used.
type preparedConns struct {
	s     *Squatter
	ln    net.Listener
	tls   bool
	peers []pendingPeer
	skip  int // clients that could not be adopted
}

// PrepareConns turns the inherited descriptors into a listener and connections (TLS sessions are
// imported: no I/O, no handshake). file returns the descriptor of a slot. Release (a rehearsal) drops
// everything without a byte on any connection; Use hands it to the squatter.
func (s *Squatter) PrepareConns(cs *ConnsState, file func(slot int) (*os.File, error)) (*Prepared, error) {
	p := &preparedConns{tls: cs.TLS, s: s}
	fail := func(err error) (*Prepared, error) { (&Prepared{p: p}).Release(); return nil, err }
	if cs.Listener >= 0 {
		f, err := file(cs.Listener)
		if err != nil {
			return fail(err)
		}
		ln, err := net.FileListener(f)
		if err != nil {
			return fail(fmt.Errorf("inherited listener: %w", err))
		}
		if _, ok := ln.(*net.TCPListener); !ok {
			ln.Close()
			return fail(errors.New("inherited listener is not TCP"))
		}
		p.ln = ln
		f.Close()
	}
	for i := range cs.Peers {
		ps := &cs.Peers[i]
		f, err := file(ps.Slot)
		if err != nil {
			return fail(err)
		}
		nc, err := net.FileConn(f)
		f.Close()
		if err != nil {
			p.skip++
			continue
		}
		var conn net.Conn = nc
		if ps.Kind == "tls" {
			if s.tlsm == nil {
				nc.Close()
				clear(ps.TLS)
				p.skip++
				continue
			}
			tc, err := s.tlsm.Import(nc, ps.TLS)
			clear(ps.TLS)
			if err != nil {
				nc.Close()
				p.skip++
				continue
			}
			conn = tc
		}
		p.peers = append(p.peers, pendingPeer{st: ps.ClientState, conn: conn})
	}
	return &Prepared{p: p}, nil
}

func (p *preparedConns) discard(nc net.Conn) {
	if _, plain := nc.(*net.TCPConn); !plain && p.s != nil && p.s.tlsm != nil {
		_ = p.s.tlsm.Discard(nc)
		return
	}
	_ = nc.Close()
}

// Prepared is the result of PrepareConns.
type Prepared struct{ p *preparedConns }

// Peers is the number of clients that came through.
func (pr *Prepared) Peers() int { return len(pr.p.peers) }

// Release drops everything (the rehearsal of a restart): no close_notify, no close frame.
func (pr *Prepared) Release() {
	p := pr.p
	if p.ln != nil {
		p.ln.Close()
	}
	for _, pp := range p.peers {
		p.discard(pp.conn)
	}
	p.ln, p.peers = nil, nil
}

// AdoptConns gives the squatter the inherited listener and clients: the first Apply listens on the
// socket and adopts the clients (before ImportState's sessions are used: call it after ImportState).
func (s *Squatter) AdoptConns(pr *Prepared) {
	p := pr.p
	s.mu.Lock()
	s.inherit = p.ln
	s.pending = &pendingConns{tls: p.tls, peers: p.peers}
	s.mu.Unlock()
}

// inheritUsable says whether an inherited listener fits the configuration now being applied.
func inheritUsable(cfg Config, ln net.Listener, tls bool) bool {
	if cfg.TLS != tls {
		return false
	}
	if cfg.Port != 0 {
		if a, ok := ln.Addr().(*net.TCPAddr); !ok || a.Port != cfg.Port {
			return false
		}
	}
	return true
}

// adoptPending gives the clients inherited from the old process to the server that now runs.
func (s *Squatter) adoptPending(nr *running) {
	s.mu.Lock()
	pc := s.pending
	s.pending = nil
	s.mu.Unlock()
	if pc == nil {
		return
	}
	n := 0
	for _, pp := range pc.peers {
		if pc.tls != (nr.tcfg != nil) {
			(&Prepared{p: &preparedConns{s: s, peers: []pendingPeer{pp}}}).Release()
			continue
		}
		if _, err := nr.srv.Adopt(pp.st, pp.conn); err != nil {
			s.log.Warn("squatter: a client could not be carried over", "device", pp.st.Name, "err", err)
			_ = pp.conn.Close()
			continue
		}
		n++
	}
	if n > 0 {
		s.publishLater()
	}
}

// dropInherited releases an inherited listener and clients that the configuration cannot use.
func (s *Squatter) dropInherited(ln net.Listener) {
	if ln != nil {
		ln.Close()
	}
	s.mu.Lock()
	pc := s.pending
	s.pending = nil
	s.mu.Unlock()
	if pc != nil {
		(&Prepared{p: &preparedConns{s: s, peers: pc.peers}}).Release()
	}
}
