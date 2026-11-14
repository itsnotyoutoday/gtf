// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package squatter

import (
	"context"
	"time"

	"github.com/itsnotyoutoday/gtf/internal/squatter/proto"
	"github.com/itsnotyoutoday/gtf/internal/squatter/wire"
)

// backend implements wire.Backend (and SecretSender and ExtBackend) for gtf.
type backend struct{ s *Squatter }

const (
	rankViewer   = 1
	rankOperator = 2
	rankAdmin    = 3
)

// rank of a peer's role. Devices paired before roles existed ("owner") have full access.
func rank(p wire.Peer) int {
	r, ok := p.(wire.Roled)
	if !ok {
		return rankAdmin
	}
	switch r.Role() {
	case wire.RoleViewer:
		return rankViewer
	case wire.RoleOperator:
		return rankOperator
	}
	return rankAdmin
}

func need(p wire.Peer, min int, what string) error {
	if rank(p) < min {
		return wire.Errf(proto.ErrForbidden, "your device role does not allow "+what)
	}
	return nil
}

func roleName(p wire.Peer) string {
	if r, ok := p.(wire.Roled); ok {
		return r.Role()
	}
	return "owner"
}

// hostDo runs f on the engine goroutine, giving up after a while (a script stuck in a loop must not
// pin every client goroutine forever).
func (b *backend) hostDo(f func()) error {
	done := make(chan struct{})
	go func() {
		b.s.host.Do(func() { f(); close(done) })
	}()
	select {
	case <-done:
		return nil
	case <-time.After(30 * time.Second):
		return wire.Errf(proto.ErrInternal, "gtf did not answer in time")
	}
}

func (b *backend) PeerJoined(p wire.Peer) []proto.SessionInfo {
	s := b.s
	role := roleName(p)
	s.pmu.Lock()
	s.peers[p.ID()] = &peerInfo{p: p, role: role, joined: time.Now()}
	s.pmu.Unlock()
	fresh := s.noteJoin(p)
	name, dev := p.DeviceName(), p.DeviceID()
	_ = dev
	s.host.Post(func() {
		if fresh {
			s.host.Hook("SQUAT_PAIR", name, role)
		}
		s.host.Hook("SQUAT_CONNECT", name, role)
	})
	s.publishLater()
	return b.List(p)
}

func (b *backend) PeerLeft(p wire.Peer) {
	s := b.s
	s.pmu.Lock()
	delete(s.peers, p.ID())
	s.pmu.Unlock()
	for _, ss := range s.sessionList() {
		ss.detach(p)
	}
	name, role := p.DeviceName(), roleName(p)
	s.host.Post(func() { s.host.Hook("SQUAT_DISCONNECT", name, role) })
	s.publishLater()
}

// publishLater refreshes the status variables from a network goroutine.
func (s *Squatter) publishLater() {
	s.host.Post(func() {
		s.mu.Lock()
		r := s.run
		s.mu.Unlock()
		s.publish(r)
	})
}

// noteJoin reports whether the device was paired just now (its record is brand new).
func (s *Squatter) noteJoin(p wire.Peer) bool {
	as := s.store()
	if as == nil {
		return false
	}
	ds, err := as.Devices()
	if err != nil {
		return false
	}
	for _, d := range ds {
		if d.DeviceID == p.DeviceID() {
			s.mu.Lock()
			defer s.mu.Unlock()
			if time.Since(time.UnixMilli(d.CreatedAt)) < 30*time.Second && !s.announce[d.DeviceID] {
				s.announce[d.DeviceID] = true
				return true
			}
		}
	}
	return false
}

func (b *backend) List(p wire.Peer) []proto.SessionInfo {
	list := b.s.sessionList()
	out := make([]proto.SessionInfo, 0, len(list))
	for _, ss := range list {
		out = append(out, ss.info(p.DeviceID()))
	}
	return out
}

func (b *backend) Connect(ctx context.Context, p wire.Peer, req wire.ConnectReq) (proto.SessionInfo, error) {
	if err := need(p, rankOperator, "opening connections"); err != nil {
		return proto.SessionInfo{}, err
	}
	w := req.World
	if w.Ref == "" && (w.Host == "" || w.Port <= 0 || w.Port > 65535) {
		return proto.SessionInfo{}, wire.Errf(proto.ErrBadRequest, "session.connect needs a world ref, or host and port")
	}
	var id string
	var err error
	if e := b.hostDo(func() {
		id, err = b.s.host.Connect(ConnectTarget{Ref: w.Ref, Name: w.Name, Host: w.Host, Port: w.Port, TLS: w.TLS})
	}); e != nil {
		return proto.SessionInfo{}, e
	}
	if err != nil {
		return proto.SessionInfo{}, wire.Errf(proto.ErrBadRequest, err.Error())
	}
	ss := b.s.session(id)
	if ss == nil {
		return proto.SessionInfo{}, wire.Errf(proto.ErrInternal, "gtf opened the connection but did not report it")
	}
	return ss.info(p.DeviceID()), nil
}

