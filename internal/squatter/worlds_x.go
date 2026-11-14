// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package squatter

// World database edits from peers (gtf extension, feature "worlds"): world.put, world.delete,
// world.rename with optimistic concurrency, and the world.changed broadcast.

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/itsnotyoutoday/gtf/internal/squatter/proto"
	"github.com/itsnotyoutoday/gtf/internal/squatter/wire"
)

// WorldEdit is one edit a peer asked for; Host.EditWorld applies it on the engine goroutine.
type WorldEdit struct {
	Op            string // "put", "delete" or "rename"
	World         proto.TFWorld
	Password      string // write-only; kept unless non-empty (set) or ClearPassword
	ClearPassword bool
	Name, NewName string // delete and rename
	BaseRev       uint64 // the revision the peer based its edit on (0 = "the world does not exist yet")
	Author        string // the device name, for the audit line
}

// WorldResult is what Host.EditWorld reports.
type WorldResult struct {
	World     proto.TFWorld // the stored record (no password, no mfile), with its new Rev; zero for delete
	Created   bool
	OldName   string   // rename
	Changed   []string // field names that changed (never values), for the audit line
	Saved     bool     // the world file was rewritten
	SaveError string   // why it was not (short, no paths)
}

// WorldConflict is returned by Host.EditWorld when BaseRev is not the world's current revision;
// Current is the world as it is now (nil when it no longer exists).
type WorldConflict struct{ Current *proto.TFWorld }

func (*WorldConflict) Error() string {
	return "the world changed since the revision the edit is based on"
}

// Errors Host.EditWorld returns for the other refusals.
var (
	ErrWorldMissing = errors.New("no such world")
	ErrWorldExists  = errors.New("a world with that name exists")
	ErrWorldBusy    = errors.New("the world is connected")
)

// WorldDB is the world database as ApplyWorldEdit needs it. Revisions must change on every change
// and never be reused (the engine's World.Rev does this).
type WorldDB interface {
	// Get returns the world (with Rev) or ok=false.
	Get(name string) (proto.TFWorld, bool)
	// Put creates the world or replaces every wire field of the existing one; fields the wire form
	// does not carry (macro file, source address, proxy flag) are kept. The password is kept unless
	// setPassword, in which case it becomes password ("" clears it).
	Put(w proto.TFWorld, setPassword bool, password string) (proto.TFWorld, error)
	Delete(name string) error
	Rename(old, newName string) (proto.TFWorld, error)
}

// ApplyWorldEdit runs one edit against db with optimistic concurrency: the edit applies only when
// e.BaseRev is the world's current revision (0 for a world that does not exist yet); otherwise it
// returns a *WorldConflict carrying the current record. The caller makes the call atomic (it runs on
// the engine goroutine) and does the persistence.
func ApplyWorldEdit(db WorldDB, e WorldEdit) (WorldResult, error) {
	var res WorldResult
	cur, exists := db.Get(e.Name)
	conflict := func() error {
		var cp *proto.TFWorld
		if exists {
			c := cur
			cp = &c
		}
		return &WorldConflict{Current: cp}
	}
	switch e.Op {
	case "put":
		if exists != (e.BaseRev != 0) || exists && e.BaseRev != cur.Rev {
			return res, conflict()
		}
		set := e.ClearPassword || e.Password != ""
		pw := e.Password
		if e.ClearPassword {
			pw = ""
		}
		rec, err := db.Put(e.World, set, pw)
		if err != nil {
			return res, err
		}
		rec.MFile = ""
		res.World, res.Created = rec, !exists
		res.Changed = changedFields(cur, rec, exists)
		if set && (pw != "" || exists && cur.PassSet) {
			res.Changed = append(res.Changed, "password")
		}
	case "delete":
		if !exists {
			return res, ErrWorldMissing
		}
		if e.BaseRev != cur.Rev {
			return res, conflict()
		}
		if err := db.Delete(e.Name); err != nil {
			return res, err
		}
	case "rename":
		if !exists {
			return res, ErrWorldMissing
		}
		if e.BaseRev != cur.Rev {
			return res, conflict()
		}
		rec, err := db.Rename(e.Name, e.NewName)
		if err != nil {
			return res, err
		}
		rec.MFile = ""
		res.World, res.OldName = rec, e.Name
	default:
		return res, errors.New("unknown edit")
	}
	return res, nil
}

