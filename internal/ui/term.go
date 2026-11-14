// Package ui is the terminal user interface: raw tty handling, scrollback,
// the more-pager, the input window, status line(s) and docked panels. It
// draws by composing a cell grid and diffing it against what the terminal
// shows, so the same code drives a real tty or the virtual terminal used by
// the tests.
package ui

import (
	"io"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"golang.org/x/term"
)

// Terminal is what the UI draws on.
type Terminal interface {
	io.Writer
	Size() (cols, rows int)
}

// TTY is the real terminal: it puts the controlling tty into raw mode and
// reports window-size changes.
type TTY struct {
	in, out *os.File
	old     *term.State
	mu      sync.Mutex
	winch   chan os.Signal
	Resized chan struct{}
}

// OpenTTY switches stdin to raw mode. Close restores it.
func OpenTTY() (*TTY, error) {
	t := &TTY{in: os.Stdin, out: os.Stdout, Resized: make(chan struct{}, 1)}
	st, err := term.MakeRaw(int(t.in.Fd()))
	if err != nil {
		return nil, err
	}
	t.old = st
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

// Input is the tty for reading keys.
func (t *TTY) Input() io.Reader { return t.in }

// Suspend restores the terminal, stops the process (SIGTSTP) and re-enters
// raw mode when it continues.
func (t *TTY) Suspend() {
	term.Restore(int(t.in.Fd()), t.old)
	syscall.Kill(syscall.Getpid(), syscall.SIGSTOP)
	if st, err := term.MakeRaw(int(t.in.Fd())); err == nil {
		t.old = st
	}
}

// Close restores the terminal.
func (t *TTY) Close() error {
	signal.Stop(t.winch)
	return term.Restore(int(t.in.Fd()), t.old)
}

// IsTerminal reports whether stdin and stdout are terminals.
func IsTerminal() bool {
	return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
}

// Sink is a Terminal that discards output (headless / batch mode).
type Sink struct{ W, H int }

// Write discards.
func (Sink) Write(p []byte) (int, error) { return len(p), nil }

// Size returns the configured size.
func (s Sink) Size() (int, int) { return s.W, s.H }
