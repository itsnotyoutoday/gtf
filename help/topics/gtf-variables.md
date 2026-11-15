---
title: gtf variables
topics: gtf-variables, variables-gtf
section: Reference
order: 50
---
# gtf variables

Variables are set with `/set name=value`, shown with `/set name`, and used as `%name` — [[Configuration and startup files]]. This page covers the variables that gtf sets itself and the TinyFugue ones that shape gtf's screen. The squatter and console protocol have settings of their own: [[Squatter settings]] and [[CMCP settings]]. The rest of the language's variables are in the TinyFugue manual (`/help tf variables`).

## Set by gtf (read only for practical purposes)

| variable | holds |
|---|---|
| `version` | gtf's version line, for example `GoTinyFugue version 0.0.2 (gtf), compatible with TinyFugue 5.0 beta 8` |
| `TFVERSION` | the TinyFugue release gtf is compatible with (`5.0 beta 8`); what `/ver` returns |
| `TFLIBDIR` | the folder of the standard library files (`tf/` in the library) |
| `GTFLIBDIR` | the folder of gtf's own scripts and demos |
| `TFHELP` | the bundled TinyFugue manual |
| `HOME` | your home folder |
| `TERM` | your terminal type, from the environment |

After a `/restart` these are refreshed to describe the new program.

## Screen and text

These are TinyFugue variables that gtf reads and obeys. gtf looks at them each time it draws, so a `/set` has an immediate effect.

| variable | default | meaning |
|---|---|---|
| `more` | off | pause output at the end of a screenful — [[Getting around]] |
| `isize` | 3 | height of the input window, in lines |
| `status_height` | 1 | number of rows of the status line |
| `visual` | on | the three-part screen; `off` is non-visual mode (`/visual`) |
| `wrap` | on | wrap long lines; `wrapsize` sets the width, `wrapspace` the indent of the continuation, `wrappunct` also affect where lines break |
| `login` | on | log in automatically when connecting (`-l` turns it off for one run) |
| `textdiv` | on | print a divider before text that arrived while a world was in the background; `off` or `clear` stops it |
| `textdiv_str` | `=====` | the divider |
| `alert_attr` | `Br` | how the status-line alert looks (bold, reverse) |
| `alert_time` | 5 | how many seconds it stays |
| `virtscreen` | on | one window per world. `off` (a shared window) is not implemented |

At startup gtf sets `wrapsize` to the width of your terminal minus one and `visual` on. If you resize the window it follows.

## Other places to look

* `/set` with no arguments lists every variable, and `/listvar` searches them.
* [[Commands and variables index]] points to the topic for each gtf setting.
* Variables can be tested by scripts and used in panel templates as `{var:name}` — [[Panel templates]].
