// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package client

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/itsnotyoutoday/gtf/internal/handoff"
	"github.com/itsnotyoutoday/gtfparser"
)

// /restart: replace the running gtf with a fresh copy of its own binary while
// every game connection stays open. The design, the state file and the limits
// are in docs/RESTART.md; the short version:
//
//  1. quiesce: stop reading the keyboard and the sockets, process what is in flight;
//  2. save the state file (0600) and a duplicate of every carried socket;
//  3. rehearse: start the binary as a probe child that validates the file and
//     adopts the sockets paused, and wait for it to report ready;
//  4. only then exec the binary in place (same pid, same terminal, so job
//     control keeps working); the new process reads the file, repaints and
//     resumes the sockets.
//
// A failure before the exec (the binary is missing, exits, hangs, refuses the
// file) rolls everything back: readers resume and the session carries on.

// RestartItem is one thing a restart would carry or end.
type RestartItem struct {
	Section string // "sessions" or a carrier's section name
	Name    string // the world, or the carrier's item
	Kind    string // "tcp", "tls", "closed" (a dead socket's screen), ...
	Reason  string // for items that end: why
}

// RestartPlan says what a restart would do with the sessions.
type RestartPlan struct {
	Supported bool // false: Why says why a restart cannot be done at all
	Why       string
	Carried   []RestartItem
	Ends      []RestartItem // sessions that cannot be carried; Restart refuses unless forced
}

