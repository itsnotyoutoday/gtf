// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package cmcpui

import (
	"fmt"

	"github.com/itsnotyoutoday/cmcp/go/pulse"
	"github.com/itsnotyoutoday/gtf/internal/layout"
)

// The red-alert pulse of the cell class (spec 19.3, 6.17): the alert roles r and x (the pill, the
// edge, the title accent) go dim for the second half of each period. The phase and level come from the
// core's pulse package; the dim colour is the core's pulse.Colour (blended from the background, raised
// to a 3:1 contrast). The glue decides when to redraw (only when the phase flips).

// PulseSpec is what the renderer needs: whether this frame is the dim phase and how deep it goes.
type PulseSpec struct {
	Dim   bool
	Depth float64 // 1 - level
}

func hexOf(c layout.Color) string {
	c = layout.RGBOf(c)
	return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B)
}

// pulsed returns the theme with the alert roles r and x in their dim phase.
func (t Theme) pulsed(depth float64) Theme {
	bg := hexOf(t.Token("color.bg"))
	out := t
	out.Roles = map[string]string{}
	for k, v := range t.Roles {
		out.Roles[k] = v
	}
	dim := map[string]string{}
	for _, r := range []string{"r", "x"} {
		if v, ok := t.Roles[r]; ok {
			if c, err := layout.ParseColor(v); err == nil {
				out.Roles[r] = pulse.Colour(hexOf(c), bg, 1-depth)
				dim[v] = out.Roles[r]
			}
		}
	}
	out.Cycle = append([]string(nil), t.Cycle...)
	for i, c := range out.Cycle {
		if d, ok := dim[c]; ok {
			out.Cycle[i] = d
		}
	}
	out.Tokens = map[string]string{}
	for k, v := range t.Tokens {
		if d, ok := dim[v]; ok {
			v = d
		}
		out.Tokens[k] = v
	}
	return out
}
