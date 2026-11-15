// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package client

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/itsnotyoutoday/gtf/internal/client/data"
	"github.com/itsnotyoutoday/gtf/internal/handoff"
	"github.com/itsnotyoutoday/gtf/internal/layout"
	gnet "github.com/itsnotyoutoday/gtf/internal/net"
	"github.com/itsnotyoutoday/gtf/internal/net/telnet"
	"github.com/itsnotyoutoday/gtf/internal/ui"
	"github.com/itsnotyoutoday/gtfparser"
)

// The core's sections of the state file (docs/RESTART.md has the table).
const (
	secTerminal = "terminal" // v1, required
	secEngine   = "engine"   // v1, required: the interpreter's state (gtfparser SaveState)
	secUI       = "ui"       // v1, required: screens, pager, input window bookkeeping
	secSessions = "sessions" // v1, required: world connections
	secPanels   = "panels"   // v1, optional: layouts, canvases
	secClient   = "client"   // v1, optional: small client state (alert, start time)
)

// coreKnown is what this build reads of the core sections.
func coreKnown() handoff.Known {
	k := handoff.Known{secTerminal: 1, secEngine: 1, secUI: 1, secSessions: 1, secPanels: 1, secClient: 1}
	names, cs := registeredCarriers()
	for i, n := range names {
		k[n] = cs[i].Version()
	}
	return k
}

type terminalSection struct {
	Termios []byte `json:"termios,omitempty"` // the settings to restore at exit (ui.TTY.State)
	W       int    `json:"w,omitempty"`
	H       int    `json:"h,omitempty"`
}

type engineSection struct {
	State []byte `json:"state"` // gtfparser Interp.SaveState output
}

type uiSection struct {
	Base    ui.ScreenState   `json:"base"`
	Screens []ui.ScreenState `json:"screens,omitempty"` // each world's screen (Name = world), also of worlds without a socket
	UI      ui.UIState       `json:"ui"`
}

type connRec struct {
	Kind  string            `json:"kind"` // "tcp" or "tls"
	Slot  int               `json:"slot"` // the inherited descriptor
	State gnet.HandoffState `json:"state"`
	TLS   []byte            `json:"tls,omitempty"` // key material of a live TLS session: why the file is 0600
}

type sessionRec struct {
	World          string                   `json:"world"`
	State          int                      `json:"state"`
	Opts           gtfparser.ConnectOptions `json:"opts"`
	Addr           string                   `json:"addr,omitempty"`
	NumQuiet       int                      `json:"numquiet,omitempty"`
	ConnectedAt    int64                    `json:"connectedAt,omitempty"`
	LastRecv       int64                    `json:"lastRecv,omitempty"`
	LastSend       int64                    `json:"lastSend,omitempty"`
	Pend           string                   `json:"pend,omitempty"` // the line being received (a prompt not yet shown)
	Prompt         string                   `json:"prompt,omitempty"`
	Ansi           ui.AnsiParser            `json:"ansi"`
	ActiveNotified bool                     `json:"activeNotified,omitempty"`
	Store          data.State               `json:"store"`
	Conn           *connRec                 `json:"conn,omitempty"`
	Reconnect      bool                     `json:"reconnect,omitempty"` // could not be carried: the new process connects again
	EndReason      string                   `json:"endReason,omitempty"`
}

type sessionsSection struct {
	Sessions []sessionRec `json:"sessions"` // in socket-list order
	Fg       string       `json:"fg,omitempty"`
	HasFg    bool         `json:"hasFg,omitempty"`
}

type layoutRec struct {
	Name   string `json:"name"`
	Script string `json:"script"` // the commands that recreate it (/layout save's form)
}

type canvasRec struct {
	Name string `json:"name"`
	W    int    `json:"w"` // cells
	H    int    `json:"h"`
}

type panelsSection struct {
	Layouts  []layoutRec       `json:"layouts,omitempty"`
	Cur      string            `json:"cur,omitempty"`
	World    map[string]string `json:"world,omitempty"`
	Files    map[string]string `json:"files,omitempty"`
	Canvases []canvasRec       `json:"canvases,omitempty"`
}

type alertRec struct {
	Attrs  []layout.Attr `json:"attrs"`
	Line   ui.LineState  `json:"line"`
	Pos    int           `json:"pos"`
	Len    int           `json:"len"`
	Remain int64         `json:"remainMs"`
}

type clientSection struct {
	Started int64     `json:"started"`
	Alert   *alertRec `json:"alert,omitempty"`
}

func unixNano(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixNano()
}

func fromNano(n int64) time.Time {
	if n == 0 {
		return time.Time{}
	}
	return time.Unix(0, n)
}

