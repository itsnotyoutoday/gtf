// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

//go:build darwin || dragonfly || freebsd || netbsd || openbsd || linux

package client

import (
	"bytes"
	"compress/zlib"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/itsnotyoutoday/gtf/internal/net/nettest"
	"github.com/itsnotyoutoday/gtf/internal/net/telnet"
	"github.com/itsnotyoutoday/gtf/internal/ui"
	"github.com/itsnotyoutoday/gtf/internal/ui/vt"
	"github.com/itsnotyoutoday/gtfparser"
)

// rig runs an "old" client and, after a restart, the "new" one on the same
// virtual terminal. The exec is replaced by a hook that builds the new client
// from the state file and the inherited descriptors (duplicated, as the kernel
// would keep them) and abandons the old one: the new process taking the same
// tty is the shared vt.Screen.
type rig struct {
	t      *testing.T
	scr    *vt.Screen
	d      *scriptDialer
	home   string
	a      *Client
	mu     sync.Mutex
	b      *Client
	probe  func(ProbeRequest) error // nil: the in-process rehearsal
	seen   []byte                   // the state file as the probe saw it
	dialer Dialer                   // replaces the script dialer when set
	term   ui.Terminal              // replaces the shared virtual terminal when set (tests that do not look at the screen)
}

func newRig(t *testing.T) *rig {
	r := &rig{t: t, scr: vt.New(100, 30), d: newScriptDialer(t), home: t.TempDir()}
	a, err := New(r.config())
	if err != nil {
		t.Fatal(err)
	}
	r.a = a
	t.Cleanup(func() {
		a.Close()
		if b := r.newClient(); b != nil {
			b.Close()
		}
	})
	return r
}

func (r *rig) config() Config {
	var dl Dialer = r.d
	if r.dialer != nil {
		dl = r.dialer
	}
	var term ui.Terminal = r.scr
	if r.term != nil {
		term = r.term
	}
	return Config{Term: term, HomeDir: r.home, NoRC: true, Quiet: true, Dialer: dl, Interactive: true,
		RestartProbe: r.doProbe, RestartExec: r.doExec}
}

func (r *rig) newClient() *Client { r.mu.Lock(); defer r.mu.Unlock(); return r.b }

// cur is the client that is "running" now.
func (r *rig) cur() *Client {
	if b := r.newClient(); b != nil {
		return b
	}
	return r.a
}

func dupFDs(t *testing.T, env []string) *RestoreSpec {
	spec, err := RestoreSpecFromEnv(env)
	if err != nil || spec == nil {
		t.Fatalf("env: %v %v", spec, err)
	}
	for i, n := range spec.FDs {
		d, err := syscall.Dup(n)
		if err != nil {
			t.Fatal(err)
		}
		spec.FDs[i] = d
	}
	return spec
}

func fdEnv(path string, files []*os.File, probe bool) []string {
	var nums []string
	for _, f := range files {
		nums = append(nums, fmt.Sprint(f.Fd()))
	}
	env := []string{envState + "=" + path, envFDs + "=" + strings.Join(nums, ",")}
	if probe {
		env = append(env, envReady+"=3")
	}
	return env
}

// doProbe rehearses in this process: the real ProbeMain path with a Sink terminal.
func (r *rig) doProbe(req ProbeRequest) error {
	if r.probe != nil {
		return r.probe(req)
	}
	r.seen, _ = os.ReadFile(req.StatePath)
	spec := dupFDs(r.t, fdEnv(req.StatePath, req.Files, false))
	restoreNonblocking(req.Files) // (File.Fd made them blocking)
	spec.Probe, spec.ReadyFD = true, -1
	lr, err := LoadRestoration(*spec)
	if err != nil {
		return err
	}
	cfg := r.config()
	cfg.Term, cfg.Restore = nil, lr
	cfg.Term = newSink()
	c, err := New(cfg)
	if err != nil {
		return err
	}
	c.discardRestored()
	return nil
}

