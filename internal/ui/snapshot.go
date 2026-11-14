// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package ui

import (
	"errors"

	"github.com/itsnotyoutoday/gtf/internal/layout"
)

// Snapshots of the display state, for /restart (docs/RESTART.md): a screen's
// scrollback with its attributes and the pager position, and the UI's own
// bookkeeping. They are plain data (JSON-friendly) and compact: a line is its
// text plus runs of attribute indexes into a per-screen table.

// ScreenStateVersion is the version of ScreenState written by this build.
const ScreenStateVersion = 1

// LineState is one logical line: T holds one rune per cell (U+0000 for the
// placeholder after a wide character) and R pairs of (run length, attribute
// index) covering the cells; absent R means every cell has attribute 0.
type LineState struct {
	T string `json:"t,omitempty"`
	R []int  `json:"r,omitempty"`
}

// ScreenState is the persistent state of a Screen.
type ScreenState struct {
	V        int           `json:"v"`
	Name     string        `json:"name"`
	Max      int           `json:"max"`
	More     bool          `json:"more,omitempty"`
	Paused   bool          `json:"paused,omitempty"`
	Armed    bool          `json:"armed,omitempty"`
	Vis      int           `json:"vis"`
	Outcount int           `json:"outcount"`
	Unseen   int           `json:"unseen,omitempty"`
	Width    int           `json:"width"` // the width the physical lines were last wrapped at
	Wrap     WrapConfig    `json:"wrap"`
	Drop     int           `json:"drop,omitempty"`
	Attrs    []layout.Attr `json:"attrs"` // attribute table; index 0 is the default
	Lines    []LineState   `json:"lines,omitempty"`
}

// EncodeCells turns a line of cells into its compact form using (and
// extending) the attribute table.
func EncodeCells(cells []layout.Cell, table *[]layout.Attr, index map[layout.Attr]int) LineState {
	rs := make([]rune, len(cells))
	var runs []int
	allDefault := true
	for i, c := range cells {
		rs[i] = c.R
		ai, ok := index[c.A]
		if !ok {
			ai = len(*table)
			*table = append(*table, c.A)
			index[c.A] = ai
		}
		if ai != 0 {
			allDefault = false
		}
		if n := len(runs); n > 0 && runs[n-1] == ai {
			runs[n-2]++
		} else {
			runs = append(runs, 1, ai)
		}
	}
	ls := LineState{T: string(rs)}
	if !allDefault {
		ls.R = runs
	}
	return ls
}

// DecodeCells is the inverse of EncodeCells.
func DecodeCells(ls LineState, table []layout.Attr) ([]layout.Cell, error) {
	rs := []rune(ls.T)
	cells := make([]layout.Cell, len(rs))
	for i, r := range rs {
		cells[i].R = r
	}
	if len(ls.R) == 0 {
		return cells, nil
	}
	if len(ls.R)%2 != 0 {
		return nil, errors.New("ui: invalid attribute runs")
	}
	pos := 0
	for k := 0; k < len(ls.R); k += 2 {
		n, ai := ls.R[k], ls.R[k+1]
		if n < 0 || pos+n > len(cells) || ai < 0 || ai >= len(table) {
			return nil, errors.New("ui: invalid attribute runs")
		}
		for j := 0; j < n; j++ {
			cells[pos+j].A = table[ai]
		}
		pos += n
	}
	if pos != len(cells) {
		return nil, errors.New("ui: invalid attribute runs")
	}
	return cells, nil
}

// State captures the screen.
func (s *Screen) State() ScreenState {
	st := ScreenState{
		V: ScreenStateVersion, Name: s.Name, Max: s.SB.Max, More: s.More, Paused: s.Paused, Armed: s.armed,
		Vis: s.vis, Outcount: s.outcount, Unseen: s.Unseen, Width: s.SB.width, Wrap: s.SB.cfg, Drop: s.SB.drop,
		Attrs: []layout.Attr{{}},
	}
	index := map[layout.Attr]int{{}: 0}
	st.Lines = make([]LineState, len(s.SB.lines))
	for i, l := range s.SB.lines {
		st.Lines[i] = EncodeCells(l.Cells, &st.Attrs, index)
	}
	return st
}

// RestoreScreen rebuilds a screen from its state. keep is the /limit filter
// (nil for none); it is applied to the lines as they are wrapped, exactly as
// the live screen did.
func RestoreScreen(st ScreenState, keep func([]layout.Cell) bool) (*Screen, error) {
	if st.V > ScreenStateVersion {
		return nil, errors.New("ui: screen state written by a newer build")
	}
	if len(st.Attrs) == 0 || st.Max < 0 || st.Vis < 0 || st.Width < 0 {
		return nil, errors.New("ui: invalid screen state")
	}
	s := NewScreen(st.Name, st.Max)
	s.SB.Keep = keep
	s.SB.width, s.SB.cfg, s.SB.drop = st.Width, st.Wrap, st.Drop
	for _, ls := range st.Lines {
		cells, err := DecodeCells(ls, st.Attrs)
		if err != nil {
			return nil, err
		}
		s.SB.lines = append(s.SB.lines, Line{cells})
	}
	for i, l := range s.SB.lines {
		if keep != nil && !keep(l.Cells) {
			continue
		}
		for _, p := range Wrap(l.Cells, st.Width, st.Wrap) {
			s.SB.phys = append(s.SB.phys, p)
			s.SB.owner = append(s.SB.owner, i)
		}
	}
	s.More, s.Paused, s.armed, s.outcount, s.Unseen = st.More, st.Paused, st.Armed, st.Outcount, st.Unseen
	s.vis = min(st.Vis, s.SB.Len())
	return s, nil
}

// UIStateVersion is the version of UIState written by this build.
const UIStateVersion = 1

// NVItem is a line entered in non-visual mode, waiting to be printed.
type NVItem struct {
	At   int    `json:"at"`
	Text string `json:"text"`
}

// UIState is the display bookkeeping that is not part of a screen.
type UIState struct {
	V         int      `json:"v"`
	Old       []string `json:"old,omitempty"` // lines entered, shown above the input line
	HideAll   int      `json:"hideAll,omitempty"`
	NVOn      bool     `json:"nvOn,omitempty"` // non-visual mode had printed the current screen
	NVPrinted int      `json:"nvPrinted,omitempty"`
	NVQueue   []NVItem `json:"nvQueue,omitempty"`
}

// State captures the UI bookkeeping.
func (u *UI) State() UIState {
	st := UIState{V: UIStateVersion, Old: append([]string(nil), u.old...), HideAll: u.hideAll}
	if !u.Visual && u.nvScr != nil && u.nvScr == u.Scr {
		st.NVOn, st.NVPrinted = true, u.nvPrinted
	}
	for _, q := range u.nvQueue {
		st.NVQueue = append(st.NVQueue, NVItem{At: q.at, Text: q.text})
	}
	return st
}

// SetState restores the bookkeeping. Call it after the screen has been set
// with SetScreen. The next Render repaints completely in visual mode; in
// non-visual mode nothing that was already printed is printed again.
func (u *UI) SetState(st UIState) error {
	if st.V > UIStateVersion {
		return errors.New("ui: UI state written by a newer build")
	}
	u.old = append([]string(nil), st.Old...)
	u.hideAll = st.HideAll
	u.nvQueue = nil
	for _, q := range st.NVQueue {
		u.nvQueue = append(u.nvQueue, nvItem{at: q.At, text: q.Text})
	}
	if st.NVOn {
		u.nvScr, u.nvPrinted = u.Scr, min(st.NVPrinted, u.Scr.vis)
	}
	u.needFull = !st.NVOn
	return nil
}
