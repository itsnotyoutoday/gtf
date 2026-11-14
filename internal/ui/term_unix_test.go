// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

//go:build darwin || dragonfly || freebsd || netbsd || openbsd || linux

package ui

import (
	"os"
	"testing"
	"time"
)

// A parked key reader consumes nothing: a key typed while it is paused is
// delivered after Resume (this is what keeps keystrokes of a /restart handover).
func TestKeyReaderPauseConsumesNothing(t *testing.T) {
	rd, wr, _ := os.Pipe()
	defer rd.Close()
	defer wr.Close()
	kr, err := newKeyReader(int(rd.Fd()))
	if err != nil {
		t.Fatal(err)
	}
	got := make(chan string, 4)
	go func() {
		buf := make([]byte, 16)
		for {
			n, err := kr.Read(buf)
			if err != nil {
				return
			}
			got <- string(buf[:n])
		}
	}()
	time.Sleep(30 * time.Millisecond) // the reader is blocked waiting
	wr.WriteString("a")
	if s := <-got; s != "a" {
		t.Fatalf("got %q", s)
	}
	if !kr.Pause(time.Second) {
		t.Fatal("did not park")
	}
	wr.WriteString("b")
	select {
	case s := <-got:
		t.Fatalf("a paused reader consumed %q", s)
	case <-time.After(150 * time.Millisecond):
	}
	kr.Resume()
	select {
	case s := <-got:
		if s != "b" {
			t.Fatalf("got %q", s)
		}
	case <-time.After(time.Second):
		t.Fatal("key lost")
	}
}
