// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

//go:build !(darwin || dragonfly || freebsd || netbsd || openbsd || linux)

package client

import (
	"errors"
	"os"
)

// Environment variables that carry the handoff to the next process.
const (
	envState = "GTF_RESTART_STATE"
	envFDs   = "GTF_RESTART_FDS"
	envReady = "GTF_RESTART_READY"
)

func restartSupported() (bool, string) { return false, "not supported on this platform" }

func restartEnv(env []string, _ string, _ []*os.File) []string { return env }
func prepareHandoverFiles([]*os.File) error                    { return errors.New("not supported on this platform") }
func undoHandoverFiles([]*os.File)                             {}
func execBinary(string, []string, []string) error {
	return errors.New("not supported on this platform")
}
func spawnProbe(ProbeRequest) error { return errors.New("not supported on this platform") }
