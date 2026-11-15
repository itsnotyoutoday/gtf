---
title: Canvases and sprites
topics: canvas, /canvas, sprite, /sprite, graphics, gtf-graphics
section: Layouts and graphics
order: 30
---
# Canvases and sprites

A **canvas** is a drawing surface you can fill with shapes, text and colour from a script, and show inside a panel. It needs a terminal with 24-bit colour and UTF-8 for the best results; gtf reduces the colours when the terminal has fewer.

Every terminal cell holds two stacked pixels (the upper and lower half-block characters), so a canvas of W by H cells is W by 2H pixels and the pixels are square. Pixels you have not drawn show the terminal's own background.

## Using a canvas

```
/canvas new radar 30 10 #001018        make a canvas, 30 x 10 cells
/canvas disc radar 15 10 6 #335577     draw on it
/canvas line radar 0 0 29 19 red t=2
/panel -dock right -w 32 view = {canvas radar}
```

Every drawing command repaints the panels that show the canvas. Pixel coordinates start at 0 in the top left corner. Colours are `#rrggbb`, a colour name or `256:N`. Any drawing command also takes `a=ALPHA` (0 to 1) for transparency. Edges of discs, rings, ellipses, polygons and rounded corners are smoothed.

| command | does |
|---|---|
| `/canvas new NAME W H [bg]`, `del NAME`, `list` | create, remove and list canvases |
| `/canvas clear NAME [color]` | wipe it |
| `/canvas px NAME x y color` | one pixel |
| `/canvas line NAME x1 y1 x2 y2 color [t=N]` | a line of thickness N |
| `/canvas rect NAME x y w h color` | an outline |
| `/canvas fill NAME x y w h color` | a filled rectangle |
| `/canvas rrect NAME x y w h r color [corners=tl,tr,bl,br]` | a rounded rectangle; `corners` chooses which are round |
| `/canvas circle NAME cx cy r color [t=N]`, `disc`, `ellipse NAME cx cy rx ry color` | outline, filled, and elliptical shapes |
| `/canvas arc NAME cx cy r1 r2 a1 a2 color` | a ring sector; degrees, 0 is up, clockwise |
| `/canvas poly NAME color x1 y1 x2 y2 ...` | a filled polygon |
| `/canvas glow NAME cx cy r color` | a soft light |
| `/canvas grad NAME x y w h color1 color2 [v]` | a colour ramp, left to right (top to bottom with `v`) |
| `/canvas stars NAME x y w h count seed [t=PHASE]` | a star field; the same seed gives the same sky |
| `/canvas text NAME cellx celly color [bold=1] text` | text on whole cells |

`term_width()` and `term_height()` give the terminal size, so a script can size its canvases to the screen. A canvas keeps its size, but not its pixels, across a [[Restarting without dropping connections]].

## Sprites (experimental)

`/sprite` draws vector pictures (layers of paths, shapes, gradients and tint zones) as terminal characters. It is experimental, and the file format it reads is a stand-in for something more final.

```
/sprite FILE [W H] [-m MODE] [-c COLOURS] [-b #rrggbb]
        MODE: half quad sext braille ascii
        COLOURS: tc 256 16 mono
/sprite FILE -p NAME           keep it for a panel: {sprite NAME}
/sprite -d NAME                forget it
/canvas sprite NAME FILE x y   draw a sprite onto a canvas
```

Without `-p` the picture is printed into the output window. `-m` picks how many pixels fit in a cell (`half` works everywhere, `sext` needs a recent font), `-c` the colour depth, and `-b` blends the smooth edges into a background colour. Files are limited in size and complexity, and a hostile or broken file gives an error, not a hang.

## Examples

Panels that show them: [[Panels and layouts]].
