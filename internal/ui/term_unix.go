// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

//go:build darwin || dragonfly || freebsd || netbsd || openbsd || linux

package ui

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

// TTY is the real terminal: it puts the controlling tty into raw mode and
// reports window-size changes.
type TTY struct {
	in, out *os.File
	orig    *unix.Termios // the settings to restore (not the raw ones)
	keys    *keyReader
	winch   chan os.Signal
	Resized chan struct{}
}

// OpenTTY switches stdin to raw mode. Close restores it.
func OpenTTY() (*TTY, error) { return openTTY(nil) }

// OpenTTYFrom is OpenTTY for a process started by /restart: the terminal is
// already raw, and orig (from State of the previous process) holds the
// settings Close must restore, so that quitting leaves the shell's terminal
// as it was found.
func OpenTTYFrom(orig []byte) (*TTY, error) {
	var t unix.Termios
	if err := json.Unmarshal(orig, &t); err != nil {
		return nil, err
	}
	return openTTY(&t)
}

func openTTY(orig *unix.Termios) (*TTY, error) {
	t := &TTY{in: os.Stdin, out: os.Stdout, Resized: make(chan struct{}, 1)}
	fd := int(t.in.Fd())
	if orig == nil {
		cur, err := unix.IoctlGetTermios(fd, ioctlReadTermios)
		if err != nil {
			return nil, err
		}
		orig = cur
	}
	t.orig = orig
	if _, err := term.MakeRaw(fd); err != nil { // idempotent when the terminal is raw already
		return nil, err
	}
	kr, err := newKeyReader(fd)
	if err != nil {
		return nil, err
	}
	t.keys = kr
	t.winch = make(chan os.Signal, 1)
	signal.Notify(t.winch, syscall.SIGWINCH)
	go func() {
		for range t.winch {
			select {
			case t.Resized <- struct{}{}:
			default:
			}
		}
	}()
	return t, nil
}

// State returns the settings Close restores, for OpenTTYFrom.
func (t *TTY) State() []byte {
	b, _ := json.Marshal(t.orig)
	return b
}

// Write implements io.Writer.
func (t *TTY) Write(p []byte) (int, error) { return t.out.Write(p) }

// Size returns the current window size (80x24 if unknown).
func (t *TTY) Size() (int, int) {
	w, h, err := term.GetSize(int(t.out.Fd()))
	if err != nil || w <= 0 || h <= 0 {
		return 80, 24
	}
	return w, h
}

// Input is the tty for reading keys. The reader can be paused (Pause/Resume),
// which /restart uses so that no key typed during the handover is read by a
// process that is about to go away.
func (t *TTY) Input() io.Reader { return t.keys }

// Suspend restores the terminal, stops the process (SIGTSTP) and re-enters
// raw mode when it continues.
func (t *TTY) Suspend() {
	unix.IoctlSetTermios(int(t.in.Fd()), ioctlWriteTermios, t.orig)
	syscall.Kill(syscall.Getpid(), syscall.SIGSTOP)
	term.MakeRaw(int(t.in.Fd()))
}

// Close restores the terminal.
func (t *TTY) Close() error {
	signal.Stop(t.winch)
	return unix.IoctlSetTermios(int(t.in.Fd()), ioctlWriteTermios, t.orig)
}

// keyReader reads the terminal with select(2) so that a reader blocked waiting
// for a key can be parked without consuming one.
type keyReader struct {
	fd           int
	wakeR, wakeW int
	mu           sync.Mutex
	cond         *sync.Cond
	paused       bool
	parked       bool
}

func newKeyReader(fd int) (*keyReader, error) {
	var p [2]int
	if err := unix.Pipe(p[:]); err != nil {
		return nil, err
	}
	for _, f := range p {
		unix.CloseOnExec(f)
		unix.SetNonblock(f, true)
	}
	r := &keyReader{fd: fd, wakeR: p[0], wakeW: p[1]}
	r.cond = sync.NewCond(&r.mu)
	return r, nil
}

// Read implements io.Reader.
func (r *keyReader) Read(b []byte) (int, error) {
	for {
		r.mu.Lock()
		for r.paused {
			r.parked = true
			r.cond.Broadcast()
			r.cond.Wait()
		}
		r.parked = false
		r.mu.Unlock()
		if r.fd >= unix.FD_SETSIZE || r.wakeR >= unix.FD_SETSIZE {
			return unix.Read(r.fd, b) // cannot select: plain blocking read, not pausable
		}
		var set unix.FdSet
		set.Set(r.fd)
		set.Set(r.wakeR)
		n := max(r.fd, r.wakeR) + 1
		if _, err := unix.Select(n, &set, nil, nil, nil); err != nil {
			if errors.Is(err, unix.EINTR) {
				continue
			}
			return 0, err
		}
		if set.IsSet(r.wakeR) {
			var junk [16]byte
			for {
				if k, err := unix.Read(r.wakeR, junk[:]); k <= 0 || err != nil {
					break
				}
			}
			continue
		}
		k, err := unix.Read(r.fd, b)
		if errors.Is(err, unix.EINTR) || errors.Is(err, unix.EAGAIN) {
			continue
		}
		if err != nil {
			return 0, err
		}
		if k == 0 {
			return 0, io.EOF
		}
		return k, nil
	}
}

// Pause parks the key reader: when it returns true no goroutine is reading the
// terminal and no key has been taken that was not delivered. It gives up after
// timeout and returns false (the reader is then resumed).
func (r *keyReader) Pause(timeout time.Duration) bool {
	r.mu.Lock()
	r.paused = true
	r.mu.Unlock()
	unix.Write(r.wakeW, []byte{0})
	deadline := time.Now().Add(timeout)
	for {
		r.mu.Lock()
		ok := r.parked
		r.mu.Unlock()
		if ok {
			return true
		}
		if time.Now().After(deadline) {
			r.Resume()
			return false
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// Resume lets a paused key reader read again.
func (r *keyReader) Resume() {
	r.mu.Lock()
	r.paused = false
	r.cond.Broadcast()
	r.mu.Unlock()
}

// Pause and Resume of the TTY forward to its key reader.
func (t *TTY) Pause(timeout time.Duration) bool { return t.keys.Pause(timeout) }

// Resume lets the key reader read again.
func (t *TTY) Resume() { t.keys.Resume() }