// saveCore writes the core sections. Sockets of carried sessions are exported
// and added to w; sessions in plan.Ends are recorded for reconnection.
func (c *Client) saveCore(f *handoff.File, w *RestartWriter, plan RestartPlan) error {
	// terminal
	ts := terminalSection{W: c.ui.W, H: c.ui.H}
	if t, ok := c.cfg.Term.(interface{ State() []byte }); ok {
		ts.Termios = t.State()
	}
	if err := f.Set(secTerminal, 1, true, ts); err != nil {
		return err
	}
	// engine
	var eb bytes.Buffer
	if err := c.in.SaveState(&eb); err != nil {
		return fmt.Errorf("saving the interpreter state: %v", err)
	}
	if err := f.Set(secEngine, 1, true, engineSection{State: eb.Bytes()}); err != nil {
		return err
	}
	clear(eb.Bytes())
	// screens and UI
	us := uiSection{Base: c.base.State(), UI: c.ui.State()}
	names := make([]string, 0, len(c.wscreens))
	for n := range c.wscreens {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		st := c.wscreens[n].State()
		st.Name = n
		us.Screens = append(us.Screens, st)
	}
	if err := f.Set(secUI, 1, true, us); err != nil {
		return err
	}
	// sessions
	w.sec = secSessions
	ss := sessionsSection{}
	if c.fg != nil {
		ss.Fg, ss.HasFg = c.fg.World.Name, true
	}
	for _, s := range c.order {
		rec := sessionRec{
			World: s.World.Name, State: int(s.state), Opts: s.opts, Addr: s.addr, NumQuiet: s.numquiet,
			ConnectedAt: unixNano(s.connectedAt), LastRecv: unixNano(s.lastRecv), LastSend: unixNano(s.lastSend),
			Pend: s.pend, Prompt: s.prompt, Ansi: s.ansi, ActiveNotified: s.activeNotified, Store: s.store.State(),
		}
		if !s.dead() && !s.carried(plan) {
			rec.Reconnect = true
			for _, it := range plan.Ends {
				if it.Section == secSessions && it.Name == s.World.Name {
					rec.EndReason = it.Reason
				}
			}
			rec.State = int(gtfparser.SockResolving)
			rec.Pend = ""
		} else if s.conn != nil && s.open() {
			st, tlsBlob, file, err := s.conn.Export()
			if err != nil {
				return fmt.Errorf("%s: %v", s.World.Name, err)
			}
			kind := "tcp"
			if tlsBlob != nil {
				kind = "tls"
			}
			rec.Conn = &connRec{Kind: kind, State: st, TLS: tlsBlob}
			rec.Conn.Slot = w.AddFile(s.World.Name, file)
		}
		ss.Sessions = append(ss.Sessions, rec)
	}
	err := f.Set(secSessions, 1, true, ss)
	for i := range ss.Sessions {
		if cr := ss.Sessions[i].Conn; cr != nil {
			clear(cr.TLS) // the file holds the key material now; the old process needs it no more
		}
	}
	if err != nil {
		return err
	}
	// panels
	ps := panelsSection{Cur: c.panels.cur, World: c.panels.world, Files: c.panels.files}
	lnames := make([]string, 0, len(c.panels.layouts))
	for n := range c.panels.layouts {
		lnames = append(lnames, n)
	}
	sort.Strings(lnames)
	for _, n := range lnames {
		ps.Layouts = append(ps.Layouts, layoutRec{Name: n, Script: layoutScript(c.panels.layouts[n])})
	}
	cnames := make([]string, 0, len(c.panels.canvases))
	for n := range c.panels.canvases {
		cnames = append(cnames, n)
	}
	sort.Strings(cnames)
	for _, n := range cnames {
		cw, ch := c.panels.canvases[n].Cells()
		ps.Canvases = append(ps.Canvases, canvasRec{Name: n, W: cw, H: ch})
	}
	if err := f.Set(secPanels, 1, false, ps); err != nil {
		return err
	}
	// client
	cs := clientSection{Started: unixNano(c.started)}
	if a := c.alert; a != nil {
		attrs := []layout.Attr{{}}
		ar := &alertRec{Pos: a.pos, Len: a.len, Remain: int64(c.alertRemaining() / time.Millisecond)}
		ar.Line = ui.EncodeCells(a.cells, &attrs, map[layout.Attr]int{{}: 0})
		ar.Attrs = attrs
		cs.Alert = ar
	}
	return f.Set(secClient, 1, false, cs)
}

// ---- the new process ----

// RestoreSpec says where the handoff is: the state file and the descriptors
// the old process left open (RestoreSpecFromEnv reads them from the environment).
type RestoreSpec struct {
	Path    string
	FDs     []int // inherited descriptor numbers, in slot order
	Probe   bool  // a rehearsal: validate and adopt paused, change nothing
	ReadyFD int   // probe only: where to report (-1 = nowhere)
}

