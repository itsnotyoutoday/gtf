// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package squatter

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/itsnotyoutoday/gtf/internal/squatter/proto"
	"github.com/itsnotyoutoday/gtf/internal/squatter/scrollback"
	"github.com/itsnotyoutoday/gtf/internal/squatter/wire"
)

// maskedEcho replaces echoed text while the game has asked for no echo (password prompts).
const maskedEcho = "********"

// maxStateKeys bounds the GMCP/MSDP snapshot.
const maxStateKeys = 1024

func nowMs() int64 { return time.Now().UnixMilli() }

// session mirrors one gtf connection: the squatter keeps its scrollback, gtf is the source of
// truth for everything else.
type session struct {
	s     *Squatter
	id    string
	store *scrollback.Store

	mu         sync.Mutex
	world      string
	host       string
	port       int
	tls        bool
	state      string
	startedAt  int64
	endedAt    int64
	endReason  string
	endDetail  string
	lastText   string
	remoteEcho bool
	acks       map[string]uint64
	attached   map[uint64]wire.Peer
	gmcpLatest map[string]json.RawMessage
	msdpLatest map[string]any
	closed     bool
}

// SessionMeta describes a connection when the squatter first hears of it.
type SessionMeta struct {
	World     string
	Host      string
	Port      int
	TLS       bool
	StartedAt int64 // unix ms (0 = now)
	State     string
}

func (s *Squatter) newSession(id string, m SessionMeta, ring int) (*session, error) {
	st, err := scrollback.New(scrollback.Options{RingSize: ring})
	if err != nil {
		return nil, err
	}
	if m.StartedAt == 0 {
		m.StartedAt = nowMs()
	}
	if m.State == "" {
		m.State = proto.StateConnecting
	}
	return &session{s: s, id: id, store: st, world: m.World, host: m.Host, port: m.Port, tls: m.TLS, state: m.State,
		startedAt: m.StartedAt, acks: map[string]uint64{}, attached: map[uint64]wire.Peer{},
		gmcpLatest: map[string]json.RawMessage{}, msdpLatest: map[string]any{}}, nil
}

// SessionOpened registers a connection and returns its session id ("" when the squatter is off).
func (s *Squatter) SessionOpened(m SessionMeta) string {
	if !s.active.Load() {
		return ""
	}
	s.mu.Lock()
	s.serial++
	id := fmt.Sprintf("g%s-%d", strconv.FormatInt(nowMs(), 36), s.serial)
	ss, err := s.newSession(id, m, s.cfg.Scrollback)
	if err != nil {
		s.mu.Unlock()
		return ""
	}
	s.sessions[id] = ss
	s.order = append(s.order, id)
	// bound the registry: forget the oldest ended sessions first
	for len(s.order) > s.cfg.MaxSessions {
		victim := ""
		for _, oid := range s.order {
			if o := s.sessions[oid]; o != nil && o != ss && o.endedState() {
				victim = oid
				break
			}
		}
		if victim == "" {
			break
		}
		s.removeLocked(victim)
	}
	s.mu.Unlock()
	ss.mu.Lock()
	ss.broadcastLocked()
	ss.mu.Unlock()
	return id
}

func (ss *session) endedState() bool {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	return ss.state == proto.StateEnded
}

// removeLocked forgets a session (Squatter.mu held) and tells the clients.
func (s *Squatter) removeLocked(id string) {
	ss := s.sessions[id]
	delete(s.sessions, id)
	for i, o := range s.order {
		if o == id {
			s.order = append(s.order[:i], s.order[i+1:]...)
			break
		}
	}
	if ss != nil {
		ss.close()
		for _, p := range s.peerList() {
			p.Send(removedMsg(id))
		}
	}
}

func removedMsg(id string) map[string]any {
	return map[string]any{"v": proto.Version, "t": wire.TSessionRemoved, "session": id}
}

func (s *Squatter) session(id string) *session {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sessions[id]
}

func (s *Squatter) sessionList() []*session {
	s.mu.Lock()
	out := make([]*session, 0, len(s.order))
	for _, id := range s.order {
		if ss := s.sessions[id]; ss != nil {
			out = append(out, ss)
		}
	}
	s.mu.Unlock()
	return out
}

// NumSessions is the number of sessions held.
func (s *Squatter) NumSessions() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.sessions)
}

// SessionConnected says the connection was established.
func (s *Squatter) SessionConnected(id string) {
	if ss := s.session(id); ss != nil {
		ss.update(func() { ss.state = proto.StateConnected })
	}
}

