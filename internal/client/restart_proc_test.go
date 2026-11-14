// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

//go:build darwin || dragonfly || freebsd || netbsd || openbsd || linux

package client

import (
	"fmt"
	"io"
	"net"
	"os"
	osexec "os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/itsnotyoutoday/gtf/internal/ui/vt"
)

// Real processes: the test binary is its own "gtf". With GTF_TEST_HELPER=client
// it runs a client on stdin/stdout pipes, with a real TCP dialer, restarting
// itself with the real exec and the real rehearsal child (this same binary).

func TestMain(m *testing.M) {
	if os.Getenv("GTF_TEST_HELPER") == "client" {
		os.Exit(helperClient())
	}
	os.Exit(m.Run())
}

type pipeTerm struct{}

func (pipeTerm) Write(p []byte) (int, error) { return os.Stdout.Write(p) }
func (pipeTerm) Size() (int, int)            { return 80, 24 }

func helperClient() int {
	spec, err := RestoreSpecFromEnv(os.Environ())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	base := Config{HomeDir: os.Getenv("GTF_TEST_HOME"), NoRC: true, Quiet: true, Interactive: true, Args: os.Args}
	if spec != nil && spec.Probe {
		switch os.Getenv("GTF_TEST_PROBE_MODE") {
		case "exit":
			return 3
		case "hang":
			time.Sleep(time.Hour)
		case "silent":
			return 0 // exits cleanly without reporting
		}
		return ProbeMain(spec, base)
	}
	cfg := base
	cfg.Term, cfg.Keys = pipeTerm{}, os.Stdin
	cfg.Commands = strings.Split(os.Getenv("GTF_TEST_CMDS"), "\n")
	if os.Getenv("GTF_TEST_FAULT") == "corrupt" {
		cfg.RestartProbe = func(req ProbeRequest) error {
			data, _ := os.ReadFile(req.StatePath)
			if len(data) > 100 {
				data[len(data)/2] ^= 0xff // bit rot
			}
			os.WriteFile(req.StatePath, data, 0o600)
			return spawnProbe(req)
		}
	}
	if spec != nil {
		r, err := LoadRestoration(*spec)
		if err != nil {
			fmt.Fprintln(os.Stderr, "restore:", err)
			return 1
		}
		cfg.Restore = r
	}
	c, err := New(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	signal.Ignore(syscall.SIGPIPE)
	c.Start()
	<-c.Done()
	c.Close()
	return 0
}

// proc is a helper client with its terminal captured in a vt.Screen.
type proc struct {
	t    *testing.T
	cmd  *osexec.Cmd
	in   io.WriteCloser
	mu   sync.Mutex
	scr  *vt.Screen
	done chan error
}

func startProc(t *testing.T, env ...string) *proc {
	t.Helper()
	cmd := osexec.Command(os.Args[0])
	cmd.Env = append(os.Environ(), "GTF_TEST_HELPER=client", "GTF_TEST_HOME="+t.TempDir())
	cmd.Env = append(cmd.Env, env...)
	in, _ := cmd.StdinPipe()
	out, _ := cmd.StdoutPipe()
	var errbuf strings.Builder
	cmd.Stderr = &errbuf
	p := &proc{t: t, cmd: cmd, in: in, scr: vt.New(80, 24), done: make(chan error, 1)}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := out.Read(buf)
			if n > 0 {
				p.mu.Lock()
				p.scr.Write(buf[:n])
				p.mu.Unlock()
			}
			if err != nil {
				return
			}
		}
	}()
	go func() { p.done <- cmd.Wait() }()
	t.Cleanup(func() {
		cmd.Process.Kill()
		if t.Failed() {
			t.Logf("helper stderr: %s\nscreen:\n%s", errbuf.String(), p.text())
		}
	})
	return p
}

func (p *proc) text() string { p.mu.Lock(); defer p.mu.Unlock(); return p.scr.Dump() }

func (p *proc) waitFor(want string) {
	p.t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		p.mu.Lock()
		ok := strings.Contains(p.scr.Text(), want)
		p.mu.Unlock()
		if ok {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	p.t.Fatalf("screen never showed %q:\n%s", want, p.text())
}

func (p *proc) keys(s string) { io.WriteString(p.in, s) }

func game(t *testing.T) (net.Listener, string) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	_, port, _ := net.SplitHostPort(ln.Addr().String())
	return ln, port
}

