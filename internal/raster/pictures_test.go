// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package raster

import (
	"image/color"
	"os"
	"path/filepath"
	"testing"
)

// RASTER_PNG=dir writes pictures of the sample sprites in every mode, for looking at.
func TestWritePictures(t *testing.T) {
	dir := os.Getenv("RASTER_PNG")
	if dir == "" {
		t.Skip("set RASTER_PNG=dir to write pictures")
	}
	os.MkdirAll(dir, 0o755)
	panel := color.RGBA{14, 16, 24, 255}
	for _, name := range []string{"saucer", "warbird", "planet"} {
		s := loadSprite(t, name)
		for _, m := range []struct {
			n string
			o Options
		}{
			{"half", Options{Mode: HalfBlock}},
			{"half_bg", Options{Mode: HalfBlock, HasBG: true, Background: Color{14, 16, 24, 255}}},
			{"quad", Options{Mode: Quadrant, HasBG: true, Background: Color{14, 16, 24, 255}}},
			{"sext", Options{Mode: Sextant, HasBG: true, Background: Color{14, 16, 24, 255}}},
			{"braille", Options{Mode: Braille}},
			{"ascii", Options{Mode: ASCII}},
			{"c256", Options{Mode: HalfBlock, Depth: Indexed256, HasBG: true, Background: Color{14, 16, 24, 255}}},
			{"c16", Options{Mode: HalfBlock, Depth: ANSI16, HasBG: true, Background: Color{14, 16, 24, 255}}},
		} {
			c, err := RenderCells(s, 40, 20, nil, m.o, nil)
			if err != nil {
				t.Fatal(err)
			}
			writePNG(t, filepath.Join(dir, name+"_"+m.n+".png"), cellsImage(c, 12, 24, panel))
		}
		buf, err := s.Render(300, 300, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		writePNG(t, filepath.Join(dir, name+"_px.png"), buf.Image())
	}
	// zones and glows: the saucer damaged and with engines off
	s := loadSprite(t, "saucer")
	p := &Params{Zones: map[string]float64{"port": 1, "hull": 0.35, "bridge": 0.8}, Glows: map[string]float64{"port-engine": 0, "starboard-engine": 1}}
	buf, err := s.Render(300, 210, p, nil)
	if err != nil {
		t.Fatal(err)
	}
	writePNG(t, filepath.Join(dir, "saucer_damaged.png"), buf.Image())
}
