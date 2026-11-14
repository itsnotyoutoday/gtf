// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package client

// squatter_gtf.go: the gtf extension's client-side half (docs/SWP-GTF.md): world edits from peers,
// persistence of the world file, the NAWS width, and the local terminal's view and typing state.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/itsnotyoutoday/gtf/internal/squatter"
	"github.com/itsnotyoutoday/gtf/internal/squatter/proto"
	"github.com/itsnotyoutoday/gtfparser"
)

// nawsCols is the width to tell the game of s: gtf's own terminal, or what squatter_naws decides.
func (c *Client) nawsCols(s *Session) int {
	if c.sq != nil && c.sq.active() && s.squatID != "" {
		return c.sq.sq.SessionCols(s.squatID, c.ui.W)
	}
	return c.ui.W
}

// squatLocalView reports the foreground world to the squatter (presence).
func (c *Client) squatLocalView() {
	if !c.sq.active() {
		return
	}
	id := ""
	if c.fg != nil {
		id = c.fg.squatID
	}
	if id != c.sq.lastView {
		c.sq.lastView = id
		c.sq.sq.LocalView(id)
	}
}

// squatLocalInput: the local user submitted a line.
func (c *Client) squatLocalInput() {
	if c.sq != nil && c.sq.active() {
		c.sq.sq.LocalInput()
	}
}

// squatLocalTyping reports whether the local user has text in the input line (not a hidden one: a
// password is not an activity to announce).
func (c *Client) squatLocalTyping() {
	if c.sq == nil || !c.sq.active() {
		return
	}
	id := ""
	if c.fg != nil {
		id = c.fg.squatID
	}
	on := id != "" && !c.ui.Hidden && c.in.Input().Text() != ""
	c.sq.sq.LocalTyping(id, on)
}

// squatWorldEvent forwards the engine's world events to the squatter (any origin: typed commands,
// scripts, or a peer's edit, which sets worldAuthor).
func (c *Client) squatWorldEvent(e gtfparser.Event) {
	if c.sq == nil || !c.sq.active() || len(e.Args) < 2 {
		return
	}
	op, name, old := e.Args[0], e.Args[1], ""
	if e.Kind == "renameworld" {
		op, old, name = "rename", e.Args[0], e.Args[1]
	}
	var rec *proto.TFWorld
	if op != "remove" {
		if w, ok := c.in.World(name); ok {
			r := tfWorld(w)
			rec = &r
		}
	}
	c.sq.sq.WorldEvent(op, name, old, c.worldAuthor, rec)
}

// EditWorld applies a peer's world edit (engine goroutine). The check of the base revision and the
// change happen in this one call, so no other edit can slip in between.
func (h *squatHost) EditWorld(e squatter.WorldEdit) (squatter.WorldResult, error) {
	c := h.c
	c.worldAuthor = e.Author
	defer func() { c.worldAuthor = "" }()
	res, err := squatter.ApplyWorldEdit(engineWorlds{c.in}, e)
	if err != nil {
		return res, err
	}
	if c.sq.sq.Config().WorldSave {
		res.Saved, res.SaveError = c.saveWorlds()
	}
	return res, nil
}

// engineWorlds is the engine's world database as a squatter.WorldDB.
type engineWorlds struct{ in *gtfparser.Interp }

func (d engineWorlds) Get(name string) (proto.TFWorld, bool) {
	w, ok := d.in.World(name)
	if !ok {
		return proto.TFWorld{}, false
	}
	return tfWorld(w), true
}

func (d engineWorlds) Put(w proto.TFWorld, set bool, password string) (proto.TFWorld, error) {
	nw := gtfparser.WorldInfo{Name: w.Name, Type: w.Type, Host: w.Host, Port: w.Port, Character: w.Char,
		SSL: w.TLS, Echo: w.Echo, Charset: w.Charset}
	if cur, ok := d.in.World(w.Name); ok {
		nw.NoProxy, nw.SrcHost, nw.MFile, nw.Password = cur.NoProxy, cur.SrcHost, cur.MFile, cur.Password
	}
	if set {
		nw.Password = password
	}
	rec, err := d.in.PutWorld(nw)
	if err != nil {
		return proto.TFWorld{}, err
	}
	return tfWorld(rec), nil
}

