# Panels and layouts

gtf can draw extra panels (a sidebar, top/bottom bars, boxes) around the normal
tf screen, driven by GMCP/MSDP/TF variables, defined in TF-flavoured text files.
With no panel defined the screen is exactly tf's.

```
/layout use sidebar                       load gtflib/gtf/layouts/sidebar.layout and show it
/panel -dock left -w 24 -border rounded -title Vitals \
       vitals = HP {bar "gmcp:Char.Vitals.hp" "gmcp:Char.Vitals.maxhp" width=12 color=auto} {gmcp:Char.Vitals.hp}
/layout save mine                         write ~/.gtf/layouts/mine.layout
```

## /panel

`/panel [options] name [= template]` defines or modifies a panel (a new panel needs a body).

| option | meaning |
|---|---|
| `-dock left\|right\|top\|bottom` (`-d`) | side of the main region (default left) |
| `-w W`, `-h H` | size: `24` cells, `30%` of the terminal, `*`/`2*` flex (n/4 of it), omitted = fit the content |
| `-title T`, `-border single\|rounded\|double\|ascii\|none` | box |
| `-minw N` | hide the panel when the terminal is narrower than N columns |
| `-fg C`, `-bg C` | default colours (names, `#rrggbb`, `256:N`) |
| `-wrap` / `-nowrap`, `-hidden` / `-shown` | |
| `-del name`, `-show name`, `-hide name`, `-toggle name`, `-list`, `-clear` | |

Panels are placed in declaration order, outermost first, around the main
scrollback region; side panels placed first span the full output height. The status
line and the input window always span the full width at the bottom. A panel is
**skipped** (not an error) when the terminal is narrower than its `-minw`, or when
it would leave the main region narrower than 30 columns (or shorter than 3 rows);
`/panel -list` shows `no room`. Removing the last panel restores the tf look.

## /layout

`/layout [list]`, `new NAME`, `use [-w WORLD] NAME` (a name or a `NAME.layout` file; `-w` assigns it to
one world), `load FILE|NAME`, `reload`, `save [FILE]`, `show`, `delete NAME`, `clear`, `off`, `dirs`.
Layout files are ordinary TF scripts made of `/layout new` and `/panel` lines (so they can also
contain `/def`, `/if`, ...). They are searched in `.`, `~/.gtf/layouts` and the built-in
`gtflib/gtf/layouts`. `/layout reload` re-reads the active layout from its file at run time.
A world can have its own layout (`/layout use -w mymud sidebar`); the layout follows the foreground world.

## Template language

Text with `{...}` tags. `\{ \} \\ \n \t` are escapes (a layout line is one TF line, so `\n` separates rows).

| form | meaning |
|---|---|
| `{gmcp:Char.Vitals.hp}` | GMCP value (dotted path, arrays from 0: `{gmcp:Room.Info.exits.0}`) |
| `{msdp:HEALTH}`, `{mssp:NAME}` | MSDP / MSSP value |
| `{var:name}` or `{name}` | a TF global variable |
| `{expr:hp*100/maxhp}` | a TF expression, evaluated by the interpreter |
| `{ref\|filter\|filter:arg}` | filters: `upper lower title trim int round k default:X pad:N rpad:N trunc:N` and printf specs `%5d %.1f %s` |
| `{if COND}..{elif COND}..{else}..{/if}` | COND: `ref`, `not ref`, `ref OP value` (`== != < <= > >= =~`), joined by `and`/`or` |
| `{each gmcp:Room.Info.exits}{#}={.} {/each}` | iterate an array or object; `{.}` the item, `{.field}` a field, `{#}` index/key |
| `{color red}..{/color}`, `{color white on blue}`, `{bg red}`, `{bold} {dim} {italic} {underline} {reverse}` | styling |
| `{bar CUR MAX width=20 color=red chars=ascii}` | bar; `color=auto` goes green/yellow/red; `bg=`, `empty=`, `fill=`, `blank=`; fractional block characters |
| `{gauge CUR MAX width=12 label=HP}` | `HP [████░░] 45/60` (`brackets=off nums=off pct=on`) |
| `{spark REF width=16}` | sparkline of the recent history of a numeric binding |
| `{hr}`, `{br}` | rule across the panel width / line break |

