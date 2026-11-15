---
title: Getting around
topics: getting-around, navigation
section: Start here
order: 50
---
# Getting around

This page is the short version: typing, moving through output and switching worlds. [[Key bindings]] has the longer list.

## Typing

Whatever you type goes to the game when you press Enter, unless it starts with `/`, in which case gtf treats it as one of its own commands (`/connect`, `/help`, `/quit` ...). To send a line that really starts with a slash, begin it with `//`. gtf shows the lines you entered above the input line, in the input window, so you can see what you just sent.

The input line is edited like most terminal programs:

| key | does |
|---|---|
| Left, Right, Ctrl-A, Ctrl-E | move; Ctrl-A is the start of the line, Ctrl-E the end |
| Ctrl-Left, Ctrl-Right, Esc b, Esc f | move by a word |
| Backspace, Delete, Ctrl-D | delete a character |
| Ctrl-K | delete to the end of the line |
| Ctrl-U | delete to the start of the line |
| Ctrl-W | delete the word before the cursor |
| Up, Down, Ctrl-P, Ctrl-N | earlier and later lines you typed |
| Esc p, Esc n | search your history for lines starting like what you have typed |
| Ctrl-L | redraw the screen |

## Output and scrollback

New text appears at the bottom of the output window. **Page Up** and **Page Down** move back and forward through what the window remembers. Esc h scrolls half a page.

gtf can also stop the output at the end of a screenful so you do not miss anything. This is the "more" pager. It is off until you turn it on with `/more on` (or `/set more=on`). While it is on, new text that does not fit is held and the status line shows `More` and how many lines are waiting. **Tab** shows the next page, and Esc j throws the held text away. See [[Windows and the status line]].

## Several worlds

Every connection has its own output window. Esc `{` and Esc `}` (or Esc Left and Esc Right) switch to the previous and next world; `/fg name` brings one to the front by name, and `/listsockets` lists them. The status line shows the name of the world in front. More in [[Several connections]].

## Leaving

`/quit` leaves gtf and closes your connections. If you want to replace gtf with a newer copy and keep the connections open, see [[Restarting without dropping connections]]. If you want your connections to survive even when you close the terminal, see [[Squatter]].

## Getting help

`/help` shows the overview, `/help topics` lists what gtf documents, and F1 is `/help` too. For a command that is part of the TinyFugue language, `/help <command>` shows the bundled TinyFugue manual page.
