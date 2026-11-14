// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

// Package client wires the interpreter (the gtfparser engine), the network
// stack and the terminal UI together. Everything runs on the engine Runner's
// goroutine: key bytes, socket events, timers and resizes are posted to it,
// so the UI and the sessions need no locking.
//
// The engine owns tf's state (input buffer, key bindings, status fields,
// history, world database, /limit, /log, ...). The client implements the
// engine's optional host interfaces (WorldHost, SocketHost, MoreHost,
// LimitHost, ...), feeds it (RecordWorldLine, SetMoreState, KeyPressed) and
// draws from it (Input(), StatusRows).
package client

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/itsnotyoutoday/gtf/gtflib"
	"github.com/itsnotyoutoday/gtf/internal/layout"
	"github.com/itsnotyoutoday/gtf/internal/ui"
	"github.com/itsnotyoutoday/gtfparser"
)

// Config describes how to run the client.
type Config struct {
	Term     ui.Terminal
	Keys     io.Reader // raw key bytes (nil: no keyboard)
	Resized  <-chan struct{}
	Suspend  func() // SIGTSTP handler (nil: /suspend warns)
	LibDir   string // directory with the tf library (empty: the embedded copy)
	HomeDir  string // default os.UserHomeDir
	NoRC     bool   // -f: do not load the personal config
	NoLogin  bool   // -l
	NoAuto   bool   // -n: no autoconnect
	Quiet    bool   // -q
	Commands []string
	World    string // world (or host port) to connect at startup
	Dialer   Dialer // nil: real network
	Now      func() time.Time
	// Interactive says whether a human is at the terminal (enables visual).
	Interactive bool
}

// Client is the application.
type Client struct {
	cfg  Config
	in   *gtfparser.Interp
	run  *gtfparser.Runner
	ui   *ui.UI
	base *ui.Screen

	sessions map[string]*Session
	wscreens map[string]*ui.Screen // each world's screen outlives its socket (tf: world->screen)
	alert    *alertState
	alertSeq uint64
	order    []*Session // creation order
	fg       *Session
	xworld   string // world whose text/event is being processed

	panels *panelState
	help   *helpData
	sq     *squatState // the squatter daemon (squatter.go); inert unless squatter=on
	// squatter plumbing (squatter.go): command output capture for tf.exec, what kind of line is
	// being added to a world screen, and remote sends in progress
	capture                              *[]string
	captureWarn                          bool
	tapGame, gameLine, noTap, remoteSend bool
	lastSquatID                          string

	keyTimer      *time.Timer
	renderMu      sync.Mutex
	renderPending bool
	started       time.Time
	done          chan struct{}
	doneOnce      sync.Once
	libDir        string
	closed        bool
}

// New builds a client. The interpreter is configured and its library loaded
// (stdlib.tf and the personal config) before New returns.
func New(cfg Config) (*Client, error) {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.HomeDir == "" {
		cfg.HomeDir, _ = os.UserHomeDir()
	}
	if cfg.Dialer == nil {
		cfg.Dialer = NetDialer{}
	}
	c := &Client{cfg: cfg, sessions: map[string]*Session{}, wscreens: map[string]*ui.Screen{}, done: make(chan struct{}), started: cfg.Now()}
	c.ui = ui.New(cfg.Term)
	c.base = ui.NewScreen("", 20000)
	c.ui.SetScreen(c.base)
	c.panels = newPanelState()
	c.help = &helpData{}
	c.sq = newSquatState(c)

	in := gtfparser.New(c, gtfparser.WithoutEnvironment())
	c.in = in
	in.Warn = func(s string) {
		if c.capture != nil {
			c.captureWarn = true
		}
		c.echoLine("% " + s)
	}
	in.Input().OnChange = c.scheduleRender
	c.ui.Ed = in.Input()
	in.EnableHookEvents("GMCP", "GMCP_LOGIN", "MSDP", "SQUAT_CONNECT", "SQUAT_DISCONNECT", "SQUAT_PAIR")
	c.install()
	if err := c.loadLibrary(); err != nil {
		c.echoLine("% " + err.Error())
	}
	c.run = gtfparser.NewRunner(in)
	c.scheduleRender()
	return c, nil
}

