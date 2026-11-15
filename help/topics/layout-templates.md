---
title: Panel templates
topics: templates, panel-templates, gtf-templates
section: Layouts and graphics
order: 20
---
# Panel templates

The text of a panel, after the `=` in `/panel`, is a template: plain text with `{...}` tags. Write `\{` and `\}` for literal braces, `\\` for a backslash, and `\n` or `\t` for a new line or tab. A layout line is one script line, so `\n` is how you start another row inside a panel.

## Inserting values

| tag | inserts |
|---|---|
| `{gmcp:Char.Vitals.hp}` | a GMCP value; arrays count from 0, as in `{gmcp:Room.Info.exits.0}` |
| `{msdp:HEALTH}`, `{mssp:NAME}` | an MSDP or MSSP value |
| `{var:name}` or `{name}` | a variable |
| `{expr:hp*100/maxhp}` | the result of an expression |

Add filters after a `|`: `{name|upper}`. Available filters are `upper lower title trim int round k default:X pad:N rpad:N trunc:N`, and printf-style forms such as `%5d` or `%.1f`.

## Conditions and loops

```
{if gmcp:Room.Info.name}{bold}{gmcp:Room.Info.name}{/bold}{/if}
{if hp < 20}LOW{elif hp < 50}hurt{else}ok{/if}
{if hp < 20}{color red}LOW{/color}{/if}
{each gmcp:Room.Info.exits}{#}: {.}\n{/each}
```

A condition is a reference, `not` a reference, or a comparison (`== != < <= > >= =~`), joined with `and` and `or`. Compare text with quotes (`{if side == "red"}`). Inside `{each}`, `{.}` is the item, `{.field}` a field of it and `{#}` its position or key.

## Style

`{color red}...{/color}`, `{color white on blue}`, `{bg red}`, `{bold}`, `{dim}`, `{italic}`, `{underline}`, `{reverse}`. Close every `{color}` in the same `{if}` branch that opened it. Colours are names, `#rrggbb` or `256:N`; gtf reduces them to what your terminal can show.

## Bars and small graphs

| tag | draws |
|---|---|
| `{bar CUR MAX width=20 color=red}` | a bar; `color=auto` goes green, yellow and red as it empties; also `bg=`, `empty=`, `fill=`, `chars=ascii`, `gradient=#from,#to` |
| `{gauge CUR MAX width=12 label=HP}` | `HP [████░░] 45/60`; options `brackets=off`, `nums=off`, `pct=on` |
| `{spark REF width=16}` | a sparkline of the recent values of a number |
| `{scope REF width=30 height=3}` | a taller line graph of recent values |
| `{hr}`, `{br}` | a rule across the panel, a line break |

CUR, MAX and REF are references: `gmcp:...`, `var:...`, a bare variable name, or a number; quote references with spaces (`"expr:hp * 2"`).

## Pictures

| tag | draws |
|---|---|
| `{radar contacts=REF sweep=REF width=25 height=13}` | a round radar with a sweeping beam, range rings and glowing contacts; contacts come from a variable of `id:bearing:range:type;...` entries |
| `{canvas NAME}` | a drawing surface made with `/canvas` — [[Canvases and sprites]] |
| `{sprite NAME}` | an (experimental) vector picture |
| `{ansi REF}` | the text in a variable, with its colour codes (24-bit, 256-colour, bold, reverse) interpreted |

Everything that is drawn with colour needs a terminal that supports it; true colour (24-bit) is best.

## When panels redraw

gtf notes which data each panel read. When GMCP or MSDP data arrives, only the panels that use that package are redrawn; panels that use variables or expressions are checked after every command, timer and trigger, so `/set hp=10` updates a bar at once.

See also: [[Panels and layouts]].
