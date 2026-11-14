// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package squatter

import (
	"encoding/json"
	"errors"
	"regexp"
	"strings"

	"github.com/itsnotyoutoday/gtf/internal/squatter/proto"
	"github.com/itsnotyoutoday/gtf/internal/squatter/wire"
)

// HandleExt implements the tf-squatterd (TFSP) extension messages that make sense inside gtf:
// control.get/set, world.list, gmcp.send, msdp.send, state.get and tf.exec. world.put/delete/rename
// and tf.script.* are not supported (use tf.exec with /addworld, /load); they answer an error.
func (b *backend) HandleExt(p wire.Peer, t, reqID string, raw []byte) (bool, error) {
	if b.s.Config().Kind != KindTF {
		return false, nil
	}
	switch t {
	case wire.TControlGet:
		return true, b.controlGet(p, reqID, raw)
	case wire.TControlSet:
		return true, b.controlSet(p, reqID, raw)
	case wire.TWorldList:
		return true, b.worldList(p, reqID)
	case wire.TGmcpSend:
		return true, b.gmcpSend(p, reqID, raw)
	case wire.TMsdpSend:
		return true, b.msdpSend(p, reqID, raw)
	case wire.TStateGet:
		return true, b.stateGet(p, reqID, raw)
	case wire.TExec:
		go func() {
			if err := b.tfExec(p, reqID, raw); err != nil {
				var pe *wire.ProtoError
				if errors.As(err, &pe) {
					p.Send(wire.NewError(reqID, pe.Code, pe.Message))
				} else {
					p.Send(wire.NewError(reqID, proto.ErrInternal, "internal error"))
				}
			}
		}()
		return true, nil
	case wire.TWorldPut, wire.TWorldDelete, wire.TWorldRename, wire.TScriptPut, wire.TScriptList, wire.TScriptDelete:
		return true, wire.Errf(proto.ErrBadRequest, t+" is not supported by gtf (use tf.exec with /addworld, /undefn, /load)")
	}
	return false, nil
}

// controlMsg: in gtf the local terminal and the remote clients type into the same sessions at the
// same time ("both") and gtf's own triggers always run ("tf").
func (ss *session) controlMsg(reqID string) wire.ControlStateMsg {
	return wire.ControlStateMsg{V: proto.Version, T: wire.TControlState, ReqID: reqID, Session: ss.id,
		Input: wire.ModeBoth, Automation: wire.ModeTF}
}

func (b *backend) controlGet(p wire.Peer, reqID string, raw []byte) error {
	var req wire.ControlGetReq
	if err := json.Unmarshal(raw, &req); err != nil {
		return wire.Errf(proto.ErrBadRequest, "bad control.get")
	}
	if req.Session != "" {
		ss := b.s.session(req.Session)
		if ss == nil {
			return wire.Errf(proto.ErrNoSuchSession, "no such session")
		}
		p.Send(ss.controlMsg(reqID))
		return nil
	}
	for _, ss := range b.s.sessionList() {
		p.Send(ss.controlMsg(reqID))
	}
	return nil
}

// controlSet always answers with the real state: gtf has one model (the local terminal and every
// remote operator type into the sessions at the same time, gtf's own triggers always run), so a
// request for another mode is not an error, it just does not change anything. A client reads the
// reply to see what it got.
func (b *backend) controlSet(p wire.Peer, reqID string, raw []byte) error {
	if err := need(p, rankOperator, "changing control"); err != nil {
		return err
	}
	var req wire.ControlSetReq
	if err := json.Unmarshal(raw, &req); err != nil || req.Session == "" {
		return wire.Errf(proto.ErrBadRequest, "bad control.set")
	}
	targets := b.s.sessionList()
	if req.Session != "*" {
		ss := b.s.session(req.Session)
		if ss == nil {
			return wire.Errf(proto.ErrNoSuchSession, "no such session")
		}
		targets = targets[:0]
		targets = append(targets, ss)
	}
	for _, ss := range targets {
		p.Send(ss.controlMsg(reqID))
	}
	return nil
}

func (b *backend) worldList(p wire.Peer, reqID string) error {
	var ws []proto.TFWorld
	if err := b.hostDo(func() { ws = b.s.host.Worlds() }); err != nil {
		return err
	}
	if ws == nil {
		ws = []proto.TFWorld{}
	}
	for i := range ws {
		ws[i].MFile = "" // a macro file is code gtf loads: never shown
	}
	p.Send(wire.WorldListMsg{V: proto.Version, T: wire.TWorldList, ReqID: reqID, Worlds: ws})
	return nil
}