// Interp returns the interpreter (for tests; use Do from other goroutines).
func (c *Client) Interp() *gtfparser.Interp { return c.in }

// UI returns the user interface (tests).
func (c *Client) UI() *ui.UI { return c.ui }

// Do runs f on the client's goroutine and waits.
func (c *Client) Do(f func(*gtfparser.Interp)) { c.run.Do(f) }

// Post queues f on the client's goroutine.
func (c *Client) Post(f func(*gtfparser.Interp)) {
	defer func() { recover() }() // posting to a closed runner is a no-op
	c.run.Post(f)
}

// Done is closed when the client has quit.
func (c *Client) Done() <-chan struct{} { return c.done }

// Close stops everything.
func (c *Client) Close() {
	c.doneOnce.Do(func() { close(c.done) })
	if !c.closed {
		c.closed = true
		func() { // the squatter writes status variables: stop it on the engine goroutine
			defer func() { recover() }()
			c.run.Do(func(*gtfparser.Interp) { c.sq.stop() })
		}()
		for _, s := range c.order {
			s.close()
		}
		c.run.Close()
	}
}

func (c *Client) libraryDir() (string, error) {
	if c.cfg.LibDir != "" {
		return c.cfg.LibDir, nil
	}
	return gtflib.TFCacheDir()
}

func (c *Client) loadLibrary() error {
	dir, err := c.libraryDir()
	if err != nil {
		return err
	}
	c.libDir = dir
	if c.cfg.LibDir == "" { // the library's local.tf is the user's own, normally absent: provide it empty
		if lt := filepath.Join(dir, "local.tf"); fileMissing(lt) {
			os.WriteFile(lt, nil, 0o644)
		}
	}
	c.in.LibDirs = append([]string{dir}, c.in.LibDirs...)
	c.in.SetVar("TFLIBDIR", dir)
	c.in.SetVar("TFHELP", filepath.Join(dir, "tf-help"))
	c.in.SetVar("HOME", c.cfg.HomeDir)
	c.in.SetVar("version", VersionVar())
	c.in.SetVar("TERM", envOr("TERM", "xterm"))
	c.in.Exec(fmt.Sprintf("/set wrapsize=%d", c.ui.W-1))
	if c.cfg.Interactive {
		c.in.Exec("/set visual=on")
	}
	c.banner()
	std := filepath.Join(dir, "stdlib.tf")
	if _, err := os.Stat(std); err == nil {
		c.echoLine("% Loading commands from " + std + ".")
		if err := c.in.LoadFile(std); err != nil {
			return err
		}
	}
	// gtf additions (layout.tf etc.) sit on top of the real library
	c.loadGtfLib()
	if !c.cfg.NoRC {
		c.loadRC()
	}
	return nil
}

func (c *Client) banner() {
	if c.cfg.Quiet {
		return
	}
	for _, l := range []string{
		VersionLine() + ", a TinyFugue-compatible client",
		CopyrightLine(),
		"Type `/help copyright' for more information.",
		"+gmcp +msdp +mssp +mccp +tls +layouts",
		"Type `/help', `/help topics', or `/help intro' for help.",
		"Type `/quit' to quit gtf.",
		"",
	} {
		c.echoLine(l)
	}
}

func (c *Client) loadRC() {
	for _, p := range []string{"~/.tfrc", "~/tfrc", "./.tfrc", "./tfrc"} {
		path := p
		if strings.HasPrefix(p, "~/") {
			path = filepath.Join(c.cfg.HomeDir, p[2:])
		}
		if _, err := os.Stat(path); err == nil {
			c.echoLine("% Loading commands from " + path + ".")
			if err := c.in.LoadFile(path); err != nil {
				c.echoLine("% " + err.Error())
			}
			return
		}
	}
}

