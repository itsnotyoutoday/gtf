// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

// Package ui is the terminal user interface: raw tty handling, scrollback,
// the more-pager, the input window, status line(s) and docked panels. It
// draws by composing a cell grid and diffing it against what the terminal
// shows, so the same code drives a real tty or the virtual terminal used by
// the tests.
package ui

import (
	"io"
	"os"

	"golang.org/x/term"
)

// Terminal is what the UI draws on.
type Terminal interface {
	io.Writer
	Size() (cols, rows int)
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