func (b *backend) Disconnect(p wire.Peer, session string) error {
	if err := need(p, rankOperator, "disconnecting"); err != nil {
		return err
	}
	if b.s.session(session) == nil {
		return wire.Errf(proto.ErrNoSuchSession, "no such session")
	}
	var err error
	if e := b.hostDo(func() { err = b.s.host.Disconnect(session) }); e != nil {
		return e
	}
	if err != nil {
		return wire.Errf(proto.ErrBadRequest, err.Error())
	}
	return nil
}

func (b *backend) Dismiss(p wire.Peer, session string) error {
	if err := need(p, rankOperator, "dismissing sessions"); err != nil {
		return err
	}
	ss := b.s.session(session)
	if ss == nil {
		return wire.Errf(proto.ErrNoSuchSession, "no such session")
	}
	if !ss.endedState() {
		return wire.Errf(proto.ErrBadRequest, "session is still live: disconnect it first")
	}
	b.s.mu.Lock()
	b.s.removeLocked(session)
	b.s.mu.Unlock()
	return nil
}

func (b *backend) Attach(p wire.Peer, req wire.AttachReq) error {
	ss := b.s.session(req.Session)
	if ss == nil {
		return wire.Errf(proto.ErrNoSuchSession, "no such session")
	}
	return ss.attach(p, req)
}

func (b *backend) Detach(p wire.Peer, session string) error {
	if ss := b.s.session(session); ss != nil {
		ss.detach(p)
	}
	return nil
}

func (b *backend) send(p wire.Peer, session, text string, secret bool) error {
	if err := need(p, rankOperator, "sending text"); err != nil {
		return err
	}
	ss := b.s.session(session)
	if ss == nil {
		return wire.Errf(proto.ErrNoSuchSession, "no such session")
	}
	if len(text) > wire.MaxSendBytes {
		return wire.Errf(proto.ErrBadRequest, "text too long")
	}
	if ss.endedState() {
		return wire.Errf(proto.ErrSessionEnded, "the connection has ended")
	}
	cfg := b.s.Config()
	cmd := !secret && cfg.Commands && rank(p) >= rankAdmin && len(text) > 0 && text[0] == '/'
	var err error
	if e := b.hostDo(func() {
		// Everything below runs on the engine goroutine, so the echo, the command's own output and the
		// game's answer are recorded in the order they happen.
		if cmd {
			b.s.Line(session, proto.KindEcho, []proto.Span{{S: text}}, false, p.DeviceID())
			err = b.s.host.SendLine(session, text, SendOpts{Origin: p.DeviceID(), Command: true})
			return
		}
		if err = b.s.host.SendLine(session, text, SendOpts{Origin: p.DeviceID(), Secret: secret}); err == nil && !secret {
			b.s.Line(session, proto.KindEcho, []proto.Span{{S: text}}, false, p.DeviceID())
		}
	}); e != nil {
		return e
	}
	if err != nil {
		return wire.Errf(proto.ErrBadRequest, err.Error())
	}
	return nil
}

func (b *backend) Send(p wire.Peer, session, text string) error {
	return b.send(p, session, text, false)
}

// SendSecret delivers text without echo or record.
func (b *backend) SendSecret(p wire.Peer, session, text string) error {
	return b.send(p, session, text, true)
}

func (b *backend) Ack(p wire.Peer, session string, seq uint64) error {
	if ss := b.s.session(session); ss != nil {
		ss.ack(p.DeviceID(), seq)
	}
	return nil
}

// Resize is ignored: the game keeps being told the size of gtf's own terminal, where the local user is.
func (b *backend) Resize(p wire.Peer, session string, cols, rows int) error { return nil }

// CapsChanged: capabilities toward the game are fixed by gtf's own terminal.
func (b *backend) CapsChanged(p wire.Peer) {}

var (
	_ wire.Backend      = (*backend)(nil)
	_ wire.SecretSender = (*backend)(nil)
	_ wire.ExtBackend   = (*backend)(nil)
)