// SessionEnded says the connection ended. reason is a proto.End* constant.
func (s *Squatter) SessionEnded(id, reason, detail string) {
	ss := s.session(id)
	if ss == nil {
		return
	}
	ss.update(func() {
		if ss.state == proto.StateEnded {
			return
		}
		ss.state, ss.endedAt, ss.endReason, ss.endDetail, ss.remoteEcho = proto.StateEnded, nowMs(), reason, detail, false
	})
}

// SetRemoteEcho says whether the game asked the client not to echo (password prompt).
func (s *Squatter) SetRemoteEcho(id string, on bool) {
	if ss := s.session(id); ss != nil {
		ss.update(func() { ss.remoteEcho = on })
	}
}

func (ss *session) update(f func()) {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	if ss.closed {
		return
	}
	type key struct {
		state, reason, detail string
		ended                 int64
		echo                  bool
	}
	k := func() key { return key{ss.state, ss.endReason, ss.endDetail, ss.endedAt, ss.remoteEcho} }
	before := k()
	f()
	if k() != before {
		ss.broadcastLocked()
	}
}

func (ss *session) infoLocked(device string) proto.SessionInfo {
	first, last := ss.store.Bounds()
	var unread uint64
	if ack := ss.acks[device]; last > ack {
		unread = last - ack
	}
	return proto.SessionInfo{ID: ss.id, Name: ss.world, Host: ss.host, Port: ss.port, TLS: ss.tls, State: ss.state,
		StartedAt: ss.startedAt, FirstSeq: first, LastSeq: last, Unread: unread, Attached: len(ss.attached),
		RemoteEcho: ss.remoteEcho, EndedAt: ss.endedAt, EndReason: ss.endReason, EndDetail: ss.endDetail, LastText: ss.lastText,
		TF: &proto.TFInfo{World: ss.world, Fg: false, Input: wire.ModeBoth, Automation: wire.ModeTF}}
}

func (ss *session) info(device string) proto.SessionInfo {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	return ss.infoLocked(device)
}

func (ss *session) broadcastLocked() {
	for _, p := range ss.s.peerList() {
		p.Send(wire.SessionMsg{V: proto.Version, T: wire.TSessionState, Session: ss.infoLocked(p.DeviceID())})
	}
}

func (ss *session) appendLocked(e scrollback.Entry) {
	seq, err := ss.store.Append(e)
	if err != nil {
		return
	}
	switch {
	case e.Line != nil:
		e.Line.Seq = seq
	case e.Gmcp != nil:
		e.Gmcp.Seq = seq
	case e.Msdp != nil:
		e.Msdp.Seq = seq
	}
	raw, err := json.Marshal(e)
	if err != nil {
		return
	}
	for _, p := range ss.attached {
		p.SendRaw(raw)
	}
}

// Line records one line shown in the session's world: kind is proto.KindGame, KindEcho or
// KindSystem; spans carry what gtf displays after its triggers and highlights ran; origin
// says who sent an echoed line ("tf" for the local user, else a device id).
func (s *Squatter) Line(id, kind string, spans []proto.Span, prompt bool, origin string) {
	if id == "" || !s.active.Load() {
		return
	}
	ss := s.session(id)
	if ss == nil {
		return
	}
	ss.mu.Lock()
	defer ss.mu.Unlock()
	if ss.closed {
		return
	}
	if kind == proto.KindEcho && ss.remoteEcho && len(spans) > 0 {
		spans = []proto.Span{{S: maskedEcho}}
	}
	for i := range spans {
		spans[i].S = strings.ToValidUTF8(spans[i].S, "�")
	}
	l := proto.Line{V: proto.Version, T: proto.TLine, Session: ss.id, Ts: nowMs(), Kind: kind, Spans: spans, Prompt: prompt}
	if spans == nil {
		l.Spans = []proto.Span{}
	}
	if kind == proto.KindEcho {
		l.Origin = origin
	}
	if kind == proto.KindGame && !prompt {
		if t := strings.TrimSpace(plain(spans)); t != "" {
			ss.lastText = t
		}
	}
	ss.appendLocked(scrollback.LineEntry(l))
}

func plain(spans []proto.Span) string {
	var b strings.Builder
	for _, sp := range spans {
		b.WriteString(sp.S)
	}
	return b.String()
}

// Gmcp records a GMCP message from the game (data is the JSON text, "" when absent).
func (s *Squatter) Gmcp(id, pkg, data string) {
	ss := s.session(id)
	if ss == nil || !s.active.Load() {
		return
	}
	ss.mu.Lock()
	defer ss.mu.Unlock()
	if ss.closed {
		return
	}
	raw := json.RawMessage("null")
	if d := strings.TrimSpace(strings.ToValidUTF8(data, "")); d != "" && json.Valid([]byte(d)) {
		raw = json.RawMessage(d)
	}
	pkg = strings.ToValidUTF8(pkg, "")
	if _, known := ss.gmcpLatest[pkg]; known || len(ss.gmcpLatest) < maxStateKeys {
		ss.gmcpLatest[pkg] = raw
	}
	ss.appendLocked(scrollback.GmcpEntry(proto.GmcpMsg{V: proto.Version, T: proto.TGmcp, Session: ss.id, Ts: nowMs(), Package: pkg, Data: raw}))
}

