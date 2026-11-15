---
title: Panels and layouts
topics: layouts, layout, /layout, /panel, panels, gtf-layouts
section: Layouts and graphics
order: 10
---
# Panels and layouts

A **panel** is a box that gtf draws beside or above the normal output window: a sidebar with health bars, a bar of room information along the top, a list of who is online. A **layout** is a named set of panels. With no panel defined, the screen is the ordinary three-part screen — [[Windows and the status line]].

Panels show data from the game (GMCP and MSDP, see [[What gtf adds to scripting]]) or from your own variables, and they redraw when the data changes.

## Try one

```
/layout use sidebar
```

shows a ready-made sidebar with health, mana and experience bars, the room name and its exits. It fills in as the game sends GMCP data. `/layout off` removes it.

To make your own, define a panel with `/panel`:

```
/panel -dock left -w 24 -border rounded -title Vitals \
    vitals = HP {bar "gmcp:Char.Vitals.hp" "gmcp:Char.Vitals.maxhp" \
    width=12 color=auto} {gmcp:Char.Vitals.hp}
```

(Type it on one line; the backslashes show where a script file may continue a line.) The text after `=` is a **template**: ordinary text with `{...}` tags that insert data. See [[Panel templates]].

## /panel

`/panel [options] name [= template]` defines or changes a panel. A new panel needs a template.

| option | meaning |
|---|---|
| `-dock left`, `right`, `top`, `bottom` (`-d`) | which side of the output window; default left |
| `-w W`, `-h H` | size: `24` cells, `30%` of the terminal, `*` or `2*` to share what is left; leave out to fit the content |
| `-title T` | a title in the border |
| `-border single`, `rounded`, `double`, `ascii`, `none` | the box |
| `-minw N` | hide the panel when the terminal is narrower than N columns |
| `-fg C`, `-bg C` | default colours: a name, `#rrggbb` or `256:N` |
| `-wrap`, `-nowrap`, `-hidden`, `-shown` | wrapping and visibility |
| `-del name`, `-show name`, `-hide name`, `-toggle name` | act on a panel |
| `-list`, `-clear` | list the panels, or remove them all |

Panels are placed in the order you define them, the first one outermost. A side panel placed first runs the full height of the output area; the status line and input window always span the full width at the bottom. gtf skips a panel (it is not an error) when the terminal is narrower than its `-minw`, or when showing it would leave the output narrower than 30 columns or shorter than 3 rows. `/panel -list` says `no room` for a panel it had to skip. Removing the last panel brings back the plain screen.

## /layout

| | |
|---|---|
| `/layout use NAME` | load a layout by name (or a `NAME.layout` file) and show it |
| `/layout use -w WORLD NAME` | use that layout only when WORLD is in front |
| `/layout new NAME` | start a new, empty layout |
| `/layout save` | write the current layout to `~/.gtf/layouts/NAME.layout` |
| `/layout save FILE` | write it to FILE |
| `/layout load FILE`, `/layout reload` | read a layout file; read the active one again |
| `/layout list`, `show`, `dirs` | what is loaded; the current definition as script; where layouts are searched |
| `/layout delete NAME`, `clear`, `off` | remove one, remove all panels, switch off |

A layout file is an ordinary script made of `/layout new` and `/panel` lines, so it may also contain macros and `/if`. Layouts are looked for in the current folder, in `~/.gtf/layouts`, and in the layouts gtf ships (`sidebar` today). Each world can have its own layout, and the layout changes when the world in front changes.

`/sidebar` and `/sidebar off` show and hide the stock sidebar. `/panelhp name HPVAR MAXVAR` makes a one-line health gauge along the top.

## Restart and panels

Panels and layouts survive [[Restarting without dropping connections]]; the pixels of canvases do not — [[Canvases and sprites]].

See also: [[Panel templates]], [[Canvases and sprites]], [[CMCP console protocol]].
