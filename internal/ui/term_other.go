// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

//go:build !(darwin || dragonfly || freebsd || netbsd || openbsd || linux)

package ui

import (
	"errors"
	"io"
	"time"
)

// TTY is not available on this platform (the interactive client needs a
// POSIX terminal); the type exists so the rest of the program builds.
type TTY struct {
	Resized chan struct{}
}

var errNoTTY = errors.New("the interactive terminal is not supported on this platform")

// OpenTTY always fails here.
func OpenTTY() (*TTY, error) { return nil, errNoTTY }

// OpenTTYFrom always fails here.
func OpenTTYFrom([]byte) (*TTY, error) { return nil, errNoTTY }

func (*TTY) State() []byte               { return nil }
func (*TTY) SetMouse(bool)               {}
func (*TTY) Write(p []byte) (int, error) { return len(p), nil }
func (*TTY) Size() (int, int)            { return 80, 24 }
func (*TTY) Input() io.Reader            { return nil }
func (*TTY) Suspend()                    {}
func (*TTY) Close() error                { return nil }
func (*TTY) Pause(time.Duration) bool    { return false }
func (*TTY) Resume()                     {}
