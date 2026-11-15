---
title: Windows and the status line
topics: gtf-windows, gtf-status, panels-screen
section: Using gtf
order: 50
---
# Windows and the status line

The screen has three parts, from top to bottom, plus optional panels around them.

```
+----------------------------------------+
| output window: the text of the world   |
| in front                               |
|                                        |
+----------------------------------------+
| status line   (world)  More 12   09:21 |
+----------------------------------------+
| input window: what you are typing      |
+----------------------------------------+
```

## The output window

Each world has its own output window and keeps its own text, so switching worlds switches what you see — [[Several connections]]. Long lines are wrapped for you. Wrapping is controlled by the variables `wrap` (on by default), `wrapsize`, `wrapspace` and `wrappunct`; `/set wrap=off` stops it. gtf follows Unicode line-breaking rules, so wide and combining characters wrap correctly.

Scrolling back is Page Up and Page Down. With `/more on` the window pauses at the end of each screenful and the status line shows `More` with the number of lines waiting; Tab shows the next page. `/more off` turns the pager off again, and `/help tf /more` has the rest.

`/visual off` switches to non-visual mode, where gtf stops drawing the three-part screen and just prints lines in the terminal; `/visual on` returns.

## The input window

Below the status line. It is `isize` lines tall (three by default) and holds the line you are typing. The last few lines you entered stay visible above it. A long line wraps inside the window. When the game asks for a password, gtf hides what you type.

## The status line

The status line is one row, normally with the world name, the pager state (`More`), activity in other worlds, the clock and a few more fields. A dead socket shows as `!name`, and an alert from a background world appears in it for a few seconds — [[Several connections]]. Its contents are a list of fields you can change:

```
/status_add -A@world hp:4     add a field showing the variable hp
/status_edit name ...         change one
/status_rm hp                 remove one
/status_clear                 remove all
/status_defaults              put the standard ones back
```

`status_height` sets how many rows it uses. `/help tf status` and `/help tf /status_add` describe the field syntax.

## Panels

On top of this, gtf can draw **panels** (sidebars, top and bottom bars) around the output window. They are off until you define them, and then they are driven by data the game sends or by your own variables. See [[Panels and layouts]]. When a game uses the console protocol, gtf can replace the output area with a console screen: [[CMCP console protocol]].

There is no mouse support in the ordinary screen. (While a console screen is up, the mouse wheel scrolls; see [[CMCP settings]].)
