// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

// Package raster is a format-agnostic vector rasteriser for terminal art.
// It turns a vector description (Sprite: layers of paths, polygons,
// ellipses, rects and strokes, with solid and gradient fills, opacity,
// transforms, clips, named tint zones and glow anchors, in a viewBox) into
// an anti-aliased RGBA Buffer at any size, and converts the buffer into
// terminal cells (half blocks, quadrants, sextants, Braille or an ASCII
// ramp, in 24-bit, 256, 16 or no colour).
//
// EXPERIMENTAL: the JSON form of Sprite is a deliberately simple stand-in for
// the CMCP sprite format and will be mapped to it; the Go API may change.
//
// Pipeline and API:
//
//	spr, err := raster.ParseJSON(data, nil)             // validates against Limits
//	buf, err := spr.Render(w, h, &raster.Params{...}, nil) // premultiplied RGBA, deterministic
//	cells, err := raster.RenderCells(spr, cw, ch, params, raster.Options{Mode: raster.Quadrant}, nil)
//	lines := cells.ANSI()                               // or walk cells.C yourself
//	cache := raster.NewCache(32)                        // keyed by sprite hash, size, options, params
//	cells, err = cache.Cells(spr, cw, ch, params, opts)
//
// Rendering. Fills use exact horizontal coverage with 4-32 vertical
// sub-scanlines per pixel (Params.Samples, default 8) and the non-zero or
// even-odd rule; strokes are built from per-segment quads plus join and cap
// polygons and filled non-zero. Output is bit-for-bit identical across runs and
// across amd64/arm64 (no fused multiply-add, own sin/cos/atan2); a test pins it.
//
// Robustness. Everything that comes from outside is bounded by Limits
// (document size, path bytes, segments, shapes, nesting, gradient stops,
// target size) and the renderer spends from a hard work budget (Limits.MaxOps);
// non-finite or huge numbers are rejected. Failures are errors wrapping ErrLimit
// or ErrInvalid; the package does not panic or loop without bound on hostile
// input (see robust_test.go, including fuzz targets).
//
// Colours without a terminal background: without Options.Background the
// converter cannot blend anti-aliased edges into the screen, so pixels under
// half alpha are left transparent. Pass the panel colour for soft edges.
package raster