// Msdp records an MSDP variable from the game (strings, []any, map[string]any, float64, bool).
func (s *Squatter) Msdp(id, name string, value any) {
	ss := s.session(id)
	if ss == nil || !s.active.Load() {
		return
	}
	vars := map[string]any{strings.ToValidUTF8(name, "�"): sanitize(value)}
	ss.mu.Lock()
	defer ss.mu.Unlock()
	if ss.closed {
		return
	}
	for k, v := range vars {
		if _, known := ss.msdpLatest[k]; known || len(ss.msdpLatest) < maxStateKeys {
			ss.msdpLatest[k] = v
		}
	}
	ss.appendLocked(scrollback.MsdpEntry(proto.MsdpMsg{V: proto.Version, T: proto.TMsdp, Session: ss.id, Ts: nowMs(), Vars: vars}))
}

func sanitize(x any) any {
	switch t := x.(type) {
	case string:
		return strings.ToValidUTF8(t, "�")
	case []any:
		out := make([]any, len(t))
		for i := range t {
			out[i] = sanitize(t[i])
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, e := range t {
			out[strings.ToValidUTF8(k, "�")] = sanitize(e)
		}
		return out
	}
	return x
}

func (ss *session) snapshotLocked(reqID string) wire.StateMsg {
	_, last := ss.store.Bounds()
	m := wire.StateMsg{V: proto.Version, T: wire.TState, ReqID: reqID, Session: ss.id, AsOf: last,
		Gmcp: make(map[string]json.RawMessage, len(ss.gmcpLatest)), Msdp: make(map[string]any, len(ss.msdpLatest))}
	for k, v := range ss.gmcpLatest {
		m.Gmcp[k] = v
	}
	for k, v := range ss.msdpLatest {
		m.Msdp[k] = v
	}
	return m
}

// attach follows docs/PROTOCOL.md 5.1: attached, backlog*, then live entries with no gap or duplicate.
func (ss *session) attach(p wire.Peer, req wire.AttachReq) error {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	var since uint64
	if req.Since != nil {
		since = *req.Since
	}
	max := req.MaxBacklog
	if max <= 0 {
		max = wire.DefaultBacklog
	}
	if max > wire.MaxBacklog {
		max = wire.MaxBacklog
	}
	rp, err := ss.store.Replay(since, req.Since != nil, max)
	if err != nil {
		return fmt.Errorf("replay: %w", err)
	}
	var gap *wire.Gap
	if rp.Gap != nil {
		gap = &wire.Gap{Missed: rp.Gap.Missed}
	}
	p.Send(wire.AttachedMsg{V: proto.Version, T: wire.TAttached, ReqID: req.ID, Session: ss.id, FirstSeq: rp.FirstSeq, LastSeq: rp.LastSeq,
		ReplayFrom: rp.ReplayFrom, Gap: gap})
	frame := make([]json.RawMessage, 0, wire.BacklogChunk)
	size := 0
	flush := func(done bool) {
		p.Send(wire.BacklogMsg{V: proto.Version, T: wire.TBacklog, Session: ss.id, Entries: frame, Done: done})
		frame, size = make([]json.RawMessage, 0, wire.BacklogChunk), 0
	}
	for _, e := range rp.Entries {
		raw, err := json.Marshal(e)
		if err != nil {
			continue
		}
		frame = append(frame, raw)
		size += len(raw)
		if len(frame) >= wire.BacklogChunk || size >= wire.BacklogChunkBytes {
			flush(false)
		}
	}
	flush(true)
	if len(ss.gmcpLatest) > 0 || len(ss.msdpLatest) > 0 {
		p.Send(ss.snapshotLocked("")) // after the backlog, before any live entry
	}
	ss.attached[p.ID()] = p
	ss.broadcastLocked()
	return nil
}

func (ss *session) detach(p wire.Peer) {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	if _, ok := ss.attached[p.ID()]; !ok {
		return
	}
	delete(ss.attached, p.ID())
	ss.broadcastLocked()
}

func (ss *session) ack(device string, seq uint64) {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	if _, last := ss.store.Bounds(); seq > last {
		seq = last
	}
	if seq > ss.acks[device] {
		ss.acks[device] = seq
	}
}

func (ss *session) close() {
	ss.mu.Lock()
	ss.closed = true
	ss.mu.Unlock()
	_ = ss.store.Close()
}
