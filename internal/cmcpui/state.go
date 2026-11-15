// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package cmcpui

import (
	"time"

	"github.com/itsnotyoutoday/cmcp/focus"

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
	// Nav, when set, asks to show that screen of the menu (the left column).
	Nav string
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
	// ContactArt renders the art the scene gives a contact (a ship of that class and empire) into
	// w x h cells, or nil when it has none (a plain marker is drawn instead).
	ContactArt(scene string, contact map[string]any, w, h int) *Block
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
	// AlertRecolour is full (the whole palette switches), edge (only the frame edge, the alert segment and
	// the title accent) or off; empty is full.
	AlertRecolour string
	// PulseDim and PulseDepth: the alert is in the dim half of its pulse (0 depth = steady).
	PulseDim   bool
	PulseDepth float64
	// Effects are recent Cobra.Event happenings for the viewscreen.
	Effects []Effect
	// Mnemonics maps Node.Path to the mnemonic letter of a button or toggle (focus.AssignMnemonics
	// through pack FocusPlan); the renderer underlines it.
	Mnemonics map[string]string
	// Header is the title shown on row 0; Notice replaces the key hint there for a while.
	// Builtin is true for a std.* screen: the theme arranges it (packs arrange their own).
	Builtin bool
	// Expanded is the log view: the output grows to about 80% of the height and the panels
	// collapse to one summary line.
	Expanded bool
	// Title, Ship and Tags fill the frame's strips; Alert is green, yellow or red.
	Title, Ship string
	Tags        []string
	Status      []string
	// ScreenID is the built-in screen shown ("std.sensors"); Vars are the closed theme variables of
	// 19.5 (sd mode hull core lock alert ship class title) for the frame's text templates.
	TextRows int // the text pane's rows for this frame (the textport state, mid-animation too); 0 = balanced
	// Nav is the screen menu (the core's UI.Screens): the left column's entries, the current one lit, the
	// ones the console may not subscribe dimmed.
	Nav []NavEntry
	// Consent is a pending pack or theme consent question, shown on its own prominent line until answered.
	Consent  string
	ScreenID string
	Vars     map[string]any // the status strip: hull, shields, core, lock, alert
	ASCII    bool           // draw with ASCII characters only
	Layout   string         // "auto" (default), "side" or "top"
	Header   string
	Notice   string
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
	Order []string // focus panel ids as last drawn, in reading order (set by the glue)
	UI    map[string]any
	Focus string // Path of the focused node ("" = none yet)
	// Active says the screen has keyboard focus (the player toggled it): only then are keys taken.
	Active bool
	// Model is the shared keyboard focus model of this screen (cmcp/go/focus); the client owns it.
	Model *focus.Model
	w     map[string]*wstate
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
	Text Rect
	// Dropped counts the widgets that did not fit and were not drawn (the frame says so; F3 and F4 are
	// the way out).
	Dropped int
	// FocusName names the focused panel for the header ("RADAR", "SRS", "SCREEN MENU"); empty when the
	// screen does not have the keyboard.
	FocusName string
	// FocusKind is the kind of the focused item ("nav" for the screen menu), for the header's key list.
	FocusKind string
	// FocusLabel is the menu's highlighted screen (the header of the menu area names it).
	FocusLabel string
	Compact    bool
	Items      []Item
	// Boxes are the rectangles of every widget drawn (for the geometry tests: none overlaps another,
	// the text pane or the frame).
	Boxes []Rect
	// Hint describes the focused item for the status line.
	Hint string
}

func (f *Frame) index(path string) int {
	for i, it := range f.Items {
		if it.Node.Node.Path == path {
			return i
		}
	}
	return -1
}

// Press offers a key to the widget with the given Node.Path (the shared focus model decides which
// widget and which key; this is the widget's own handling of it: cursor moves, slider steps,
// team and helm editing). It returns the events to perform and whether the widget used the key.
func (f *Frame) Press(path string, k Key) ([]Event, bool) {
	if i := f.index(path); i >= 0 && f.Items[i].key != nil && !f.Items[i].Disabled {
		return f.Items[i].key(k)
	}
	return nil, false
}

// Activate is the activation a pressed mnemonic, or the confirmed third press of a hold_ms button,
// performs: a button's click, a toggle's change.
func (f *Frame) Activate(path string) []Event {
	i := f.index(path)
	if i < 0 || f.Items[i].Disabled {
		return nil
	}
	b := f.Items[i].Node
	switch b.Widget {
	case "button":
		return []Event{{Node: b.Node, Name: "click"}}
	case "toggle":
		return []Event{{Node: b.Node, Name: "change", Value: !boolProp(b, "value")}}
	}
	ev, _ := f.Items[i].key("enter")
	return ev
}

// Plan is the focus plan of a bound screen (pack BoundNode.FocusPlan).
type Plan struct {
	Panels    []focus.Panel
	Mnemonics map[string]string
}

func newModel(p Plan, focusKey string) *focus.Model {
	return focus.New(p.Panels, p.Mnemonics, focusKey)
}

// NavEntry is one entry of the screen menu drawn in the left column.
type NavEntry struct {
	ID, Title string
	Active    bool
	Available bool
	Cursor    bool // the keyboard cursor of the focused menu
}

// navWindow picks the entries that fit `capacity` rows of the column (two rows each), keeping the
// active one in view.
func navWindow(n []NavEntry, capacity int) []NavEntry {
	per := capacity / 2
	if per <= 0 || len(n) == 0 {
		return nil
	}
	if len(n) <= per {
		return n
	}
	act := 0
	for i, e := range n {
		if e.Active && act == 0 {
			act = i
		}
		if e.Cursor {
			act = i
		}
	}
	from := max(0, min(act-per/2, len(n)-per))
	return n[from : from+per]
}
