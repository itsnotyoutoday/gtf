// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package raster

import (
	"sort"
	"strconv"
	"strings"
	"sync"
)

// Cache keeps rendered cell grids so that drawing an unchanged sprite again
// costs a map lookup. The key is the sprite's content hash, the size, the
// conversion options and the params. Cached *Cells are shared: do not modify.
type Cache struct {
	mu     sync.Mutex
	max    int
	m      map[cacheKey]*cacheEntry
	tick   uint64
	hits   uint64
	misses uint64
	Limits *Limits // applied to cache-miss renders; nil = defaults
}

type cacheKey struct {
	hash   uint64
	cw, ch int
	opt    Options
	par    string
}

type cacheEntry struct {
	cells *Cells
	used  uint64
}

// NewCache makes a cache holding at most max grids (least recently used out).
func NewCache(max int) *Cache {
	if max < 1 {
		max = 32
	}
	return &Cache{max: max, m: map[cacheKey]*cacheEntry{}}
}

func paramsKey(p *Params) string {
	if p == nil || (len(p.Zones) == 0 && len(p.Glows) == 0 && p.Samples == 0) {
		return ""
	}
	var b strings.Builder
	add := func(tag string, m map[string]float64) {
		ks := make([]string, 0, len(m))
		for k := range m {
			ks = append(ks, k)
		}
		sort.Strings(ks)
		for _, k := range ks {
			b.WriteString(tag + k + "=" + strconv.FormatFloat(m[k], 'g', -1, 64) + ";")
		}
	}
	add("z:", p.Zones)
	add("g:", p.Glows)
	b.WriteString("s" + strconv.Itoa(p.Samples))
	return b.String()
}

// Cells returns the sprite rendered at cw x ch cells, from the cache when the
// same request was seen before.
func (c *Cache) Cells(s *Sprite, cw, ch int, p *Params, o Options) (*Cells, error) {
	k := cacheKey{hash: s.Hash(), cw: cw, ch: ch, opt: o, par: paramsKey(p)}
	c.mu.Lock()
	c.tick++
	if e, ok := c.m[k]; ok {
		e.used = c.tick
		c.hits++
		c.mu.Unlock()
		return e.cells, nil
	}
	c.misses++
	c.mu.Unlock()
	cells, err := RenderCells(s, cw, ch, p, o, c.Limits)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.m) >= c.max {
		var oldK cacheKey
		var oldU uint64 = ^uint64(0)
		for kk, e := range c.m {
			if e.used < oldU {
				oldK, oldU = kk, e.used
			}
		}
		delete(c.m, oldK)
	}
	c.m[k] = &cacheEntry{cells: cells, used: c.tick}
	return cells, nil
}

// Stats reports cache hits and misses.
func (c *Cache) Stats() (hits, misses uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.hits, c.misses
}

// Len is the number of cached grids.
func (c *Cache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.m)
}
