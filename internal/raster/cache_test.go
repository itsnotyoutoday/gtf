// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package raster

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCacheHitsAndKeys(t *testing.T) {
	c := NewCache(4)
	s := loadSprite(t, "saucer")
	a, err := c.Cells(s, 24, 10, nil, Options{})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := c.Cells(s, 24, 10, nil, Options{})
	if a != b {
		t.Error("second request should return the cached grid")
	}
	// a re-parsed identical sprite shares the entry (content hash, not pointer)
	b, _ = c.Cells(loadSprite(t, "saucer"), 24, 10, nil, Options{})
	if a != b {
		t.Error("an identical sprite should hit the same entry")
	}
	if h, m := c.Stats(); h != 2 || m != 1 {
		t.Errorf("hits %d misses %d", h, m)
	}
	// anything that changes the picture is a miss
	for name, f := range map[string]func() (*Cells, error){
		"size":  func() (*Cells, error) { return c.Cells(s, 25, 10, nil, Options{}) },
		"mode":  func() (*Cells, error) { return c.Cells(s, 24, 10, nil, Options{Mode: Quadrant}) },
		"depth": func() (*Cells, error) { return c.Cells(s, 24, 10, nil, Options{Depth: ANSI16}) },
		"zone": func() (*Cells, error) {
			return c.Cells(s, 24, 10, &Params{Zones: map[string]float64{"hull": 0.5}}, Options{})
		},
		"glow": func() (*Cells, error) {
			return c.Cells(s, 24, 10, &Params{Glows: map[string]float64{"beacon": 0}}, Options{})
		},
		"sprite2": func() (*Cells, error) { return c.Cells(loadSprite(t, "planet"), 24, 10, nil, Options{}) },
	} {
		_, m0 := c.Stats()
		if _, err := f(); err != nil {
			t.Fatal(err)
		}
		if _, m1 := c.Stats(); m1 != m0+1 {
			t.Errorf("%s: expected a miss", name)
		}
	}
	// the same params in a different map order are the same key
	p1 := &Params{Zones: map[string]float64{"a": 1, "b": 0.5}}
	p2 := &Params{Zones: map[string]float64{"b": 0.5, "a": 1}}
	if paramsKey(p1) != paramsKey(p2) {
		t.Error("params key depends on map order")
	}
	// bounded size, least recently used out
	if c.Len() > 4 {
		t.Errorf("cache holds %d > 4", c.Len())
	}
	if _, err := c.Cells(s, 0, 0, nil, Options{}); err == nil {
		t.Error("errors must pass through")
	}
}

func TestPerformance(t *testing.T) {
	if testing.Short() || raceDetector {
		t.Skip("timing")
	}
	// 120x40 cells in half-block mode is 120x80 pixels; sextant is 240x120
	for _, n := range []string{"saucer", "warbird", "planet"} {
		s := loadSprite(t, n)
		for _, o := range []Options{{}, {Mode: Quadrant}, {Mode: Sextant, HasBG: true, Background: Color{10, 10, 10, 255}}} {
			RenderCells(s, 120, 40, nil, o, nil) // warm
			const N = 30
			start := time.Now()
			for i := 0; i < N; i++ {
				if _, err := RenderCells(s, 120, 40, nil, o, nil); err != nil {
					t.Fatal(err)
				}
			}
			per := time.Since(start) / N
			t.Logf("%-8s mode=%d 120x40 cells: %v per render", n, o.Mode, per)
			if per > 25*time.Millisecond { // the target is ~5ms; the test bound is loose so loaded CI does not flake
				t.Errorf("%s: %v per render", n, per)
			}
		}
		c := NewCache(8)
		c.Cells(s, 120, 40, nil, Options{})
		start := time.Now()
		const M = 20000
		for i := 0; i < M; i++ {
			c.Cells(s, 120, 40, nil, Options{})
		}
		per := time.Since(start) / M
		t.Logf("%-8s cache hit: %v", n, per)
		if per > 20*time.Microsecond {
			t.Errorf("%s: cache hit %v", n, per)
		}
	}
}

func BenchmarkRender120x40(b *testing.B) {
	for _, n := range []string{"saucer", "warbird", "planet"} {
		s := loadSprite(b, n)
		b.Run(n+"/half", func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				RenderCells(s, 120, 40, nil, Options{}, nil)
			}
		})
		b.Run(n+"/sextant", func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				RenderCells(s, 120, 40, nil, Options{Mode: Sextant}, nil)
			}
		})
		b.Run(n+"/cachehit", func(b *testing.B) {
			c := NewCache(4)
			c.Cells(s, 120, 40, nil, Options{})
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				c.Cells(s, 120, 40, nil, Options{})
			}
		})
	}
}

// Rendering is bit-for-bit the same on every platform; the digests below were
// recorded on arm64 and are checked on amd64 in CI (go test -update rewrites them).
func TestCrossPlatformDigests(t *testing.T) {
	var got strings.Builder
	for _, n := range []string{"saucer", "warbird", "planet", "geometry"} {
		s := loadSprite(t, n)
		for _, sz := range [][2]int{{37, 23}, {160, 90}, {301, 177}} {
			b, err := s.Render(sz[0], sz[1], &Params{Zones: map[string]float64{"hull": 0.4, "land": 1}}, nil)
			if err != nil {
				t.Fatal(err)
			}
			fmt.Fprintf(&got, "%s %dx%d %x\n", n, sz[0], sz[1], sha256.Sum256(b.Pix))
		}
	}
	path := filepath.Join("testdata", "digests.txt")
	if *update {
		if err := os.WriteFile(path, []byte(got.String()), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(want) != got.String() {
		t.Errorf("pixel output changed (platform difference or an intended change; go test -update records it):\nwant:\n%s\ngot:\n%s", want, got.String())
	}
}
