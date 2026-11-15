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