func changedFields(old, nw proto.TFWorld, existed bool) []string {
	var ch []string
	add := func(diff bool, n string) {
		if diff || !existed {
			ch = append(ch, n)
		}
	}
	add(old.Host != nw.Host, "host")
	add(old.Port != nw.Port, "port")
	add(old.Type != nw.Type, "type")
	add(old.Char != nw.Char, "char")
	add(old.TLS != nw.TLS, "tls")
	add(old.Echo != nw.Echo, "echo")
	add(old.Charset != nw.Charset, "charset")
	return ch
}

var (
	worldNameRE = regexp.MustCompile(`^[^\s()"'\\/;%][^\s"'\;%]{0,63}$`)
	hostRE      = regexp.MustCompile(`^[A-Za-z0-9._:\[\]%-]{1,253}$`)
	svcRE       = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9-]{0,31}$`)
	wordRE      = regexp.MustCompile(`^[A-Za-z0-9_.+-]{0,32}$`)
)

func hasControl(s string) bool {
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}

// ValidateWorldName checks a world name a peer may create or rename to.
func ValidateWorldName(n string) error {
	if !worldNameRE.MatchString(n) || strings.EqualFold(n, "default") || !utf8.ValidString(n) {
		return fmt.Errorf("bad world name (1-64 characters, no spaces, quotes, ; %% \\ or leading parenthesis or slash, not \"default\")")
	}
	return nil
}

// ValidateWorld checks a record a peer wants to store (not the password: see validatePassword).
func ValidateWorld(w proto.TFWorld) error {
	if err := ValidateWorldName(w.Name); err != nil {
		return err
	}
	if (w.Host == "") != (w.Port == "") {
		return errors.New("host and port must be both blank or both set")
	}
	if w.Host != "" {
		if !hostRE.MatchString(w.Host) {
			return errors.New("bad host name")
		}
		if n, err := strconv.Atoi(w.Port); err == nil {
			if n < 1 || n > 65535 {
				return errors.New("port must be 1-65535")
			}
		} else if !svcRE.MatchString(w.Port) {
			return errors.New("port must be a number or a service name")
		}
	}
	if !wordRE.MatchString(w.Type) {
		return errors.New("bad world type")
	}
	if !wordRE.MatchString(w.Charset) {
		return errors.New("bad charset")
	}
	if len(w.Char) > 64 || hasControl(w.Char) || !utf8.ValidString(w.Char) || strings.ContainsAny(w.Char, "\"\\") {
		return errors.New("bad character name")
	}
	if w.MFile != "" {
		return errors.New("a peer cannot set a macro file")
	}
	return nil
}

func validatePassword(p string) error {
	if len(p) > 256 || hasControl(p) || !utf8.ValidString(p) || strings.ContainsAny(p, "\"\\") {
		return errors.New("bad password (up to 256 characters, no control characters, quotes or backslashes)")
	}
	return nil
}

type worldPutX struct {
	ID            string        `json:"id"`
	World         proto.TFWorld `json:"world"`
	Password      string        `json:"password"`
	ClearPassword bool          `json:"clearPassword"`
	SetMFile      bool          `json:"setMfile"`
	BaseRev       uint64        `json:"baseRev"`
}

type worldDelX struct {
	ID      string  `json:"id"`
	Name    string  `json:"name"`
	NewName string  `json:"newName"`
	BaseRev *uint64 `json:"baseRev"`
}

// handleWorldEdit serves world.put/delete/rename for a gtf peer with the worlds feature.
func (b *backend) handleWorldEdit(p wire.Peer, g *gtfPeer, t, reqID string, raw []byte) error {
	s := b.s
	if err := need(p, rankAdmin, "editing worlds"); err != nil {
		return err
	}
	s.pmu.Lock()
	ok := g.allow("world")
	s.pmu.Unlock()
	if !ok {
		return wire.Errf(proto.ErrRateLimited, "too many world edits")
	}
	var e WorldEdit
	e.Author = p.DeviceName()
	switch t {
	case wire.TWorldPut:
		var r worldPutX
		if json.Unmarshal(raw, &r) != nil {
			return wire.Errf(proto.ErrBadRequest, "bad world.put")
		}
		if r.SetMFile {
			return wire.Errf(proto.ErrBadRequest, "a peer cannot set a macro file")
		}
		r.World.PassSet, r.World.Rev, r.World.Temp = false, 0, false
		if err := ValidateWorld(r.World); err != nil {
			return wire.Errf(proto.ErrBadRequest, err.Error())
		}
		if err := validatePassword(r.Password); err != nil {
			return wire.Errf(proto.ErrBadRequest, err.Error())
		}
		e.Op, e.World, e.Password, e.ClearPassword, e.BaseRev = "put", r.World, r.Password, r.ClearPassword, r.BaseRev
		e.Name = r.World.Name
	case wire.TWorldDelete, wire.TWorldRename:
		var r worldDelX
		if json.Unmarshal(raw, &r) != nil || r.Name == "" || len(r.Name) > 128 {
			return wire.Errf(proto.ErrBadRequest, "bad "+t)
		}
		if r.BaseRev == nil {
			return wire.Errf(proto.ErrBadRequest, t+" needs baseRev")
		}
		e.Op, e.Name, e.BaseRev = "delete", r.Name, *r.BaseRev
		if t == wire.TWorldRename {
			if err := ValidateWorldName(r.NewName); err != nil {
				return wire.Errf(proto.ErrBadRequest, err.Error())
			}
			e.Op, e.NewName = "rename", r.NewName
		}
	}
	var res WorldResult
	var err error
	if he := b.hostDo(func() { res, err = s.host.EditWorld(e) }); he != nil {
		return he
	}
	if err != nil {
		var wc *WorldConflict
		switch {
		case errors.As(err, &wc):
			var data json.RawMessage
			if wc.Current != nil {
				cur := *wc.Current
				cur.MFile = ""
				data, _ = json.Marshal(map[string]any{"current": cur})
			} else {
				data = json.RawMessage(`{"current":null}`)
			}
			pe := wire.Errf(ErrConflict, wc.Error())
			pe.Data = data
			return pe
		case errors.Is(err, ErrWorldMissing):
			return wire.Errf(ErrNotFound, "no such world")
		case errors.Is(err, ErrWorldExists):
			return wire.Errf(ErrExists, "a world with that name exists")
		case errors.Is(err, ErrWorldBusy):
			return wire.Errf(proto.ErrBusy, "the world is connected")
		}
		return wire.Errf(proto.ErrBadRequest, err.Error())
	}
	res.World.MFile = ""
	p.Send(map[string]any{"v": proto.Version, "t": "world.result", "reqId": reqID, "op": e.Op, "created": res.Created,
		"oldName": res.OldName, "world": worldOrNil(e.Op, res.World), "saved": res.Saved, "saveError": res.SaveError})
	s.auditWorld(p, e, res)
	return nil
}

func worldOrNil(op string, w proto.TFWorld) any {
	if op == "delete" {
		return nil
	}
	return w
}

// auditWorld prints one line on gtf's screen for an administrative change by a peer: who and what,
// never a password or any value.
func (s *Squatter) auditWorld(p wire.Peer, e WorldEdit, res WorldResult) {
	who := p.DeviceName() + " (" + roleName(p) + ")"
	var what string
	switch e.Op {
	case "put":
		verb := "updated"
		if res.Created {
			verb = "added"
		}
		what = fmt.Sprintf("%s world %s", verb, res.World.Name)
		if len(res.Changed) > 0 {
			what += " [" + strings.Join(res.Changed, ",") + "]"
		}
	case "delete":
		what = "removed world " + e.Name
	case "rename":
		what = fmt.Sprintf("renamed world %s to %s", e.Name, e.NewName)
	}
	save := ""
	switch {
	case res.Saved:
		save = ", world file saved"
	case res.SaveError != "":
		save = ", world file NOT saved: " + res.SaveError
	}
	line := "% squatter: " + who + " " + what + save
	s.host.Post(func() { s.host.Notice(line) })
}

// WorldEvent tells the squatter a world changed (the engine's "world" and "renameworld" events,
// forwarded by the client): it is broadcast as world.changed to every peer with the worlds feature.
// op is "add", "update", "remove" or "rename"; for a rename name is the new name. rec is the
// record after the change (nil for remove); author is the device name or "" for gtf itself.
func (s *Squatter) WorldEvent(op, name, oldName, author string, rec *proto.TFWorld) {
	if !s.active.Load() {
		return
	}
	if author == "" {
		author = "local"
	}
	msg := map[string]any{"v": proto.Version, "t": wire.TWorldChanged, "name": name, "author": author, "ts": nowMs()}
	switch op {
	case "add", "update":
		msg["op"] = "put"
		if op == "add" {
			msg["created"] = true
		}
	case "remove":
		msg["op"], msg["deleted"] = "delete", true
	case "rename":
		msg["op"], msg["oldName"] = "rename", oldName
	}
	if rec != nil {
		r := *rec
		r.MFile = ""
		msg["world"], msg["rev"] = r, r.Rev
	}
	for _, p := range s.presenceTargets(FeatWorlds, 0) {
		p.Send(msg)
	}
}
