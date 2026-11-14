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

## Implementation

`internal/layout` (no terminal I/O): `ParseTemplate`, `Ctx.Eval` -> styled cells, `Layout`
(`Set`, `Compute(w, h)` -> frame, `Render(panel)` -> `*Grid`, `Changed(key)` dirty tracking),
`internal/ui` composes the grids with the scrollback, `internal/client/layoutcmds.go` implements the commands.