// Start begins event processing (keys, resizes, startup connection and
// commands). It returns immediately; wait on Done.
func (c *Client) Start() {
	c.Post(func(in *gtfparser.Interp) {
		for _, cmd := range c.cfg.Commands {
			in.Exec(cmd)
		}
		c.squatCheck() // the rc file and -c commands have set the squatter variables by now
		if w := c.cfg.World; w != "" && !c.cfg.NoAuto {
			in.Exec("/connect " + w)
		}
		c.afterTask()
	})
	if c.cfg.Keys != nil {
		go c.readKeys()
	}
	if c.cfg.Resized != nil {
		go func() {
			for range c.cfg.Resized {
				c.Post(func(*gtfparser.Interp) { c.onResize(); c.afterTask() })
			}
		}()
	}
	go c.clockLoop()
	go c.pollLoop()
}

// pollLoop notices panel bindings changed by timer-driven script work (/repeat,
// /at, triggers fired from timers), which run on the engine's runner without
// passing through afterTask. It does nothing unless a layout is active.
func (c *Client) pollLoop() {
	t := time.NewTicker(250 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-c.done:
			return
		case <-t.C:
			c.Post(func(*gtfparser.Interp) { c.pollPanels(); c.squatCheck() })
		}
	}
}

// pollPanels re-checks the active layout's variable bindings and repaints when
// one changed.
func (c *Client) pollPanels() {
	if c.panels.current(c) == nil {
		return
	}
	c.checkLayoutDeps()
	c.scheduleRender()
}

func (c *Client) clockLoop() {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	last := ""
	for {
		select {
		case <-c.done:
			return
		case now := <-t.C:
			cur := now.Format("15:04")
			if cur != last {
				last = cur
				c.Post(func(*gtfparser.Interp) { c.scheduleRender() })
			}
		}
	}
}

func (c *Client) readKeys() {
	buf := make([]byte, 4096)
	for {
		n, err := c.cfg.Keys.Read(buf)
		if n > 0 {
			b := append([]byte(nil), buf[:n]...)
			c.Post(func(in *gtfparser.Interp) { c.onKeys(string(b)); c.afterTask() })
		}
		if err != nil {
			c.Post(func(in *gtfparser.Interp) { c.quit() })
			return
		}
	}
}

func (c *Client) onResize() {
	oldW := c.ui.W
	c.ui.Resize()
	// tf (tty.c): wrapsize follows the width
	ws := atoi(c.in.Var("wrapsize"), oldW-1)
	nw := c.ui.W - (oldW - ws)
	if nw < 1 {
		nw = max(c.ui.W-1, 1)
	}
	c.in.Exec(fmt.Sprintf("/set wrapsize=%d", nw))
	for _, s := range c.order {
		s.resize(c.ui.W, c.ui.H)
	}
	c.in.FireHook("RESIZE", fmt.Sprintf("%d %d", c.ui.W, c.ui.H))
}

func (c *Client) quit() {
	c.doneOnce.Do(func() { close(c.done) })
}

// afterTask is called at the end of every unit of work on the client
// goroutine: it notices quit, and schedules a repaint.
func (c *Client) afterTask() {
	if c.in.Quit {
		c.quit()
		return
	}
	c.squatCheck()
	c.checkLayoutDeps()
	c.scheduleRender()
}

// scheduleRender coalesces repaints: the screen is redrawn a few ms after
// the last change so bursts of output cost one repaint.
func (c *Client) scheduleRender() {
	if c.run == nil {
		return
	}
	c.renderMu.Lock()
	if c.renderPending {
		c.renderMu.Unlock()
		return
	}
	c.renderPending = true
	c.renderMu.Unlock()
	time.AfterFunc(6*time.Millisecond, func() {
		defer func() { recover() }() // the runner may have been closed meanwhile
		select {
		case <-c.done:
			return
		default:
		}
		c.run.Post(func(*gtfparser.Interp) { c.Render() })
	})
}

// Render repaints now (on the client goroutine).
func (c *Client) Render() {
	c.renderMu.Lock()
	c.renderPending = false
	c.renderMu.Unlock()
	c.syncUI()
	c.ui.Render()
}

// Flush performs any pending repaint synchronously (for tests).
func (c *Client) Flush() { c.Do(func(*gtfparser.Interp) { c.checkLayoutDeps(); c.Render() }) }

