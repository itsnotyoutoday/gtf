// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Rick Bird

package client

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/itsnotyoutoday/gtf/internal/layout"
	"github.com/itsnotyoutoday/gtf/internal/raster"
	"github.com/itsnotyoutoday/gtfparser"
)

// EXPERIMENTAL: vector sprites. The rasteriser (internal/raster) is
// format-agnostic; this is the debug front end that feeds it a JSON file.
//
//	/sprite FILE [W H] [-m half|quad|sext|braille|ascii] [-c tc|256|16|mono]
//	        [-b #rrggbb] [-z id=v,id=v] [-g id=v,id=v] [-p NAME]
//	/sprite -d NAME        forget a panel sprite
//
// Renders the sprite at W x H cells (default 40 x 20) into the output, or with
// -p NAME into a panel ({sprite NAME}). -b blends edges into that background
// colour (without it, half-transparent pixels are dropped). -z sets zone tints
// (0..1), -g glow intensities. The sprite file format is documented in
// internal/raster (Sprite).
func (c *Client) cmdSprite(in *gtfparser.Interp, args string) gtfparser.Value {
	fail := func(f string, a ...any) gtfparser.Value {
		in.Warn("sprite: " + fmt.Sprintf(f, a...))
		return gtfparser.Int(0)
	}
	w := splitWords(args)
	var pos []string
	opt := raster.Options{}
	par := &raster.Params{}
	panel := ""
	for i := 0; i < len(w); i++ {
		a := w[i]
		next := func() (string, bool) {
			if i+1 < len(w) {
				i++
				return w[i], true
			}
			return "", false
		}
		switch a {
		case "-d":
			n, ok := next()
			if !ok {
				return fail("usage: /sprite -d NAME")
			}
			delete(c.panels.sprites, n)
			c.spriteChanged(n)
			return gtfparser.Int(1)
		case "-p":
			v, ok := next()
			if !ok {
				return fail("-p needs a name")
			}
			panel = v
		case "-m":
			v, _ := next()
			m, ok := map[string]raster.Mode{"half": raster.HalfBlock, "quad": raster.Quadrant, "sext": raster.Sextant, "braille": raster.Braille, "ascii": raster.ASCII}[v]
			if !ok {
				return fail("-m wants half, quad, sext, braille or ascii")
			}
			opt.Mode = m
		case "-c":
			v, _ := next()
			d, ok := map[string]raster.Depth{"tc": raster.TrueColor, "256": raster.Indexed256, "16": raster.ANSI16, "mono": raster.Mono}[v]
			if !ok {
				return fail("-c wants tc, 256, 16 or mono")
			}
			opt.Depth = d
		case "-b":
			v, _ := next()
			col, err := raster.ParseColor(v)
			if err != nil {
				return fail("-b: %v", err)
			}
			opt.Background, opt.HasBG = col, true
		case "-z", "-g":
			v, _ := next()
			m, err := parseValueMap(v)
			if err != nil {
				return fail("%s: %v", a, err)
			}
			if a == "-z" {
				par.Zones = m
			} else {
				par.Glows = m
			}
		default:
			pos = append(pos, a)
		}
	}
	if len(pos) == 0 {
		return fail("usage: /sprite FILE [W H] [-m half|quad|sext|braille|ascii] [-c tc|256|16|mono] [-b #rrggbb] [-z id=v,..] [-g id=v,..] [-p NAME]")
	}
	cw, ch := 40, 20
	if len(pos) >= 3 {
		x, e1 := strconv.Atoi(pos[1])
		y, e2 := strconv.Atoi(pos[2])
		if e1 != nil || e2 != nil {
			return fail("W and H must be whole numbers of cells")
		}
		cw, ch = x, y
	}
	cells, err := c.renderSpriteFile(pos[0], cw, ch, par, opt)
	if err != nil {
		return fail("%v", err)
	}
	if panel != "" {
		blk := &layout.CellBlock{W: cells.W, H: cells.H, Cells: make([]layout.Cell, len(cells.C))}
		for i, ce := range cells.C {
			blk.Cells[i] = layout.Cell{R: ce.R, A: layout.Attr{FG: layoutColor(ce.FG), BG: layoutColor(ce.BG)}}
		}
		c.panels.sprites[panel] = blk
		c.spriteChanged(panel)
		in.Host.Echo(fmt.Sprintf("%% sprite %s: %dx%d cells (show it with {sprite %s})", panel, cells.W, cells.H, panel))
		return gtfparser.Int(1)
	}
	in.Host.Echo(strings.Join(cells.ANSI(), "\n"))
	return gtfparser.Int(1)
}