func (r *rig) doExec(exe string, argv, env []string) error {
	spec := dupFDs(r.t, env)
	lr, err := LoadRestoration(*spec)
	if err != nil {
		r.t.Errorf("new process: %v", err)
		return err
	}
	cfg := r.config()
	cfg.Restore = lr
	b, err := New(cfg)
	if err != nil {
		r.t.Errorf("new process: %v", err)
		return err
	}
	r.mu.Lock()
	r.b = b
	r.mu.Unlock()
	b.Start()
	r.a.Abandon()
	return ErrExecDone
}

func (r *rig) restart(force bool) error {
	var err error
	r.a.Do(func(*gtfparser.Interp) { err = r.a.Restart(force) })
	return err
}

// snap captures what the user sees: every row with its attributes, and the cursor.
func snap(scr *vt.Screen) string {
	var b strings.Builder
	for y := 0; y < scr.H; y++ {
		fmt.Fprintf(&b, "%s |%s\n", scr.Row(y), scr.AttrRow(y))
	}
	fmt.Fprintf(&b, "cursor %d,%d", scr.X, scr.Y)
	return b.String()
}

func (r *rig) settled() string {
	r.t.Helper()
	time.Sleep(60 * time.Millisecond)
	r.cur().Flush()
	return snap(r.scr)
}

func (r *rig) connect(world, port string) *nettest.Server {
	exec(r.a, "/addworld "+world+" localhost "+port, "/connect "+world)
	return r.d.server(r.t, r.a, world)
}

func (r *rig) wait(want string) {
	r.t.Helper()
	waitScreen(r.t, r.cur(), r.scr, want)
}

type sinkTerm struct{}

func newSink() *sinkTerm                      { return &sinkTerm{} }
func (*sinkTerm) Write(p []byte) (int, error) { return len(p), nil }
func (*sinkTerm) Size() (int, int)            { return 100, 30 }

// The whole picture: a connected world in the middle of a line, colour in the
// scrollback, a pager that has engaged, a half-typed input line with its
// cursor, a second world with unseen lines; the screen after the restart is
// the screen before, and the connections carry on where they were.
func TestRestartKeepsScreenConnectionsAndInput(t *testing.T) {
	r := newRig(t)
	srv := r.connect("w", "4000")
	exec(r.a, "/set more=on")
	var sb strings.Builder
	sb.WriteString("\x1b[1;31mred bold\x1b[0m and plain\r\n")
	for i := 1; i <= 50; i++ {
		fmt.Fprintf(&sb, "game line %d\r\n", i)
	}
	srv.Send([]byte(sb.String()))
	r.wait("game line")
	// a second world in the background with output nobody has seen
	srv2 := r.connect("bg", "4001")
	srv2.Send([]byte("background news\r\n"))
	time.Sleep(50 * time.Millisecond)
	exec(r.a, "/fg w")
	r.a.Do(func(*gtfparser.Interp) { r.a.onKeys("hello world"); r.a.onKeys("\x01X") }) // ^A, then X: cursor after the X
	srv.Send([]byte("partial "))                                                       // a line that is not finished
	time.Sleep(40 * time.Millisecond)
	before := r.settled()
	var paused bool
	var heldBefore int
	r.a.Do(func(*gtfparser.Interp) { paused, heldBefore = r.a.fg.scr.Paused, r.a.fg.scr.Held() })
	if !paused || heldBefore == 0 {
		t.Fatalf("pager not engaged:\n%s", before)
	}

	if err := r.restart(false); err != nil {
		t.Fatal(err)
	}
	after := r.settled()
	if before != after {
		t.Fatalf("the screen changed across the restart\nbefore:\n%s\nafter:\n%s", before, after)
	}
	b := r.newClient()
	var fgName, in string
	var stillPaused bool
	var held int
	b.Do(func(i *gtfparser.Interp) {
		fgName = b.fg.World.Name
		in = b.in.Input().Text()
		stillPaused, held = b.fg.scr.Paused, b.fg.scr.Held()
	})
	if fgName != "w" || in != "Xhello world" || !stillPaused || held != heldBefore {
		t.Fatalf("fg=%q input=%q paused=%v held=%d (was %d)", fgName, in, stillPaused, held, heldBefore)
	}
	// the server never saw the connection end, and the rest of the line completes it
	srv.Send([]byte("line\r\nafter restart\r\n"))
	b.Do(func(*gtfparser.Interp) { b.onKeys("\t\t\t") }) // page through what was held
	r.wait("after restart")
	mustContain(t, r.scr, "partial line")
	// typing and sending work through the new process
	b.Do(func(*gtfparser.Interp) { b.in.Input().Clear(); b.onKeys("look\r") })
	srv.Expect([]byte("look\r\n"))
	// the background world's unseen lines survive and can be read
	exec(b, "/fg bg")
	r.wait("background news")
	srv2.Send([]byte("more from bg\r\n"))
	r.wait("more from bg")
	// the state file is gone and was private
	ents, _ := os.ReadDir(filepath.Join(r.home, ".gtf"))
	for _, e := range ents {
		if strings.HasPrefix(e.Name(), "restart-") {
			t.Fatalf("state file left behind: %s", e.Name())
		}
	}
}