// syncUI copies interpreter state that affects the display into the UI.
func (c *Client) syncUI() {
	in := c.in
	u := c.ui
	ts := in.TermSettings()
	u.SetVisual(ts.Visual)
	if ts.Isize >= 1 {
		u.ISize = min(ts.Isize, max(u.H-3, 1))
	}
	u.StatH = min(max(ts.StatusHeight, 0), 1)
	u.Wrap.Wrap = ts.Wrap
	u.Wrap.Size = ts.WrapSize
	u.Wrap.Space = ts.WrapSpace
	u.Wrap.Punct = ts.WrapPunct
	for _, s := range c.screens() {
		s.More = ts.More
		if !ts.More && s.Paused {
			s.Flush(u.Winlines())
		}
	}
	if c.fg != nil {
		u.Hidden = !in.LocalEcho(c.fg.World.Name)
		u.Prompt = c.fg.prompt
	} else {
		u.Hidden = false
		u.Prompt = ""
	}
	cells := c.statusCells(u.W)
	c.applyAlert(cells)
	u.SetStatus(cells)
	u.Layout = c.panels.current(c)
	u.Resolver = c.panels
	c.feedMoreState()
}

func (c *Client) screens() []*ui.Screen {
	out := []*ui.Screen{c.base}
	for _, s := range c.order {
		out = append(out, s.scr)
	}
	return out
}

// feedMoreState tells the engine each world's paging state (nactive(), the
// @more status field and moresize() read it).
func (c *Client) feedMoreState() {
	for _, s := range c.order {
		st := gtfparser.MoreState{Paused: s.scr.Paused, Back: s.scr.Held(), New: s.scr.Held(), Above: max(s.scr.Vis()-c.ui.Winlines(), 0)}
		st.BackFiltered, st.NewFiltered = st.Back, st.New
		st.Active = s != c.fg && s.scr.Unseen > 0
		c.in.SetMoreState(s.World.Name, st)
	}
}

func atoi(s string, def int) int {
	n := 0
	neg := false
	if s == "" {
		return def
	}
	for i, ch := range s {
		if i == 0 && ch == '-' {
			neg = true
			continue
		}
		if ch < '0' || ch > '9' {
			return def
		}
		n = n*10 + int(ch-'0')
	}
	if neg {
		n = -n
	}
	return n
}

// statusCells renders the engine's status rows (attributes apply to field
// values only, as in tf).
func (c *Client) statusCells(w int) [][]layout.Cell {
	rows := c.in.StatusRows(w)
	out := make([][]layout.Cell, len(rows))
	for i, r := range rows {
		var base layout.Attr
		if a, err := c.in.ParseAttrs(r.Attrs); err == nil {
			base = toLayout(a, base)
		}
		cells := ui.PlainCells(r.Text, base)
		// field attributes apply to the value runes only
		col := 0
		for _, sp := range r.Spans {
			if sp.Attrs != "" && sp.ValueLen > 0 {
				if a, err := c.in.ParseAttrs(sp.Attrs); err == nil {
					for k := 0; k < sp.ValueLen && sp.Col+sp.ValueStart+k < len(cells); k++ {
						idx := sp.Col + sp.ValueStart + k
						cells[idx].A = toLayout(a, cells[idx].A)
					}
				}
			}
			col = sp.Col + sp.Width
		}
		_ = col
		out[i] = cells
	}
	return out
}

// screenFor is where output for the interpreter's current context goes.
func (c *Client) screenFor() *ui.Screen {
	if cw := c.CurrentWorld(); cw != "" {
		if s := c.sessions[cw]; s != nil {
			return s.scr
		}
	}
	if c.fg != nil {
		return c.fg.scr
	}
	return c.base
}

func (c *Client) echoLine(text string) {
	c.echoTo(c.screenFor(), "", text)
}

func (c *Client) echoTo(scr *ui.Screen, attrs, text string) {
	for _, ln := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		cells := c.attrsCells(ln, attrs)
		if c.addLine(scr, cells) {
			continue
		}
		if scr == c.base || scr == c.fgScreen() {
			c.in.RecordLocalLine(ln)
		}
	}
	c.scheduleRender()
}

func (c *Client) fgScreen() *ui.Screen {
	if c.fg != nil {
		return c.fg.scr
	}
	return c.base
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func fileMissing(p string) bool { _, err := os.Stat(p); return err != nil }
