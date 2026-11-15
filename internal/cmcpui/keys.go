// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package cmcpui

import (
	"strings"
	"time"
	"unicode/utf8"
)

// rawKeys names the terminal byte sequences the focus model knows (cmcp/go/focus key names).
var rawKeys = map[string]string{
	"\t": "tab", "\x1b[Z": "shift_tab", "\x1b": "esc", "\r": "enter", "\n": "enter",
	"\x1b[A": "up", "\x1b[B": "down", "\x1b[C": "right", "\x1b[D": "left",
	"\x1bOA": "up", "\x1bOB": "down", "\x1bOC": "right", "\x1bOD": "left",
	"\x1b[H": "home", "\x1b[F": "end", "\x1bOH": "home", "\x1bOF": "end", "\x1b[5~": "pgup", "\x1b[6~": "pgdn",
	"\x1bOP": "f1", "\x1bOQ": "f2", "\x1bOR": "f3", "\x1bOS": "f4", "\x1b[12~": "f2", "\x1b[1;2Q": "f2",
	// modified arrows are named so the model can swallow them: they are never bound (spec 6.12.3)
	"\x1b[1;2A": "shift_up", "\x1b[1;2B": "shift_down", "\x1b[1;2C": "shift_right", "\x1b[1;2D": "shift_left",
	"\x1b[1;3A": "alt_up", "\x1b[1;3B": "alt_down", "\x1b[1;3C": "alt_right", "\x1b[1;3D": "alt_left",
	"\x1b[1;5A": "ctrl_up", "\x1b[1;5B": "ctrl_down", "\x1b[1;5C": "ctrl_right", "\x1b[1;5D": "ctrl_left",
}

// KeyName gives the focus-model name of a raw key sequence: a table entry, a control character
// ("ctrl_x"), or a single printable character. ok is false for anything else (a paste, an unknown
// sequence): those are the input line's.
func KeyName(raw string) (string, bool) {
	if n, ok := rawKeys[raw]; ok {
		return n, true
	}
	if len(raw) == 1 && raw[0] < 0x20 {
		return "ctrl_" + string(rune('a'+raw[0]-1)), true
	}
	if utf8.RuneCountInString(raw) == 1 && raw[0] >= 0x20 && raw[0] != 0x7f {
		return raw, true
	}
	return "", false
}

// FocusKeyName turns the %cmcp_key_focus setting into a key name: "" or "^I" is tab, "F2".."F4",
// "^X" a control letter, "^[OQ" an escape sequence in /bind notation.
func FocusKeyName(setting string) string {
	v := strings.TrimSpace(setting)
	switch {
	case v == "" || v == "^I" || strings.EqualFold(v, "tab"):
		return "tab"
	case len(v) == 2 && v[0] == '^':
		if n, ok := KeyName(string(rune(v[1] & 0x1f))); ok {
			return n
		}
	case strings.HasPrefix(v, "^[") && len(v) > 2:
		if n, ok := KeyName("\x1b" + v[2:]); ok {
			return n
		}
	case len(v) == 2 && (v[0] == 'F' || v[0] == 'f'):
		return strings.ToLower(v)
	}
	return v
}

// Result is what Dispatch did with a key.
type Result struct {
	Events []Event // widget events to perform
	Input  bool    // the key is the input line's (it always works)
	Hold   string  // a hold_ms button was pressed but not yet confirmed: "id:n"
	Used   bool    // the screen took the key
}

// Dispatch runs one key through the shared focus model (cmcp/go/focus) and carries out its
// actions on the frame's widgets. raw is the key as the widgets understand it (a name from
// rawKeys, or the character) for the keys the model leaves to the widget (Left/Right on a helm
// field, a team assigner stage).
func Dispatch(f *Frame, st *State, key string, lineEmpty bool, now time.Time) Result {
	var r Result
	m := st.Model
	if m == nil {
		r.Input = true
		return r
	}
	for _, a := range m.Key(key, lineEmpty, now) {
		kind, arg, _ := strings.Cut(a, ":")
		switch kind {
		case "enter_focus", "leave_focus":
			r.Used = true
		case "focus":
			st.Focus, r.Used = arg, true
		case "move":
			k := Key("down")
			if arg == "-1" {
				k = "up"
			}
			ev, _ := f.Press(st.Focus, k)
			r.Events, r.Used = append(r.Events, ev...), true
		case "adjust":
			k := Key("right")
			if arg == "-1" {
				k = "left"
			}
			ev, _ := f.Press(st.Focus, k)
			r.Events, r.Used = append(r.Events, ev...), true
		case "activate":
			ev, _ := f.Press(arg, "enter")
			r.Events, r.Used = append(r.Events, ev...), true
		case "press":
			r.Events, r.Used = append(r.Events, f.Activate(arg)...), true
		case "hold":
			r.Hold, r.Used = arg, true
		case "ignore":
			// the model has no use for it: the focused widget may (Left/Right on a helm field)
			ev, _ := f.Press(st.Focus, Key(key))
			r.Events, r.Used = append(r.Events, ev...), true
		case "input":
			r.Input = true
		}
	}
	st.Active = m.InFocus()
	if p, ok := m.Focused(); ok {
		st.Focus = p.ID
	}
	return r
}

// SetupModel (re)builds the state's focus model from a bound screen's focus plan.
func (st *State) SetupModel(plan Plan, focusKey string) {
	if st.Model == nil {
		st.Model = newModel(plan, focusKey)
		return
	}
	st.Model.SetPanels(plan.Panels, plan.Mnemonics)
}
