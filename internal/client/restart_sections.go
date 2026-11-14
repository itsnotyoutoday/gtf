// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package client

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"sync"
)

// Extra things that must survive /restart (a listener, peer connections of a
// component added to gtf later, ...) register a carrier here instead of
// editing the restart core. A carrier owns one named section of the state
// file with its own version, and any descriptors it wants to hand over are
// listed in the file next to the section name and item, so there is no fixed
// layout of inherited descriptors to run out of (see docs/RESTART.md,
// "Extending").
//
// Lifecycle, all on the client goroutine:
//
//	RestartPlan     what the section would carry and what would end (any time)
//	RestartQuiesce  old process: stop reading/accepting, flush what must not be lost
//	RestartSave     old process: return the section data (JSON-encodable) and
//	                hand over descriptors with w.AddFile
//	RestartRollback old process: the restart was abandoned, carry on
//	RestartRestore  new process: rebuild from the data and the inherited files
//
// A section is optional by default: a binary that does not know it ignores
// it. A carrier whose data must not be dropped silently (live connections)
// returns Required() true, so an older binary refuses the whole handoff and
// the old process keeps running.

// RestartCarrier carries one extra section across /restart.
type RestartCarrier interface {
	// Version is the version of the section this build writes and the highest
	// it reads. Fields may be added without bumping it; anything else bumps
	// it and RestartRestore migrates older versions (it is told which one
	// the file holds).
	Version() int
	// Required says that a reader which does not know the section must refuse
	// the file instead of skipping it.
	Required() bool
	// RestartPlan reports what a restart would carry and what would end.
	RestartPlan(c *Client) (carried, ends []RestartItem)
	// RestartQuiesce stops the component's activity so its state is stable.
	RestartQuiesce(c *Client) error
	// RestartSave returns the section's data and hands over descriptors.
	RestartSave(c *Client, w *RestartWriter) (any, error)
	// RestartRollback resumes after a failed or abandoned restart.
	RestartRollback(c *Client)
	// RestartRestore rebuilds the component in the new process from the saved
	// data (written at version v). When ctx.Probe is set this is a rehearsal
	// run by a short-lived process: validate and adopt paused, but do not
	// read, accept, bind or send anything (the old process still owns it all).
	RestartRestore(c *Client, ctx *RestoreContext, v int, data json.RawMessage) error
}

// RestartWriter is given to RestartSave.
type RestartWriter struct {
	files []*os.File
	owner []string // "section/item" per file
	sec   string
}

// AddFile hands a descriptor to the new process and returns its slot, which
// the section stores in its data and passes to RestoreContext.File. The writer
// keeps the *os.File alive until the exec; the caller must not close it, and
// should pass a duplicate (os.File of net.TCPConn.File() is one) so that the
// old process can still use its own descriptor if the restart is abandoned.
func (w *RestartWriter) AddFile(item string, f *os.File) int {
	w.files = append(w.files, f)
	w.owner = append(w.owner, w.sec+"/"+item)
	return len(w.files) - 1
}

// RestoreContext is given to RestartRestore (and carries what the core knows
// about this start).
type RestoreContext struct {
	// Probe is set in the rehearsal process (see RestartRestore).
	Probe bool
	files []*os.File
}

// File returns the inherited descriptor in a slot returned by AddFile.
func (r *RestoreContext) File(slot int) (*os.File, error) {
	if slot < 0 || slot >= len(r.files) || r.files[slot] == nil {
		return nil, fmt.Errorf("restart: no inherited descriptor in slot %d", slot)
	}
	return r.files[slot], nil
}

var (
	carrierMu    sync.Mutex
	carriers     = map[string]RestartCarrier{}
	carrierOrder []string
)

// RegisterRestartSection registers a carrier for the section called name (use
// a component-specific name such as "squatter"; the core's own sections are
// terminal, engine, ui, sessions, panels and client). Call it from an init
// function or before New.
func RegisterRestartSection(name string, carrier RestartCarrier) {
	carrierMu.Lock()
	defer carrierMu.Unlock()
	switch name {
	case secTerminal, secEngine, secUI, secSessions, secPanels, secClient:
		panic("client: restart section name " + name + " is reserved")
	}
	if _, dup := carriers[name]; dup {
		panic("client: restart section " + name + " registered twice")
	}
	carriers[name] = carrier
	carrierOrder = append(carrierOrder, name)
}

// registeredCarriers returns the carriers in registration order.
func registeredCarriers() (names []string, cs []RestartCarrier) {
	carrierMu.Lock()
	defer carrierMu.Unlock()
	for _, n := range carrierOrder {
		names = append(names, n)
		cs = append(cs, carriers[n])
	}
	return
}

// carrierNames returns the registered names sorted (for messages).
func carrierNames() []string {
	carrierMu.Lock()
	defer carrierMu.Unlock()
	out := make([]string, 0, len(carriers))
	for n := range carriers {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}
