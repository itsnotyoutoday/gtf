// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package cmcpui

import "testing"

func TestClampTextWidth(t *testing.T) {
	for _, c := range []struct{ inner, want, got int }{
		{150, 84, 84}, {150, 10, 40}, {150, 200, 150 - 40 - 3}, {100, 90, 57}, {60, 70, 40}, {200, 100, 100},
	} {
		got, note := ClampTextWidth(c.inner, c.want)
		if got != c.got {
			t.Errorf("inner %d want %d: got %d (%s), want %d", c.inner, c.want, got, note, c.got)
		}
		if (got != c.want) != (note != "") {
			t.Errorf("inner %d want %d: note %q does not match the clamp", c.inner, c.want, note)
		}
	}
}

func TestHitTest(t *testing.T) {
	f := &Frame{Text: Rect{X: 10, Y: 5, W: 40, H: 20}, Items: []Item{
		{Kind: "table", Rect: Rect{X: 60, Y: 5, W: 30, H: 10}},
		{Kind: "button", Rect: Rect{X: 62, Y: 6, W: 8, H: 1}}, // inside the table: the smaller one wins
		{Kind: "nav", Rect: Rect{X: 0, Y: 0, W: 8, H: 30}},
	}}
	for _, c := range []struct {
		x, y int
		text bool
		item int
	}{{20, 10, true, -1}, {60, 5, false, 0}, {62, 6, false, 1}, {3, 3, false, -1}, {55, 3, false, -1}, {9, 5, false, -1}, {10, 5, true, -1}, {49, 24, true, -1}, {50, 24, false, -1}} {
		if h := f.HitTest(c.x, c.y); h.Text != c.text || h.Item != c.item {
			t.Errorf("(%d,%d): got %+v, want text=%v item=%d", c.x, c.y, h, c.text, c.item)
		}
	}
}
