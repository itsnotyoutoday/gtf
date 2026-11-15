// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package cmcpui

import "math"

// hash01 is a deterministic pseudo-random value in 0..1 for an integer seed.
func hash01(i int) float64 { return float64(hash(i)) / float64(1<<32) }

// noise is smooth value noise in 0..1.
func noise(x, y float64, seed int) float64 {
	xi, yi := int(math.Floor(x)), int(math.Floor(y))
	fx, fy := x-math.Floor(x), y-math.Floor(y)
	sm := func(t float64) float64 { return t * t * (3 - 2*t) }
	h := func(a, b int) float64 { return hash01(seed + a*73856093 ^ b*19349663) }
	a, b := h(xi, yi), h(xi+1, yi)
	c, d := h(xi, yi+1), h(xi+1, yi+1)
	u, v := sm(fx), sm(fy)
	return (a*(1-u)+b*u)*(1-v) + (c*(1-u)+d*u)*v
}
