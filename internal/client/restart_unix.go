// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

//go:build darwin || dragonfly || freebsd || netbsd || openbsd || linux

package client

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	osexec "os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// Environment variables that carry the handoff to the next process.
const (
	envState = "GTF_RESTART_STATE" // path of the state file
	envFDs   = "GTF_RESTART_FDS"   // inherited descriptor numbers, in slot order
	envReady = "GTF_RESTART_READY" // probe only: descriptor to report on
)

func restartSupported() (bool, string) { return true, "" }

// restartEnv is env with the handoff variables for the exec'd process.
func restartEnv(env []string, statePath string, files []*os.File) []string {
	out := stripRestartEnv(env)
	nums := make([]string, len(files))
	for i, f := range files {
		nums[i] = strconv.Itoa(int(f.Fd()))
	}
	return append(out, envState+"="+statePath, envFDs+"="+strings.Join(nums, ","))
}

func stripRestartEnv(env []string) []string {
	var out []string
	for _, e := range env {
		if strings.HasPrefix(e, envState+"=") || strings.HasPrefix(e, envFDs+"=") || strings.HasPrefix(e, envReady+"=") {
			continue
		}
		out = append(out, e)
	}
	return out
}

// prepareHandoverFiles clears close-on-exec on the descriptors that must
// survive the exec (the terminal's 0, 1 and 2 never have it).
func prepareHandoverFiles(files []*os.File) error {
	for _, f := range files {
		if _, err := unix.FcntlInt(f.Fd(), unix.F_SETFD, 0); err != nil {
			return fmt.Errorf("clearing close-on-exec: %v", err)
		}
	}
	return nil
}

// undoHandoverFiles puts close-on-exec back after a failed exec.
func undoHandoverFiles(files []*os.File) {
	for _, f := range files {
		unix.CloseOnExec(int(f.Fd()))
	}
	restoreNonblocking(files)
}

// restoreNonblocking puts the descriptors back in non-blocking mode. File.Fd
// switches a descriptor to blocking mode, and a duplicate shares that flag with
// the socket the old process is still reading through the network poller:
// left blocking, a deadline could no longer wake its reader.
func restoreNonblocking(files []*os.File) {
	for _, f := range files {
		unix.SetNonblock(int(f.Fd()), true)
	}
}

// execBinary replaces the process. It returns only on failure.
func execBinary(exe string, argv, env []string) error {
	return syscall.Exec(exe, argv, env)
}

// spawnProbe starts the binary as a rehearsal child: descriptor 3 is the pipe
// it reports on, 4.. are the handed-over files. It must say "ok" (it has read
// the state file, validated everything and adopted the sockets paused, then
// let them go) within the timeout; anything else is a failure and the child is
// killed. The child has no terminal (its own session, no stdin).
func spawnProbe(req ProbeRequest) (err error) {
	defer restoreNonblocking(req.Files) // handing a file to a child (File.Fd) makes the shared socket blocking
	rd, wr, err := os.Pipe()
	if err != nil {
		return err
	}
	defer rd.Close()
	nums := make([]string, len(req.Files))
	for i := range req.Files {
		nums[i] = strconv.Itoa(4 + i)
	}
	env := append(stripRestartEnv(req.Env), envState+"="+req.StatePath, envFDs+"="+strings.Join(nums, ","), envReady+"=3")
	cmd := osexec.Command(req.Exe)
	cmd.Args = req.Argv
	cmd.Env = env
	cmd.ExtraFiles = append([]*os.File{wr}, req.Files...)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &limitWriter{b: &out, n: 4096}, &limitWriter{b: &out, n: 4096}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err = cmd.Start(); err != nil {
		wr.Close()
		return err
	}
	wr.Close()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	line := make(chan string, 1)
	go func() {
		s, _ := bufio.NewReader(rd).ReadString('\n')
		line <- strings.TrimSpace(s)
	}()
	tail := func() string {
		if t := strings.TrimSpace(out.String()); t != "" {
			if len(t) > 300 {
				t = t[len(t)-300:]
			}
			return ": " + strings.ReplaceAll(t, "\n", " | ")
		}
		return ""
	}
	kill := func() {
		cmd.Process.Kill()
		<-done
	}
	timer := time.NewTimer(req.Timeout)
	defer timer.Stop()
	select {
	case l := <-line:
		switch {
		case l == "ok":
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				kill()
			}
			return nil
		case l == "":
			// closed without a word: it died or exited
			select {
			case err := <-done:
				return fmt.Errorf("it exited (%v) without reporting ready%s", err, tail())
			case <-time.After(time.Second):
				kill()
				return fmt.Errorf("it closed its report pipe without reporting ready%s", tail())
			}
		default:
			kill()
			return errors.New(strings.TrimPrefix(l, "error: "))
		}
	case err := <-done:
		select { // it may have reported just before exiting
		case l := <-line:
			if l == "ok" {
				return nil
			}
		default:
		}
		return fmt.Errorf("it exited (%v) before reporting ready%s", err, tail())
	case <-timer.C:
		kill()
		return fmt.Errorf("it did not report ready within %v (killed)", req.Timeout)
	}
}

type limitWriter struct {
	b *bytes.Buffer
	n int
}

func (w *limitWriter) Write(p []byte) (int, error) {
	if w.b.Len() < w.n {
		w.b.Write(p[:min(len(p), w.n-w.b.Len())])
	}
	return len(p), nil
}