Widget arguments are references (`gmcp:...`, `var:...`, bare names mean `var:`), numbers, or
quoted strings; quote references that contain spaces (`"expr:hp * 2"`).

### Truecolor widgets

| form | meaning |
|---|---|
| `{radar contacts=REF sweep=REF width=25 height=13 trail=100 rings=3 bg=#rrggbb}` | a Braille (2x4 dots per cell) radar dish with a fading sweep trail, range rings and contacts that glow brightest right after the sweep passes. `contacts` is a variable holding `id:bearing:range:type;...` (bearing in degrees, 0 = up; range 0..100; type `H` hostile `F` friend `N` neutral `U` unknown) |
| `{scope REF width=30 height=3 color=#rrggbb fill=on}` | a multi-row line graph of a numeric binding's recent history (Braille) |
| `{ansi REF}` | renders a variable holding text with SGR escapes (ESC `[38;2;r;g;bm` truecolor, `\e[48;5;Nm` 256-colour, bold, reverse ...); the two characters `\e` can stand in for ESC |
| `{canvas NAME}` | shows a `/canvas` drawing surface |
| `{sprite NAME}` | (experimental) shows a vector sprite rendered with `/sprite FILE -p NAME` |
| `{bar CUR MAX gradient=#from,#to}` | colours the bar along a ramp |

## /canvas

Truecolor drawing surfaces. Each terminal cell holds two vertically stacked pixels (the upper and lower
half blocks), so a W x H cell canvas is W x 2H pixels and pixels are square. Unset pixels show the terminal
background. Show a canvas in a panel with `{canvas NAME}`; any `/canvas` command repaints it.

```
/canvas new NAME W H [bg]            /canvas clear NAME [color]        /canvas del NAME     /canvas list
/canvas px NAME x y color            /canvas line NAME x1 y1 x2 y2 color [t=THICKNESS]
/canvas rect NAME x y w h color      outline          /canvas fill NAME x y w h color
/canvas rrect NAME x y w h r color [corners=tl,tr,bl,br]       rounded rectangle (LCARS pills and elbows)
/canvas circle NAME cx cy r color [t=N]    /canvas disc NAME cx cy r color    /canvas ellipse NAME cx cy rx ry color
/canvas arc NAME cx cy r1 r2 a1 a2 color   ring sector, degrees, 0 = up, clockwise
/canvas poly NAME color x1 y1 x2 y2 x3 y3 ...    filled polygon
/canvas glow NAME cx cy r color            soft additive light
/canvas grad NAME x y w h color1 color2 [v]      colour ramp, left to right (or top to bottom with v)
/canvas stars NAME x y w h count seed [t=PHASE]  a starfield (same seed = same sky; t makes some twinkle)
/canvas text NAME cellx celly color [bold=1] text...   text on whole cells
```

Any drawing command also takes `a=ALPHA` (0..1). Colors are `#rrggbb`, names, or `256:N`. Edges of discs,
rings, ellipses, polygons and rounded corners are anti-aliased. `term_width()` and `term_height()` give the
terminal size, so a script can size its canvases to the screen.

`gtf/examples/lcars.tf` (just type `/lcars`) uses all of this: a TNG-style console with rounded elbows and pill buttons, a starfield
viewscreen with ships, the radar dish, and an animated warp core (`/lcars`, `/lcars eng`).

### Data bindings and redraws

Each render records the bindings it read. When GMCP/MSDP data arrives only panels
depending on the changed package re-render; `var:`/`expr:` bindings are compared after every
unit of interpreter work, so `/set hp=10` updates a bar. Colours are ANSI-16/256/truecolor
and are quantized to the terminal's depth.

## Examples

