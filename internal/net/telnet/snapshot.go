// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package telnet

import "errors"

// State snapshots let a live connection be handed to a new process (/restart,
// docs/RESTART.md): everything the Machine knows about the stream so far,
// including a half-parsed sequence and the MCCP2 decompressor in the middle of
// a stream, as plain data that survives JSON. Restoring it into a Machine that
// has the same handlers and feeding the bytes that follow produces exactly what
// the original Machine would have. The shape follows squatClientDaemon's
// pkg/telnet/snapshot.go.

// MachineStateVersion is the version of MachineState written by this build.
// Fields may be added without changing it (they must be optional); anything
// else bumps it and Restore refuses a state it does not know.
const MachineStateVersion = 1

// OptState is the RFC 1143 state of one option (the Q-method states of
// our side and the server's side).
type OptState struct {
	Opt  byte  `json:"o"`
	Us   uint8 `json:"u,omitempty"`
	Them uint8 `json:"t,omitempty"`
}

// MachineState is the persistent state of a Machine.
type MachineState struct {
	V        int               `json:"v"`
	PS       uint8             `json:"ps,omitempty"`    // scanner state
	Cmd      byte              `json:"cmd,omitempty"`   // pending WILL/WONT/DO/DONT
	SBOpt    byte              `json:"sbOpt,omitempty"` // option of the subnegotiation in progress
	SB       []byte            `json:"sb,omitempty"`    // its payload so far
	Opts     []OptState        `json:"opts,omitempty"`  // options whose state is not "off"
	Z        *ZState           `json:"z,omitempty"`     // an MCCP2 stream is active
	Handlers map[string][]byte `json:"handlers,omitempty"`
}

// ZState is the state of an MCCP2 (zlib) decompressor.
type ZState struct {
	In     []byte    `json:"in,omitempty"` // unconsumed input
	BitBuf uint32    `json:"bitBuf"`
	BitCnt uint      `json:"bitCnt"`
	State  int       `json:"state"`
	Final  bool      `json:"final,omitempty"`
	Lit    *HuffTree `json:"lit,omitempty"`
	Dist   *HuffTree `json:"dist,omitempty"`
	Stored int       `json:"stored,omitempty"`
	Hist   []byte    `json:"hist,omitempty"`
	AdlerA uint32    `json:"adlerA"`
	AdlerB uint32    `json:"adlerB"`
}

// HuffTree is a Huffman decoding table.
type HuffTree struct {
	Count  [16]uint16 `json:"count"`
	Symbol []uint16   `json:"symbol"`
}

// Stateful is implemented by handlers that keep state between messages (for
// example the TERMINAL-TYPE cycle position); it is saved and restored by name.
type Stateful interface {
	SaveState() []byte
	LoadState([]byte) error
}

// ErrBadState is returned by Restore for a state that cannot belong to a Machine.
var ErrBadState = errors.New("telnet: invalid machine state")

// ErrStateTooNew is returned by Restore for a state written by a newer build.
var ErrStateTooNew = errors.New("telnet: machine state written by a newer build")

func toTree(h *huffman) *HuffTree {
	if h == nil {
		return nil
	}
	return &HuffTree{Count: h.count, Symbol: append([]uint16(nil), h.symbol...)}
}

func fromTree(t *HuffTree) *huffman {
	if t == nil {
		return nil
	}
	return &huffman{count: t.Count, symbol: append([]uint16(nil), t.Symbol...)}
}

// Snapshot returns the Machine's state. Call it only while nothing is feeding
// the machine (the reader is stopped). Pending text is always flushed at the
// end of Feed, so none is part of the state.
func (m *Machine) Snapshot() MachineState {
	st := MachineState{V: MachineStateVersion, PS: m.ps, Cmd: m.cmd, SBOpt: m.sbOpt, SB: append([]byte(nil), m.sb...)}
	m.smu.Lock()
	for i, s := range m.states {
		if s.us != qNo || s.them != qNo {
			st.Opts = append(st.Opts, OptState{Opt: byte(i), Us: uint8(s.us), Them: uint8(s.them)})
		}
	}
	m.smu.Unlock()
	if z := m.z; z != nil {
		st.Z = &ZState{
			In: append([]byte(nil), z.in[z.pos:]...), BitBuf: z.bitBuf, BitCnt: z.bitCnt, State: z.state,
			Final: z.final, Lit: toTree(z.lit), Dist: toTree(z.dist), Stored: z.stored,
			Hist: append([]byte(nil), z.hist...), AdlerA: z.adl.a, AdlerB: z.adl.b,
		}
	}
	for _, h := range m.order {
		if sh, ok := h.(Stateful); ok {
			if b := sh.SaveState(); b != nil {
				if st.Handlers == nil {
					st.Handlers = map[string][]byte{}
				}
				st.Handlers[h.Name()] = b
			}
		}
	}
	return st
}

// CheckState verifies that Restore would accept st, without changing m.
func (m *Machine) CheckState(st MachineState) error {
	if st.V > MachineStateVersion {
		return ErrStateTooNew
	}
	if st.PS > psSBIAC || len(st.SB) > maxSubneg {
		return ErrBadState
	}
	for _, o := range st.Opts {
		if o.Us > uint8(qWantYesOpp) || o.Them > uint8(qWantYesOpp) {
			return ErrBadState
		}
	}
	if z := st.Z; z != nil {
		if z.State < izHeader || z.State > izDone || (z.State == izCodes && (z.Lit == nil || z.Dist == nil)) {
			return ErrBadState
		}
	}
	return nil
}

// Restore puts st into m (a freshly made Machine with the same handlers). The
// handlers' own state is restored too; a handler missing from st keeps its
// initial state.
func (m *Machine) Restore(st MachineState) error {
	if err := m.CheckState(st); err != nil {
		return err
	}
	m.ps, m.cmd, m.sbOpt = st.PS, st.Cmd, st.SBOpt
	m.sb = append([]byte(nil), st.SB...)
	m.smu.Lock()
	m.states = [256]optState{}
	for _, o := range st.Opts {
		m.states[o.Opt] = optState{us: qs(o.Us), them: qs(o.Them)}
	}
	m.smu.Unlock()
	m.z = nil
	if z := st.Z; z != nil {
		m.z = &inflater{in: append([]byte(nil), z.In...), bitBuf: z.BitBuf, bitCnt: z.BitCnt, state: z.State,
			final: z.Final, lit: fromTree(z.Lit), dist: fromTree(z.Dist), stored: z.Stored,
			hist: append([]byte(nil), z.Hist...)}
		m.z.adl.a, m.z.adl.b = z.AdlerA, z.AdlerB
	}
	for _, h := range m.order {
		if sh, ok := h.(Stateful); ok {
			if b, ok := st.Handlers[h.Name()]; ok {
				if err := sh.LoadState(b); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