// RestoreSpecFromEnv returns the handoff described by env, or nil when this
// is a normal start.
func RestoreSpecFromEnv(env []string) (*RestoreSpec, error) {
	get := func(k string) (string, bool) {
		for _, e := range env {
			if strings.HasPrefix(e, k+"=") {
				return e[len(k)+1:], true
			}
		}
		return "", false
	}
	path, ok := get(envState)
	if !ok || path == "" {
		return nil, nil
	}
	spec := &RestoreSpec{Path: path, ReadyFD: -1}
	if v, ok := get(envFDs); ok && v != "" {
		for _, p := range strings.Split(v, ",") {
			n, err := strconv.Atoi(p)
			if err != nil || n < 3 {
				return nil, fmt.Errorf("bad %s", envFDs)
			}
			spec.FDs = append(spec.FDs, n)
		}
	}
	if v, ok := get(envReady); ok {
		n, err := strconv.Atoi(v)
		if err != nil || n < 3 {
			return nil, fmt.Errorf("bad %s", envReady)
		}
		spec.Probe, spec.ReadyFD = true, n
	}
	return spec, nil
}

// Restoration is a decoded handoff, ready for New (Config.Restore).
type Restoration struct {
	Probe  bool
	bundle *handoff.Bundle
	files  []*os.File
	term   terminalSection
}

// Termios returns the terminal settings to restore at exit (for ui.OpenTTYFrom); nil when none.
func (r *Restoration) Termios() []byte { return r.term.Termios }

// CloseFiles closes the inherited descriptors (when the restoration is abandoned).
func (r *Restoration) CloseFiles() {
	for _, f := range r.files {
		if f != nil {
			f.Close()
		}
	}
}

// LoadRestoration reads and checks the state file. The file is deleted as soon
// as it has been read (not for a probe). Nothing else is touched.
func LoadRestoration(spec RestoreSpec) (*Restoration, error) {
	b, err := handoff.Read(spec.Path, coreKnown(), !spec.Probe)
	if err != nil {
		return nil, err
	}
	r := &Restoration{Probe: spec.Probe, bundle: b}
	if len(b.File.FDs) != len(spec.FDs) {
		return nil, fmt.Errorf("the state file lists %d descriptors but %d were inherited", len(b.File.FDs), len(spec.FDs))
	}
	for i, n := range spec.FDs {
		r.files = append(r.files, os.NewFile(uintptr(n), "restart-"+b.File.FDs[i].Section+"-"+b.File.FDs[i].Item))
	}
	if err := r.section(secTerminal, &r.term); err != nil {
		return nil, err
	}
	return r, nil
}

// section decodes a section this build knows into v.
func (r *Restoration) section(name string, v any) error {
	s, ok := r.bundle.Section(name)
	if !ok {
		return fmt.Errorf("the state file has no %q section", name)
	}
	if err := json.Unmarshal(s.Data, v); err != nil {
		return fmt.Errorf("%w: section %s: %v", handoff.ErrCorrupt, name, err)
	}
	return nil
}

func (r *Restoration) optional(name string, v any) error {
	if _, ok := r.bundle.Section(name); !ok {
		return nil
	}
	return r.section(name, v)
}

// restore rebuilds the client's state from r. It runs in New, before the
// runner exists, so it only builds data; Start resumes the sockets.
func (c *Client) restore(r *Restoration) error {
	var es engineSection
	var us uiSection
	var ss sessionsSection
	var ps panelsSection
	var cl clientSection
	if err := r.section(secEngine, &es); err != nil {
		return err
	}
	if err := r.section(secUI, &us); err != nil {
		return err
	}
	if err := r.section(secSessions, &ss); err != nil {
		return err
	}
	if err := r.optional(secPanels, &ps); err != nil {
		return err
	}
	if err := r.optional(secClient, &cl); err != nil {
		return err
	}
	// the interpreter first: worlds, macros, variables, histories
	if err := c.setupLibDir(); err != nil {
		c.echoLine("% " + err.Error())
	}
	if err := c.in.LoadState(bytes.NewReader(es.State)); err != nil {
		return fmt.Errorf("the interpreter state: %w", err)
	}
	clear(es.State)
	c.setLibVars()
	// screens
	if st := us.Base; true {
		scr, err := ui.RestoreScreen(st, nil)
		if err != nil {
			return err
		}
		c.base = scr
	}
	for _, st := range us.Screens {
		name := st.Name
		keep := c.limitKeep(name)
		scr, err := ui.RestoreScreen(st, keep)
		if err != nil {
			return fmt.Errorf("screen %s: %w", name, err)
		}
		c.wscreens[name] = scr
	}
	c.ui.SetScreen(c.base)
	// sessions
	for _, rec := range ss.Sessions {
		if err := c.restoreSession(r, rec, ss.HasFg && ss.Fg == rec.World); err != nil {
			return fmt.Errorf("world %s: %w", rec.World, err)
		}
	}
	if ss.HasFg {
		c.fg = c.sessions[ss.Fg]
	}
	if c.fg != nil {
		c.ui.SetScreen(c.fg.scr)
	}
	if err := c.ui.SetState(us.UI); err != nil {
		return err
	}
	// panels
	if err := c.restorePanels(ps); err != nil {
		return err
	}
	if cl.Started != 0 {
		c.started = fromNano(cl.Started)
	}
	if a := cl.Alert; a != nil {
		cells, err := ui.DecodeCells(a.Line, a.Attrs)
		if err == nil && a.Remain > 0 {
			c.alert = &alertState{cells: cells, pos: a.Pos, len: a.Len}
			c.alertSeq++
			c.pendingAlert = time.Duration(a.Remain) * time.Millisecond
		}
	}
	// extra sections
	names, cs := registeredCarriers()
	ctx := &RestoreContext{Probe: r.Probe, files: r.files}
	for _, fd := range r.bundle.File.FDs {
		ctx.owner = append(ctx.owner, fd.Section)
	}
	for i, n := range names {
		sec, ok := r.bundle.Section(n)
		if !ok {
			continue
		}
		if err := cs[i].RestartRestore(c, ctx, sec.V, sec.Data); err != nil {
			return fmt.Errorf("%s: %w", n, err)
		}
	}
	c.restored = r
	return nil
}