// A session in the middle of an MCCP2 compressed stream survives a restart and
// the new process sees the rest of the data.
func TestRestartMidCompressedStream(t *testing.T) {
	r := newRig(t)
	srv := r.connect("w", "4000")
	srv.Send(nettest.IAC(telnet.WILL, telnet.OptMCCP2))
	srv.Expect(nettest.IAC(telnet.DO, telnet.OptMCCP2))
	srv.Send(nettest.IAC(telnet.SB, telnet.OptMCCP2), []byte{telnet.IAC, telnet.SE})
	var z bytes.Buffer
	zw := zlib.NewWriter(&z)
	zw.Write([]byte("zipped one\r\nzipped tw"))
	zw.Flush()
	cut := z.Len()
	zw.Write([]byte("o\r\nzipped three\r\n"))
	zw.Flush()
	srv.Send(z.Bytes()[:cut])
	r.wait("zipped one")
	before := r.settled()
	if err := r.restart(false); err != nil {
		t.Fatal(err)
	}
	srv.Send(z.Bytes()[cut:])
	r.wait("zipped three")
	mustContain(t, r.scr, "zipped two")
	_ = before
}

// A probe that fails leaves everything as it was: the connection keeps working,
// bytes that arrived during the attempt are delivered, the input line is intact.
func TestRestartRollbackKeepsEverything(t *testing.T) {
	for name, probe := range map[string]func(ProbeRequest) error{
		"exits":   func(ProbeRequest) error { return errors.New("it exited (exit status 3) before reporting ready") },
		"refuses": func(ProbeRequest) error { return errors.New("handoff: the state file was written by a newer gtf") },
		"hangs": func(ProbeRequest) error {
			time.Sleep(100 * time.Millisecond)
			return errors.New("it did not report ready within 100ms (killed)")
		},
	} {
		t.Run(name, func(t *testing.T) {
			r := newRig(t)
			r.probe = probe
			srv := r.connect("w", "4000")
			srv.Send([]byte("before\r\n"))
			r.wait("before")
			r.a.Do(func(*gtfparser.Interp) { r.a.onKeys("half typed") })
			err := r.restart(false)
			if err == nil || !strings.Contains(err.Error(), "not ready") {
				t.Fatalf("err = %v", err)
			}
			srv.Send([]byte("during and after\r\n"))
			r.wait("during and after")
			r.a.Do(func(*gtfparser.Interp) {
				if got := r.a.in.Input().Text(); got != "half typed" {
					t.Errorf("input %q", got)
				}
				r.a.in.Input().Clear()
				r.a.onKeys("look\r")
			})
			srv.Expect([]byte("look\r\n"))
			if _, err := os.Stat(filepath.Join(r.home, ".gtf", fmt.Sprintf("restart-%d.state", os.Getpid()))); !os.IsNotExist(err) {
				t.Fatalf("state file not removed after the rollback: %v", err)
			}
			// and a later restart still works
			r.probe = nil
			if err := r.restart(false); err != nil {
				t.Fatal(err)
			}
			srv.Send([]byte("after second try\r\n"))
			r.wait("after second try")
		})
	}
}