func acceptConn(t *testing.T, ln net.Listener) net.Conn {
	ln.(*net.TCPListener).SetDeadline(time.Now().Add(8 * time.Second))
	c, err := ln.Accept()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func readLine(t *testing.T, c net.Conn, want string) {
	t.Helper()
	c.SetReadDeadline(time.Now().Add(8 * time.Second))
	buf := make([]byte, 0, 64)
	one := make([]byte, 1)
	for !strings.HasSuffix(string(buf), want) {
		if _, err := c.Read(one); err != nil {
			t.Fatalf("game read: %v (got %q, want %q)", err, buf, want)
		}
		buf = append(buf, one[0])
	}
}

// The real thing: exec in place, same process, same TCP connection to the game.
func TestRestartRealExec(t *testing.T) {
	ln, port := game(t)
	p := startProc(t, "GTF_TEST_CMDS=/addworld w localhost "+port+"\n/connect w")
	g := acceptConn(t, ln)
	g.Write([]byte("hello from the game\r\npart"))
	p.waitFor("hello from the game")
	p.keys("typed")
	p.waitFor("typed")
	time.Sleep(30 * time.Millisecond)
	p.keys("\r")
	readLine(t, g, "typed\r\n")
	g.Write([]byte("ial\r\n")) // completes the partial line in the middle of the restart
	time.Sleep(20 * time.Millisecond)
	p.keys("half\x01X") // an unfinished input line with the cursor after X
	p.waitFor("Xhalf")
	pid := p.cmd.Process.Pid
	p.mu.Lock()
	before := snap(p.scr)
	p.mu.Unlock()
	// the line typed above is only on screen; restart
	p.keys("\x15/restart\r") // ^U clears the line first
	time.Sleep(1500 * time.Millisecond)
	select {
	case err := <-p.done:
		t.Fatalf("the process ended: %v\n%s", err, p.text())
	default:
	}
	if p.cmd.Process.Pid != pid {
		t.Fatal("pid changed")
	}
	// the game connection is the same one: it continues
	g.Write([]byte("after the exec\r\n"))
	p.waitFor("after the exec")
	p.mu.Lock()
	after := p.scr.Text()
	p.mu.Unlock()
	if !strings.Contains(after, "hello from the game") || !strings.Contains(after, "partial") {
		t.Fatalf("scrollback lost:\n%s\nbefore:\n%s", after, before)
	}
	p.keys("look\r")
	readLine(t, g, "look\r\n")
	// and it can be restarted again, and quits cleanly
	p.keys("/restart\r")
	time.Sleep(1500 * time.Millisecond)
	g.Write([]byte("second generation\r\n"))
	p.waitFor("second generation")
	p.keys("/quit\r")
	select {
	case err := <-p.done:
		if err != nil {
			t.Fatalf("exit: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("did not quit:\n%s", p.text())
	}
}

// A new binary that exits, hangs, exits silently or refuses a corrupt file
// costs nothing: the old process keeps the connection and says why.
func TestRestartRealRollback(t *testing.T) {
	for _, mode := range []string{"exit", "hang", "silent", "corrupt"} {
		t.Run(mode, func(t *testing.T) {
			ln, port := game(t)
			env := []string{"GTF_TEST_CMDS=/addworld w localhost " + port + "\n/connect w", "GTF_RESTART_TIMEOUT=1.5s"}
			if mode == "corrupt" {
				env = append(env, "GTF_TEST_FAULT=corrupt")
			} else {
				env = append(env, "GTF_TEST_PROBE_MODE="+mode)
			}
			p := startProc(t, env...)
			g := acceptConn(t, ln)
			g.Write([]byte("before\r\n"))
			p.waitFor("before")
			p.keys("/restart\r")
			p.waitFor("gtf keeps running")
			g.Write([]byte("the connection survived\r\n"))
			p.waitFor("the connection survived")
			p.keys("ping\r")
			readLine(t, g, "ping\r\n")
			select {
			case err := <-p.done:
				t.Fatalf("the process ended: %v", err)
			default:
			}
			p.keys("/quit\r")
		})
	}
}