// limitKeep returns the /limit filter of a world's screen, as newSession sets it.
func (c *Client) limitKeep(world string) func([]layout.Cell) bool {
	return func(cells []layout.Cell) bool { return !c.in.LimitedFor(world, ui.StripCells(cells), "") }
}

func (c *Client) restoreSession(r *Restoration, rec sessionRec, wasFg bool) error {
	w, ok := c.in.World(rec.World)
	if !ok {
		return errors.New("the world is not in the restored world database")
	}
	scr := c.wscreens[rec.World]
	s := c.newSession(w, rec.Opts, scr)
	s.state = gtfparser.SockState(rec.State)
	s.addr, s.numquiet = rec.Addr, rec.NumQuiet
	s.connectedAt, s.lastRecv, s.lastSend = fromNano(rec.ConnectedAt), fromNano(rec.LastRecv), fromNano(rec.LastSend)
	s.pend, s.prompt, s.ansi, s.activeNotified = rec.Pend, rec.Prompt, rec.Ansi, rec.ActiveNotified
	s.store = data.FromState(rec.Store)
	c.sessions[rec.World] = s
	c.order = append(c.order, s)
	if rec.Reconnect {
		c.reconnect = append(c.reconnect, reconnectRec{world: rec.World, opts: rec.Opts, wasFg: wasFg, reason: rec.EndReason})
		s.state = gtfparser.SockDead
		return nil
	}
	if rec.Conn != nil {
		cr := rec.Conn
		if err := gnet.CheckHandoff(cr.State, cr.TLS); err != nil {
			return err
		}
		if cr.Kind != "tcp" && cr.Kind != "tls" {
			return fmt.Errorf("unknown connection kind %q", cr.Kind)
		}
		f, err := (&RestoreContext{files: r.files}).File(cr.Slot)
		if err != nil {
			return err
		}
		conn, err := gnet.Adopt(cr.State, cr.TLS, f, gnet.Options{}, nil)
		clear(cr.TLS)
		if err != nil {
			return err
		}
		s.conn = conn
		st := cr.State.Env
		conn.Env().Do(func(e *telnet.EnvInfo) {
			e.TermName = strings.ToLower(envOr("TERM", "xterm"))
			e.Color256 = strings.Contains(e.TermName, "256") || c.ui.Depth >= 256
			ct := envOr("COLORTERM", "")
			e.TrueColor = strings.Contains(ct, "truecolor") || strings.Contains(ct, "24bit")
			e.ClientName, e.ClientVer = "GTF", Version
		})
		if st.Cols != c.ui.W || st.Rows != c.ui.H {
			s.resizeAfterRestore = true
		}
	}
	return nil
}

func (c *Client) restorePanels(ps panelsSection) error {
	for _, lr := range ps.Layouts {
		for _, ln := range strings.Split(lr.Script, "\n") {
			if strings.TrimSpace(ln) != "" {
				c.in.Exec(ln)
			}
		}
	}
	c.panels.cur = ps.Cur
	if ps.World != nil {
		c.panels.world = ps.World
	}
	if ps.Files != nil {
		c.panels.files = ps.Files
	}
	for _, cv := range ps.Canvases {
		c.panels.canvases[cv.Name] = layout.NewCanvas(cv.W, cv.H)
	}
	c.panels.seen = map[string]string{}
	return nil
}

type reconnectRec struct {
	world  string
	opts   gtfparser.ConnectOptions
	wasFg  bool
	reason string
}