// Nothing typed in no-echo mode (a password) reaches the state file, which is
// private and deleted once read; the world stays in no-echo mode.
func TestRestartNeverWritesSecrets(t *testing.T) {
	r := newRig(t)
	srv := r.connect("w", "4000")
	srv.Send(nettest.IAC(telnet.WILL, telnet.OptEcho), []byte("Password: "))
	srv.Expect(nettest.IAC(telnet.DO, telnet.OptEcho))
	r.wait("Password:")
	time.Sleep(50 * time.Millisecond)
	r.a.Do(func(*gtfparser.Interp) { r.a.onKeys("hunter2secret") })
	r.a.Flush()
	if strings.Contains(r.scr.Text(), "hunter2secret") {
		t.Fatal("the secret was echoed")
	}
	var mode os.FileMode
	r.probe = func(req ProbeRequest) error {
		fi, err := os.Stat(req.StatePath)
		if err != nil {
			return err
		}
		mode = fi.Mode().Perm()
		data, _ := os.ReadFile(req.StatePath)
		if bytes.Contains(data, []byte("hunter2secret")) {
			t.Errorf("the state file contains the secret")
		}
		return errors.New("rehearsal declined") // roll back: the point was only to look at the file
	}
	if err := r.restart(false); err == nil {
		t.Fatal("expected the rehearsal error")
	}
	if mode != 0o600 {
		t.Fatalf("state file mode %v", mode)
	}
	r.probe = func(req ProbeRequest) error {
		data, _ := os.ReadFile(req.StatePath)
		if bytes.Contains(data, []byte("hunter2secret")) {
			t.Errorf("the state file contains the secret")
		}
		return nil
	}
	if err := r.restart(false); err != nil {
		t.Fatal(err)
	}
	b := r.newClient()
	var hidden bool
	b.Do(func(*gtfparser.Interp) { hidden = !b.in.LocalEcho("w"); b.onKeys("again") })
	b.Flush()
	if !hidden || strings.Contains(r.scr.Text(), "again") {
		t.Fatalf("password mode lost (hidden=%v):\n%s", hidden, r.scr.Dump())
	}
}

// The plan lists sessions that cannot be carried; Restart refuses unless forced.
func TestRestartPlanAndRefusal(t *testing.T) {
	r := newRig(t)
	r.d.fail["slow.example 1"] = nil
	exec(r.a, "/addworld w localhost 4000", "/connect w")
	r.d.server(t, r.a, "w")
	// a connection still being set up cannot be carried
	exec(r.a, "/addworld pending localhost 4001")
	r.a.Do(func(*gtfparser.Interp) {
		s := r.a.newSession(World{Name: "pending"}, gtfparser.ConnectOptions{}, nil)
		r.a.sessions["pending"] = s
		r.a.order = append(r.a.order, s)
	})
	var plan RestartPlan
	r.a.Do(func(*gtfparser.Interp) { plan = r.a.RestartPlan() })
	if !plan.Supported || len(plan.Carried) != 1 || plan.Carried[0].Name != "w" || plan.Carried[0].Kind != "tcp" ||
		len(plan.Ends) != 1 || plan.Ends[0].Name != "pending" || plan.Ends[0].Reason == "" {
		t.Fatalf("%+v", plan)
	}
	err := r.restart(false)
	var ref *RestartRefused
	if !errors.As(err, &ref) {
		t.Fatalf("err = %v", err)
	}
	// /restart -n prints the plan and does nothing
	exec(r.a, "/restart -n")
	r.a.Flush()
	mustContain(t, r.scr, "carried: w (tcp)", "ends:    pending")
	// /restart refuses and lists
	exec(r.a, "/restart")
	r.a.Flush()
	mustContain(t, r.scr, "refused", "would end: pending")
	if r.newClient() != nil {
		t.Fatal("restarted although refused")
	}
}
