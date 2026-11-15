// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package raster

import (
	"errors"
	"fmt"
	"math"
)

// ErrLimit is wrapped by every error that reports a violated limit.
var ErrLimit = errors.New("raster: limit exceeded")

// ErrInvalid is wrapped by every error that reports malformed input.
var ErrInvalid = errors.New("raster: invalid input")

func limitErr(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrLimit, fmt.Sprintf(format, a...))
}

func invalidErr(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, a...))
}

// Limits bound everything the library accepts and everything it does. A nil
// *Limits means DefaultLimits(). Exceeding a limit returns an error wrapping
// ErrLimit; the library never panics and never loops without bound.
type Limits struct {
	MaxJSONBytes int     // size of a JSON sprite document
	MaxPathBytes int     // length of one path-data string
	MaxSegments  int     // path segments in one path (arcs count as their pieces)
	MaxPoints    int     // points in one polygon/polyline
	MaxShapes    int     // shapes (groups included) in the whole sprite
	MaxLayers    int     // layers
	MaxDepth     int     // group/clip nesting (JSON nesting is bounded by a multiple of it)
	MaxStops     int     // gradient stops
	MaxZones     int     // zones plus glows
	MaxCoord     float64 // |any coordinate, radius, width, scale| must not exceed this
	MaxPixelDim  int     // width or height of a render target in pixels
	MaxPixels    int     // width * height of a render target in pixels
	MaxCells     int     // terminal cells in one conversion
	MaxOps       int64   // hard budget on rasteriser work units (crossings, painted pixels, segments)
}

// DefaultLimits are generous for terminal-sized art and small enough that a
// hostile document costs a fraction of a second at most.
func DefaultLimits() Limits {
	return Limits{
		MaxJSONBytes: 1 << 20,
		MaxPathBytes: 256 << 10,
		MaxSegments:  50_000,
		MaxPoints:    50_000,
		MaxShapes:    4096,
		MaxLayers:    64,
		MaxDepth:     12,
		MaxStops:     32,
		MaxZones:     256,
		MaxCoord:     1e6,
		MaxPixelDim:  4096,
		MaxPixels:    1 << 20,
		MaxCells:     1 << 16,
		MaxOps:       32 << 20,
	}
}

func limitsOrDefault(l *Limits) Limits {
	if l == nil {
		return DefaultLimits()
	}
	d := DefaultLimits()
	out := *l
	// zero fields fall back to the default, so a partly filled Limits is safe
	if out.MaxJSONBytes <= 0 {
		out.MaxJSONBytes = d.MaxJSONBytes
	}
	if out.MaxPathBytes <= 0 {
		out.MaxPathBytes = d.MaxPathBytes
	}
	if out.MaxSegments <= 0 {
		out.MaxSegments = d.MaxSegments
	}
	if out.MaxPoints <= 0 {
		out.MaxPoints = d.MaxPoints
	}
	if out.MaxShapes <= 0 {
		out.MaxShapes = d.MaxShapes
	}
	if out.MaxLayers <= 0 {
		out.MaxLayers = d.MaxLayers
	}
	if out.MaxDepth <= 0 {
		out.MaxDepth = d.MaxDepth
	}
	if out.MaxStops <= 0 {
		out.MaxStops = d.MaxStops
	}
	if out.MaxZones <= 0 {
		out.MaxZones = d.MaxZones
	}
	if out.MaxCoord <= 0 {
		out.MaxCoord = d.MaxCoord
	}
	if out.MaxPixelDim <= 0 {
		out.MaxPixelDim = d.MaxPixelDim
	}
	if out.MaxPixels <= 0 {
		out.MaxPixels = d.MaxPixels
	}
	if out.MaxCells <= 0 {
		out.MaxCells = d.MaxCells
	}
	if out.MaxOps <= 0 {
		out.MaxOps = d.MaxOps
	}
	return out
}

// budget counts work units; spend returns ErrLimit once the allowance is gone.
type budget struct{ left int64 }

func (b *budget) spend(n int64) error {
	b.left -= n
	if b.left < 0 {
		return limitErr("operation budget exhausted")
	}
	return nil
}

func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

// checkNum rejects NaN, Inf and anything beyond lim.MaxCoord.
func checkNum(what string, v, max float64) error {
	if !finite(v) {
		return invalidErr("%s is not a finite number", what)
	}
	if math.Abs(v) > max {
		return limitErr("%s = %g exceeds %g", what, v, max)
	}
	return nil
}
