// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package raster

import (
	"math"
	"slices"
)

// FillRule selects how overlapping outlines fill.
const (
	NonZero = "nonzero"
	EvenOdd = "evenodd"
)

// mask is 8-bit coverage over a rectangle of the target.
type mask struct {
	x0, y0, w, h int
	a            []uint8
}

// maxCrossings bounds the scratch memory one shape may use (16 bytes each).
const maxCrossings = 4 << 20

type crossing struct {
	x   float64
	dir int8
}

// scanner turns polygons (device coordinates) into coverage. Vertical
// resolution comes from `sub` sub-scanlines per pixel row; horizontal coverage
// within a sub-scanline is exact. sub is a power of two so all sums of 1/sub
// are exact in floating point, which keeps results identical across platforms.
type scanner struct {
	w, h int
	sub  int
	cnt  []int32
	xs   []crossing
	acc  []float64
	diff []float64
	buf  []uint8
}

func newScanner(w, h, sub int) *scanner {
	return &scanner{w: w, h: h, sub: sub, acc: make([]float64, w+2), diff: make([]float64, w+2)}
}

// coverage rasterises polys into a mask clipped to the target. The returned
// mask aliases scratch memory valid until the next call.
func (s *scanner) coverage(polys [][]pt, evenodd bool, bud *budget) (mask, error) {
	minx, miny := math.Inf(1), math.Inf(1)
	maxx, maxy := math.Inf(-1), math.Inf(-1)
	edges := 0
	for _, p := range polys {
		for _, v := range p {
			if !finite(v.x) || !finite(v.y) {
				return mask{}, invalidErr("non-finite coordinate after transform")
			}
			minx, maxx = math.Min(minx, v.x), math.Max(maxx, v.x)
			miny, maxy = math.Min(miny, v.y), math.Max(maxy, v.y)
		}
		edges += len(p)
	}
	if edges == 0 {
		return mask{}, nil
	}
	x0, x1 := int(math.Max(0, math.Floor(minx))), int(math.Min(float64(s.w), math.Ceil(maxx)))
	y0, y1 := int(math.Max(0, math.Floor(miny))), int(math.Min(float64(s.h), math.Ceil(maxy)))
	if x1 <= x0 || y1 <= y0 {
		return mask{}, nil
	}
	rows := y1 - y0
	K := rows * s.sub
	if cap(s.cnt) < K+1 {
		s.cnt = make([]int32, K+1)
	}
	s.cnt = s.cnt[:K+1]
	clear(s.cnt)
	sub := float64(s.sub)
	// visible sub-scanline range of an edge, relative to y0
	krange := func(ya, yb float64) (int, int) {
		ka := int(math.Ceil(math.Max(ya, float64(y0))*sub-0.5)) - y0*s.sub
		kb := int(math.Ceil(math.Min(yb, float64(y1))*sub-0.5)) - y0*s.sub
		return max(ka, 0), min(kb, K)
	}
	for pass := 0; pass < 2; pass++ {
		if pass == 1 {
			// prefix sums -> start offsets
			var sum int32
			for k := 0; k <= K; k++ {
				c := s.cnt[k]
				s.cnt[k] = sum
				sum += c
			}
			if sum > maxCrossings {
				return mask{}, limitErr("shape needs %d scanline crossings (max %d)", sum, maxCrossings)
			}
			if cap(s.xs) < int(sum) {
				s.xs = make([]crossing, sum)
			}
			s.xs = s.xs[:sum]
		}
		for _, p := range polys {
			n := len(p)
			for i := 0; i < n; i++ {
				a, b := p[i], p[(i+1)%n]
				if a.y == b.y {
					continue
				}
				dir := int8(1)
				if a.y > b.y {
					a, b = b, a
					dir = -1
				}
				ka, kb := krange(a.y, b.y)
				if kb <= ka {
					continue
				}
				if pass == 0 {
					if err := bud.spend(int64(kb-ka) + 1); err != nil {
						return mask{}, err
					}
					for k := ka; k < kb; k++ {
						s.cnt[k]++
					}
					continue
				}
				slope := (b.x - a.x) / (b.y - a.y)
				for k := ka; k < kb; k++ {
					sy := (float64(k+y0*s.sub) + 0.5) / sub
					x := a.x + float64((sy-a.y)*slope)
					s.xs[s.cnt[k]] = crossing{x, dir}
					s.cnt[k]++
				}
			}
		}
		if pass == 1 {
			// cnt[k] now holds the end of bucket k; shift to starts
			for k := K; k > 0; k-- {
				s.cnt[k] = s.cnt[k-1]
			}
			s.cnt[0] = 0
		}
	}
	mw := x1 - x0
	if err := bud.spend(int64(mw) * int64(rows)); err != nil {
		return mask{}, err
	}
	if cap(s.buf) < mw*rows {
		s.buf = make([]uint8, mw*rows)
	}
	out := s.buf[:mw*rows]
	clear(out)
	inv := 1 / sub
	fx0, fx1 := float64(x0), float64(x1)
	for r := 0; r < rows; r++ {
		any := false
		for ss := 0; ss < s.sub; ss++ {
			k := r*s.sub + ss
			cs := s.xs[s.cnt[k]:s.cnt[k+1]]
			if len(cs) < 2 {
				continue
			}
			if len(cs) > 1 {
				slices.SortFunc(cs, func(a, b crossing) int {
					switch {
					case a.x < b.x:
						return -1
					case a.x > b.x:
						return 1
					}
					return int(a.dir) - int(b.dir)
				})
			}
			wind := 0
			for i := 0; i < len(cs)-1; i++ {
				wind += int(cs[i].dir)
				in := wind != 0
				if evenodd {
					in = wind&1 != 0
				}
				if !in {
					continue
				}
				a, b := math.Max(cs[i].x, fx0), math.Min(cs[i+1].x, fx1)
				if b <= a {
					continue
				}
				any = true
				a -= fx0
				b -= fx0
				ia, ib := int(a), int(b)
				if ia == ib {
					s.acc[ia] += (b - a) * inv
					continue
				}
				s.acc[ia] += (float64(ia+1) - a) * inv
				s.acc[ib] += (b - float64(ib)) * inv
				s.diff[ia+1] += inv
				s.diff[ib] -= inv
			}
		}
		if !any {
			continue
		}
		run := 0.0
		row := out[r*mw : (r+1)*mw]
		for x := 0; x < mw; x++ {
			run += s.diff[x]
			c := s.acc[x] + run
			s.acc[x], s.diff[x] = 0, 0
			if c > 0 {
				if c >= 1 {
					row[x] = 255
				} else {
					row[x] = uint8(mul(c, 255) + 0.5)
				}
			}
		}
		s.acc[mw], s.diff[mw], s.acc[mw+1], s.diff[mw+1] = 0, 0, 0, 0
	}
	return mask{x0: x0, y0: y0, w: mw, h: rows, a: out}, nil
}