`gtflib/gtf/layouts/sidebar.layout` (HP/MP/XP bars, room name, exits) and `gtflib/gtf/layout.tf`
(`/sidebar [off]`, `/panelhp name hpref maxref`). A top bar:

```
/layout new bars
/panel -dock top -h 1 status = {bold}{gmcp:Room.Info.name}{/bold} {dim}[{gmcp:Room.Info.exits}]{/dim}
/panel -dock right -w 22 -minw 100 -border single -title Who who = {each gmcp:Comm.Who}{.}\n{/each}
```

Reacting from scripts: `/def -hGMCP -t'Char.Vitals *' ~lowhp = /if (gmcp_get("Char.Vitals.hp") < 20) /echo -aBCred LOW HP%; /endif`.

## Sprites (experimental)

`internal/raster` rasterises vector art (layers of paths, polygons, ellipses, rects, strokes with caps and joins,
solid/linear/radial fills, opacity, transforms, clips, named tint zones and glow anchors in a viewBox) into an
anti-aliased RGBA buffer at any size and converts it to terminal cells. It knows nothing about the CMCP wire
format; the JSON form documented on `raster.Sprite` is a stand-in that is easy to map to the final spec. Samples
are in `internal/raster/testdata/`.

```
/sprite FILE [W H] [-m half|quad|sext|braille|ascii] [-c tc|256|16|mono] [-b #rrggbb] [-z id=v,..] [-g id=v,..] [-p NAME]
/sprite -d NAME
/canvas sprite NAME FILE x y [w h] [zones=id=v,..] [glows=id=v,..]
```

* Without `-p`, `/sprite` prints the picture into the output (W x H cells, default 40 x 20). With `-p NAME` it is stored
  for a panel: `/panel ... = {sprite NAME}`. `-d NAME` forgets it.
* Modes: `half` is half blocks, 1 x 2 pixels per cell (default, works everywhere); `quad` is quadrant blocks, 2 x 2, two
  colours per cell; `sext` is sextants (U+1FB00..), 2 x 3, needs a recent font; `braille` is 2 x 4 dots; `ascii` is a
  density ramp. Colours: `tc` 24-bit (default), `256`, `16`, `mono` (no colour codes).
* `-b` blends anti-aliased edges into the given background colour. Without it, pixels under half coverage are left
  transparent (the terminal's own background) and the rest drawn opaque, so soft edges become hard.
* `-z hull=0.5,port=1` tints the named zones toward their colour (damage overlays); `-g engine=0` switches a glow off.
* `/canvas sprite` rasterises at w x h pixels (default: the canvas) and blits onto a `/canvas` surface at pixel x, y.
* Input is size-limited (1 MB document, 4096 shapes, 12 levels of nesting, 50 000 path segments, 1M pixels) and the
  rasteriser has a hard work budget; hostile files give an error, never a hang.

## Demo

`gtf/examples/layout-demo.tf` is a guided tour of everything above, no MUD connection needed:

```
/layoutdemo          run all 18 scenes, one every 3 seconds
/layoutdemo 7        jump to a single scene
/layoutdemo stop     quit and restore the plain tf screen
```

`gtf/examples/lcars.tf` is a larger example: a TNG-style LCARS bridge console (`/lcars` sensors with a live
radar dish, contact list and forward viewscreen; `/lcars eng` engineering with an animated warp core;
`/lock`, `/pwr`, `/lcars alert red`).
It shows how panels are driven from script variables that you would feed from GMCP/MSDP in a real game.
Note: in `{if}` conditions compare strings with quotes (`{if x == "red"}`), and every `{color}` must be
closed inside the same `{if}` branch that opened it.

## Implementation

`internal/layout` (no terminal I/O): `ParseTemplate`, `Ctx.Eval` -> styled cells, `Layout`
(`Set`, `Compute(w, h)` -> frame, `Render(panel)` -> `*Grid`, `Changed(key)` dirty tracking),
`internal/ui` composes the grids with the scrollback, `internal/client/layoutcmds.go` implements the commands.