func (d engineWorlds) Delete(name string) error { return mapWorldErr(d.in.DeleteWorld(name)) }

func (d engineWorlds) Rename(old, nw string) (proto.TFWorld, error) {
	rec, err := d.in.RenameWorld(old, nw)
	if err != nil {
		return proto.TFWorld{}, mapWorldErr(err)
	}
	return tfWorld(rec), nil
}

func mapWorldErr(err error) error {
	switch {
	case errors.Is(err, gtfparser.ErrWorldNotFound):
		return squatter.ErrWorldMissing
	case errors.Is(err, gtfparser.ErrWorldExists):
		return squatter.ErrWorldExists
	case errors.Is(err, gtfparser.ErrWorldInUse):
		return squatter.ErrWorldBusy
	}
	return err
}

// saveWorlds rewrites the world file the way /saveworld would (the WORLDFILE macro's file, all
// worlds, mode 0600), atomically, and keeps a backup of whatever was there that gtf did not write
// itself (a hand-edited file). It returns whether it saved and, if not, a short reason (no paths).
func (c *Client) saveWorlds() (bool, string) {
	if c.in.Restriction() >= gtfparser.RestrictFile {
		return false, "file access is restricted"
	}
	path, ok := c.in.WorldFile()
	if !ok {
		return false, "no WORLDFILE is defined"
	}
	lines := c.in.WorldDefinitions()
	content := strings.Join(lines, "\n")
	if content != "" {
		content += "\n"
	}
	backup, err := writeWorldFile(path, content, &c.sq.worldSum)
	if backup != "" {
		c.privateLine("% squatter: world file " + path + " was not written by gtf; its previous contents are kept in " + backup)
	}
	if err != nil {
		c.privateLine("% squatter: could not save " + path + ": " + err.Error())
		return false, "the world file could not be written"
	}
	return true, ""
}

// writeWorldFile replaces path with content (mode 0600, via a temporary file and rename, following a
// symlink). When the file exists and is neither identical nor exactly what this process wrote last
// (*last), it is copied to path.gtf-bak-<time> first (the 5 newest backups are kept).
func writeWorldFile(path, content string, last *string) (backup string, err error) {
	if p, e := filepath.EvalSymlinks(path); e == nil {
		path = p
	}
	if old, e := os.ReadFile(path); e == nil && string(old) != content && (last == nil || string(old) != *last) {
		backup = fmt.Sprintf("%s.gtf-bak-%s", path, time.Now().Format("20060102-150405"))
		for i := 1; ; i++ {
			if _, e := os.Stat(backup); e != nil {
				break
			}
			backup = fmt.Sprintf("%s.gtf-bak-%s-%d", path, time.Now().Format("20060102-150405"), i)
		}
		if err := os.WriteFile(backup, old, 0o600); err != nil {
			return "", fmt.Errorf("cannot back up the old file: %w", err)
		}
		pruneBackups(path, 5)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".gtf-world-*")
	if err != nil {
		return backup, err
	}
	name := tmp.Name()
	_, werr := tmp.WriteString(content)
	cerr := tmp.Close()
	if werr != nil || cerr != nil {
		os.Remove(name)
		return backup, errors.Join(werr, cerr)
	}
	if err := os.Chmod(name, 0o600); err != nil {
		os.Remove(name)
		return backup, err
	}
	if err := os.Rename(name, path); err != nil {
		os.Remove(name)
		return backup, err
	}
	if last != nil {
		*last = content
	}
	return backup, nil
}

func pruneBackups(path string, keep int) {
	m, _ := filepath.Glob(path + ".gtf-bak-*")
	sort.Strings(m)
	for len(m) > keep {
		os.Remove(m[0])
		m = m[1:]
	}
}