var pkgRE = regexp.MustCompile(`^[A-Za-z0-9_.]{1,128}$`)

func (b *backend) gmcpSend(p wire.Peer, reqID string, raw []byte) error {
	if err := need(p, rankOperator, "sending GMCP"); err != nil {
		return err
	}
	var req wire.GmcpSendReq
	if err := json.Unmarshal(raw, &req); err != nil || !pkgRE.MatchString(req.Package) {
		return wire.Errf(proto.ErrBadRequest, "bad gmcp.send")
	}
	if b.s.session(req.Session) == nil {
		return wire.Errf(proto.ErrNoSuchSession, "no such session")
	}
	data := ""
	if d := strings.TrimSpace(string(req.Data)); d != "" && d != "null" {
		if !json.Valid([]byte(d)) || len(d) > wire.MaxSendBytes {
			return wire.Errf(proto.ErrBadRequest, "gmcp data must be valid JSON")
		}
		data = d
	}
	var err error
	if e := b.hostDo(func() { err = b.s.host.SendGMCP(req.Session, req.Package, data) }); e != nil {
		return e
	}
	if err != nil {
		return wire.Errf(proto.ErrBadRequest, err.Error())
	}
	p.Send(map[string]any{"v": proto.Version, "t": "ok", "reqId": reqID})
	return nil
}

func (b *backend) msdpSend(p wire.Peer, reqID string, raw []byte) error {
	if err := need(p, rankOperator, "sending MSDP"); err != nil {
		return err
	}
	var req wire.MsdpSendReq
	if err := json.Unmarshal(raw, &req); err != nil || len(req.Vars) == 0 {
		return wire.Errf(proto.ErrBadRequest, "bad msdp.send")
	}
	if b.s.session(req.Session) == nil {
		return wire.Errf(proto.ErrNoSuchSession, "no such session")
	}
	var err error
	if e := b.hostDo(func() {
		for k, v := range req.Vars {
			if err = b.s.host.SendMSDP(req.Session, k, v); err != nil {
				return
			}
		}
	}); e != nil {
		return e
	}
	if err != nil {
		return wire.Errf(proto.ErrBadRequest, err.Error())
	}
	p.Send(map[string]any{"v": proto.Version, "t": "ok", "reqId": reqID})
	return nil
}

func (b *backend) stateGet(p wire.Peer, reqID string, raw []byte) error {
	var req wire.StateGetReq
	if err := json.Unmarshal(raw, &req); err != nil || req.Session == "" {
		return wire.Errf(proto.ErrBadRequest, "bad state.get")
	}
	ss := b.s.session(req.Session)
	if ss == nil {
		return wire.Errf(proto.ErrNoSuchSession, "no such session")
	}
	ss.mu.Lock()
	msg := ss.snapshotLocked(reqID)
	ss.mu.Unlock()
	p.Send(msg)
	return nil
}

// tfExec runs a gtf command for an admin and returns what it printed (never into a session's scrollback).
func (b *backend) tfExec(p wire.Peer, reqID string, raw []byte) error {
	if err := need(p, rankAdmin, "running commands"); err != nil {
		return err
	}
	if !b.s.Config().Commands {
		return wire.Errf(proto.ErrForbidden, "squatter_commands is off")
	}
	var req wire.ExecReq
	if err := json.Unmarshal(raw, &req); err != nil || req.Line == "" {
		return wire.Errf(proto.ErrBadRequest, "bad tf.exec")
	}
	if req.Line[0] != '/' || len(req.Line) > wire.MaxSendBytes || strings.ContainsAny(req.Line, "\x00\r\n") {
		return wire.Errf(proto.ErrBadRequest, "the line must be one command starting with /")
	}
	var ok bool
	var out []string
	if e := b.hostDo(func() { ok, out = b.s.host.Exec(req.Line) }); e != nil {
		return e
	}
	if out == nil {
		out = []string{}
	}
	for i := range out {
		out[i] = strings.ToValidUTF8(out[i], "?")
	}
	p.Send(wire.ExecResultMsg{V: proto.Version, T: wire.TExecResult, ReqID: reqID, OK: ok, Out: out})
	return nil
}
