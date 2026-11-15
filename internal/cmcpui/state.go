// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package cmcpui

import (
	"time"
	"unicode"

	"github.com/itsnotyoutoday/cmcp/pack"
	"github.com/itsnotyoutoday/gtf/internal/layout"
)

// Key is a normalised key press: "tab", "btab", "up", "down", "left", "right", "enter", "space",
// "esc", "home", "end", "pgup", "pgdn", or the character itself ("+", "-", "a").
type Key string

// Event is a widget event for the core to perform (node.Perform(Name, Value, ...)).
type Event struct {
	Node  *pack.Node
	Name  string // click, select, change, release, assign
	Value any
}

// Block is a ready cell picture (a rasterised sprite).
type Block struct {
	W, H  int
	Cells []layout.Cell
}

// Assets gives the renderer the pictures it cannot compute itself. All methods may return nil.
type Assets interface {
	// Sprite renders sprite id (view chosen by the implementation) into w x h cells, with the
	// zone tints and glows currently evaluated from data.
	Sprite(id string, w, h int) *Block
	// Zones lists the zone ids of a sprite in drawing order (for keyboard selection).
	Zones(id string) []string
}

// Env is what one Draw needs besides the tree.
type Env struct {
	Theme  Theme
	Assets Assets
	Now    time.Time
	// Alert is "green"/"yellow"/"red" (tints the frame of a red alert).
	Alert string
	// Effects are recent Cobra.Event happenings for the viewscreen.
	Effects []Effect
	// Header is the title shown on row 0; Notice replaces the key hint there for a while.
	Header string
	Notice string
}

// Effect is a transient viewscreen effect (spec 7.8): fire, hit, destroyed.
type Effect struct {
	Type string
	At   time.Time
	Data map[string]any
}

type wstate struct {
	cur, scroll int
	stage       int
	team        string
	zone        int
	val         float64
	valUntil    time.Time
	az, mark    float64
	speed, warp float64
	inited      bool
	hist        []float64
	armed       time.Time
	lastVal     float64
}

// State is the screen-local UI state that outlives a frame: $ui, the focus and the per-widget
// cursors. A ship change (UI.Generation) is the caller's cue to Reset it.
type State struct {
	UI    map[string]any
	Focus string // Path of the focused node ("" = none yet)
	// Active says the screen has keyboard focus (the player toggled it): only then are keys taken.
	Active bool
	w      map[string]*wstate
}

// NewState returns an empty state.
func NewState() *State { return &State{UI: map[string]any{}, w: map[string]*wstate{}} }

// Reset clears everything (ship change).
func (s *State) Reset() { *s = *NewState() }

func (s *State) ws(path string) *wstate {
	w := s.w[path]
	if w == nil {
		w = &wstate{}
		s.w[path] = w
	}
	return w
}

// Item is one operable widget of a drawn frame.
type Item struct {
	Node *pack.BoundNode
	Rect Rect
	Kind string
	key  func(k Key) ([]Event, bool)
	// Disabled items are drawn dim and skipped by the focus order.
	Disabled bool
	// Mnemonic is the letter (upper case) that activates a button in screen-key mode, 0 if none.
	Mnemonic rune
}

// Frame is the result of Draw.
type Frame struct {
	Grid *layout.Grid
	// Text is where the real text pane goes (never drawn on). Empty W/H: the whole area is text
	// (the screen did not fit and was dropped, see Compact).
	Text    Rect
	Compact bool
	Items   []Item
	// Hint describes the focused item for the status line.
	Hint string
}

// focusOrder returns the indexes of items that can take focus.
func (f *Frame) focusOrder() []int {
	var o []int
	for i, it := range f.Items {
		if !it.Disabled && it.key != nil {
			o = append(o, i)
		}
	}
	return o
}

func (f *Frame) index(path string) int {
	for i, it := range f.Items {
		if it.Node.Node.Path == path {
			return i
		}
	}
	return -1
}

// Move shifts the focus by d items (wrapping).
func (f *Frame) Move(st *State, d int) {
	o := f.focusOrder()
	if len(o) == 0 {
		return
	}
	cur := -1
	for k, i := range o {
		if f.Items[i].Node.Node.Path == st.Focus {
			cur = k
		}
	}
	if cur < 0 {
		if d < 0 {
			cur = 0
		} else {
			cur = len(o) - 1
		}
	}
	n := ((cur+d)%len(o) + len(o)) % len(o)
	st.Focus = f.Items[o[n]].Node.Node.Path
}

// HandleKey offers a key to the focused widget. It returns the events to perform and whether the
// key was used. Tab and Shift-Tab (and the arrows a widget does not need) move the focus.
func (f *Frame) HandleKey(st *State, k Key) ([]Event, bool) {
	if len(f.focusOrder()) == 0 {
		return nil, false
	}
	if r := []rune(string(k)); len(r) == 1 && (unicode.IsLetter(r[0]) || unicode.IsDigit(r[0])) {
		// a mnemonic letter: focus that button and press it
		u := unicode.ToUpper(r[0])
		for _, i := range f.focusOrder() {
			if f.Items[i].Mnemonic == u {
				st.Focus = f.Items[i].Node.Node.Path
				return f.Items[i].key("enter")
			}
		}
	}
	if i := f.index(st.Focus); i >= 0 && f.Items[i].key != nil && !f.Items[i].Disabled {
		if ev, used := f.Items[i].key(k); used {
			return ev, true
		}
	}
	switch k {
	case "tab", "down", "right":
		f.Move(st, 1)
		return nil, true
	case "btab", "up", "left":
		f.Move(st, -1)
		return nil, true
	}
	return nil, false
}

// AtEdge reports whether moving the focus by d (Tab = +1, Shift-Tab = -1) would leave the
// screen: the focus is on the last (first) operable item, so the key returns to the input line.
func (f *Frame) AtEdge(st *State, d int) bool {
	o := f.focusOrder()
	if len(o) == 0 {
		return true
	}
	if i := f.index(st.Focus); i >= 0 {
		if d > 0 {
			return i == o[len(o)-1]
		}
		return i == o[0]
	}
	return false
}