// String renders the plan the way /restart prints it.
func (p RestartPlan) String() string {
	var b strings.Builder
	if !p.Supported {
		return "restart is not possible: " + p.Why
	}
	for _, it := range p.Carried {
		fmt.Fprintf(&b, "carried: %s (%s)\n", it.Name, it.Kind)
	}
	for _, it := range p.Ends {
		fmt.Fprintf(&b, "ends:    %s (%s)\n", it.Name, it.Reason)
	}
	if b.Len() == 0 {
		b.WriteString("nothing to carry\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// RestartRefused is returned by Restart when sessions would end and force is false.
type RestartRefused struct{ Plan RestartPlan }

func (e *RestartRefused) Error() string {
	var names []string
	for _, it := range e.Plan.Ends {
		names = append(names, it.Name+" ("+it.Reason+")")
	}
	return "these connections would end: " + strings.Join(names, ", ") + "; use /restart -f to restart anyway (they are closed and reconnected)"
}

// RestartPlan reports which sessions a restart would carry and which would
// end, with reasons. It changes nothing. Call it on the client goroutine.
func (c *Client) RestartPlan() RestartPlan {
	p := RestartPlan{Supported: true}
	if ok, why := restartSupported(); !ok {
		return RestartPlan{Why: why}
	}
	if _, _, err := c.restartExe(); err != nil {
		return RestartPlan{Why: err.Error()}
	}
	for _, s := range c.order {
		it := RestartItem{Section: secSessions, Name: s.World.Name}
		switch {
		case s.dead():
			it.Kind = "closed"
			p.Carried = append(p.Carried, it)
		case s.state != gtfparser.SockConnected || s.conn == nil:
			it.Reason = "connection still being established"
			p.Ends = append(p.Ends, it)
		default:
			kind, why := s.conn.HandoffKind()
			if kind == "" {
				it.Reason = why
				p.Ends = append(p.Ends, it)
			} else {
				it.Kind = kind
				p.Carried = append(p.Carried, it)
			}
		}
	}
	_, cs := registeredCarriers()
	names, _ := registeredCarriers()
	for i, cr := range cs {
		car, ends := cr.RestartPlan(c)
		for _, it := range car {
			it.Section = names[i]
			p.Carried = append(p.Carried, it)
		}
		for _, it := range ends {
			it.Section = names[i]
			p.Ends = append(p.Ends, it)
		}
	}
	return p
}

// restartExe is the binary to start and the argument vector to give it.
func (c *Client) restartExe() (string, []string, error) {
	exe := c.cfg.ExePath
	if exe == "" {
		e, err := os.Executable()
		if err != nil {
			return "", nil, fmt.Errorf("cannot find the running binary: %v", err)
		}
		exe = e
	}
	exe = strings.TrimSuffix(exe, " (deleted)") // Linux: the binary was replaced on disk
	fi, err := os.Stat(exe)
	if err != nil {
		return "", nil, fmt.Errorf("cannot restart: %v", err)
	}
	if fi.IsDir() || fi.Mode().Perm()&0o111 == 0 {
		return "", nil, fmt.Errorf("cannot restart: %s is not an executable file", exe)
	}
	argv := c.cfg.Args
	if len(argv) == 0 {
		argv = []string{exe}
	}
	return exe, argv, nil
}

func (c *Client) restartTimeout() time.Duration {
	if v := os.Getenv("GTF_RESTART_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			return d
		}
		var secs float64
		if _, err := fmt.Sscanf(v, "%g", &secs); err == nil && secs > 0 {
			return time.Duration(secs * float64(time.Second))
		}
	}
	if c.cfg.RestartTimeout > 0 {
		return c.cfg.RestartTimeout
	}
	return 10 * time.Second
}

func (c *Client) restartStatePath() string {
	return filepath.Join(c.cfg.HomeDir, ".gtf", fmt.Sprintf("restart-%d.state", os.Getpid()))
}

// undo is a list of rollback actions run in reverse.
type undo []func()

func (u *undo) add(f func()) { *u = append(*u, f) }
func (u undo) run() {
	for i := len(u) - 1; i >= 0; i-- {
		u[i]()
	}
}

// Restart replaces this process with a fresh copy of its own binary, keeping
// the game connections open (docs/RESTART.md). On success it does not return.
// It returns *RestartRefused when sessions would end and force is false; any
// other error means the restart was abandoned and this process carries on with
// everything as it was. Call it on the client goroutine (from a command, or
// inside Do or Post): it blocks that goroutine until the new process has
// reported ready, at most about ten seconds.
func (c *Client) Restart(force bool) (err error) {
	plan := c.RestartPlan()
	if !plan.Supported {
		return errors.New(plan.Why)
	}
	if c.restarting {
		return errors.New("a restart is already in progress")
	}
	if len(plan.Ends) > 0 && !force {
		return &RestartRefused{plan}
	}
	exe, argv, err := c.restartExe()
	if err != nil {
		return err
	}
	c.restarting = true
	var rb undo
	var files []*os.File // descriptors handed over; kept referenced until the exec
	var statePath string
	defer func() {
		if err != nil {
			rb.run()
			if statePath != "" {
				os.Remove(statePath)
			}
			for _, f := range files {
				f.Close()
			}
			c.restarting = false
			c.scheduleRender()
		}
	}()

	// 1. quiesce: no more keys, no more socket reads, in-flight events processed
	if c.keyTimer != nil {
		c.keyTimer.Stop()
		c.keyTimer = nil
	}
	c.in.FlushKeys() // a lone ESC held back as a possible prefix becomes the key it is
	if p, ok := c.cfg.Keys.(keyPauser); ok {
		if p.Pause(2 * time.Second) {
			rb.add(p.Resume)
		}
	}
	for _, s := range c.order {
		if s.conn == nil || !s.open() || !s.carried(plan) {
			continue
		}
		if err = s.conn.Pause(); err != nil {
			return fmt.Errorf("%s: %v", s.World.Name, err)
		}
		sess := s
		rb.add(func() { sess.conn.Resume() })
		select {
		case <-s.pausedCh:
		case <-time.After(3 * time.Second):
			return fmt.Errorf("%s: the connection did not stop in time", s.World.Name)
		}
		c.drain(s) // what the reader had delivered before it stopped
		if s.pendTimer != nil {
			s.pendTimer.Stop()
			s.pendTimer = nil
		}
		rb.add(func() { sess.rearmPartial() })
	}
	// what was processed may have ended a connection: look again
	plan2 := c.RestartPlan()
	if len(plan2.Ends) > len(plan.Ends) && !force {
		return &RestartRefused{plan2}
	}
	plan = plan2
	names, cs := registeredCarriers()
	for i, cr := range cs {
		if err = cr.RestartQuiesce(c); err != nil {
			return fmt.Errorf("%s: %v", names[i], err)
		}
		cr := cr
		rb.add(func() { cr.RestartRollback(c) })
	}

	// 2. save
	f := handoff.New(VersionLine(), c.cfg.Now().Unix())
	w := &RestartWriter{}
	if err = c.saveCore(f, w, plan); err != nil {
		return err
	}
	for i, cr := range cs {
		w.sec = names[i]
		data, serr := cr.RestartSave(c, w)
		if serr != nil {
			err = fmt.Errorf("%s: %v", names[i], serr)
			files = append(files, w.files...)
			return err
		}
		if serr := f.Set(names[i], cr.Version(), cr.Required(), data); serr != nil {
			files = append(files, w.files...)
			return serr
		}
	}
	files = append(files, w.files...)
	for _, o := range w.owner {
		sec, item, _ := strings.Cut(o, "/")
		f.FDs = append(f.FDs, handoff.FD{Section: sec, Item: item})
	}
	statePath = c.restartStatePath()
	if err = handoff.Write(statePath, f); err != nil {
		return fmt.Errorf("writing the state file: %v", err)
	}

	// 3. rehearse in a child process
	req := ProbeRequest{Exe: exe, Argv: argv, Env: os.Environ(), StatePath: statePath, Files: files, Timeout: c.restartTimeout()}
	probe := c.cfg.RestartProbe
	if probe == nil {
		probe = spawnProbe
	}
	if err = probe(req); err != nil {
		return fmt.Errorf("the new binary was not ready: %v", err)
	}

	// 4. commit: end what could not be carried, hand over the descriptors, exec
	for _, s := range c.order {
		if s.conn != nil && s.open() && !s.carried(plan) {
			s.close() // a clean close (TLS close_notify); the new process reconnects it
		}
		if s.cancel != nil && !s.open() && !s.dead() {
			s.cancel()
		}
	}
	execFn := c.cfg.RestartExec
	if execFn == nil {
		execFn = execBinary
	}
	env := restartEnv(os.Environ(), statePath, files)
	if err = prepareHandoverFiles(files); err != nil {
		return err
	}
	err = execFn(exe, argv, env)
	if errors.Is(err, ErrExecDone) {
		// only a test hook says this: the "new process" has taken over. A real exec closes the old
		// process's descriptors (they are close-on-exec; the new process has its own copies): do the same,
		// so that a connection the new process does not adopt really ends.
		for _, f := range files {
			f.Close()
		}
		return nil
	}
	// exec returns only when it failed
	undoHandoverFiles(files)
	for _, s := range c.order {
		if s.conn != nil && s.open() && !s.carried(plan) {
			c.zombie(s) // it was closed above
		}
	}
	if err == nil {
		err = errors.New("exec returned without error")
	}
	return fmt.Errorf("exec %s: %v", exe, err)
}

// carried reports whether the session is carried by the plan p (not in p.Ends).
func (s *Session) carried(p RestartPlan) bool {
	for _, it := range p.Ends {
		if it.Section == secSessions && it.Name == s.World.Name {
			return false
		}
	}
	return true
}

// rearmPartial restarts the timer that turns a partial line into a prompt
// (after a rollback, or in a new process).
func (s *Session) rearmPartial() {
	if s.pend == "" || s.pendTimer != nil {
		return
	}
	c := s.c
	s.pendTimer = time.AfterFunc(250*time.Millisecond, func() {
		c.Post(func(*gtfparser.Interp) {
			if s.pend != "" {
				s.promptEnd()
				c.afterTask()
			}
		})
	})
}

// keyPauser is implemented by a key reader that can be parked without losing a key (ui.TTY).
type keyPauser interface {
	Pause(time.Duration) bool
	Resume()
}

// ProbeRequest describes the rehearsal of a restart: start Exe with Argv, give
// it the state file and the descriptors (slot order) and wait until it reports
// that it could restore everything, or fail.
type ProbeRequest struct {
	Exe       string
	Argv      []string
	Env       []string
	StatePath string
	Files     []*os.File
	Timeout   time.Duration
}

// cmdRestart is /restart [-f] [-n].
func (c *Client) cmdRestart(in *gtfparser.Interp, args string) gtfparser.Value {
	force, planOnly := false, false
	for _, a := range strings.Fields(args) {
		switch a {
		case "-f":
			force = true
		case "-n":
			planOnly = true
		default:
			in.Warn("/restart: usage: /restart [-f] [-n]   (-f: restart even if connections would end, -n: only show what would happen)")
			return gtfparser.Int(0)
		}
	}
	plan := c.RestartPlan()
	if planOnly {
		for _, ln := range strings.Split(plan.String(), "\n") {
			in.Host.Echo("% /restart: " + ln)
		}
		return gtfparser.Int(1)
	}
	if err := c.Restart(force); err != nil {
		var ref *RestartRefused
		if errors.As(err, &ref) {
			in.Host.Echo("% /restart: refused: " + err.Error())
			for _, it := range ref.Plan.Ends {
				in.Host.Echo("%   would end: " + it.Name + ": " + it.Reason)
			}
			return gtfparser.Int(0)
		}
		in.Host.Echo("% /restart: " + err.Error() + "; gtf keeps running")
		return gtfparser.Int(0)
	}
	return gtfparser.Int(1)
}

// ErrExecDone is what a Config.RestartExec hook returns to say "the new
// process has taken over" (the real exec never returns).
var ErrExecDone = errors.New("restart: exec done")