// renderSpriteFile loads a sprite file (cached by content) and renders it.
func (c *Client) renderSpriteFile(path string, cw, ch int, par *raster.Params, opt raster.Options) (*raster.Cells, error) {
	data, err := os.ReadFile(expandHome(path, c.cfg.HomeDir))
	if err != nil {
		return nil, err
	}
	spr, err := raster.ParseJSON(data, nil)
	if err != nil {
		return nil, err
	}
	if c.spriteCache == nil {
		c.spriteCache = raster.NewCache(32)
	}
	return c.spriteCache.Cells(spr, cw, ch, par, opt)
}

func expandHome(p, home string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		return home + p[1:]
	}
	return p
}

// parseValueMap reads "id=0.5,other=1".
func parseValueMap(s string) (map[string]float64, error) {
	m := map[string]float64{}
	for _, part := range strings.Split(s, ",") {
		if part == "" {
			continue
		}
		k, v, ok := strings.Cut(part, "=")
		if !ok {
			return nil, fmt.Errorf("%q is not id=value", part)
		}
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return nil, fmt.Errorf("%q: %v", part, err)
		}
		m[k] = f
	}
	return m, nil
}

func layoutColor(t raster.TermColor) layout.Color {
	switch t.Kind {
	case raster.KindANSI:
		return layout.Color{Kind: layout.ANSI, N: t.N}
	case raster.KindIndexed:
		return layout.Color{Kind: layout.Indexed, N: t.N}
	case raster.KindRGB:
		return layout.Color{Kind: layout.RGB, R: t.R, G: t.G, B: t.B}
	}
	return layout.Color{}
}

// spriteChanged tells the active layout that a {sprite NAME} may have changed.
func (c *Client) spriteChanged(name string) {
	if l := c.panels.current(c); l != nil {
		l.Changed("sprite:" + name)
	}
	c.scheduleRender()
}

// canvasSprite is /canvas sprite NAME FILE x y [w h] [zone=id:v,..] ...: the
// sprite is rasterised at w x h pixels (default: the canvas size) and blitted
// at pixel (x, y). Experimental.
func (c *Client) canvasSprite(cv *layout.Canvas, pos []string, opts map[string]string) error {
	if len(pos) < 3 {
		return fmt.Errorf("usage: /canvas sprite NAME FILE x y [w h] [zones=id=v,..] [glows=id=v,..]")
	}
	x, e1 := strconv.Atoi(pos[1])
	y, e2 := strconv.Atoi(pos[2])
	w, h := cv.W, cv.H
	if len(pos) >= 5 {
		var e3, e4 error
		w, e3 = strconv.Atoi(pos[3])
		h, e4 = strconv.Atoi(pos[4])
		if e3 != nil || e4 != nil {
			return fmt.Errorf("w and h must be whole numbers of pixels")
		}
	}
	if e1 != nil || e2 != nil {
		return fmt.Errorf("x and y must be whole numbers of pixels")
	}
	data, err := os.ReadFile(expandHome(pos[0], c.cfg.HomeDir))
	if err != nil {
		return err
	}
	spr, err := raster.ParseJSON(data, nil)
	if err != nil {
		return err
	}
	par := &raster.Params{}
	if s := opts["zones"]; s != "" {
		if par.Zones, err = parseValueMap(s); err != nil {
			return err
		}
	}
	if s := opts["glows"]; s != "" {
		if par.Glows, err = parseValueMap(s); err != nil {
			return err
		}
	}
	buf, err := spr.Render(w, h, par, nil)
	if err != nil {
		return err
	}
	cv.BlitRGBA(x, y, buf.W, buf.H, buf.Pix)
	return nil
}
