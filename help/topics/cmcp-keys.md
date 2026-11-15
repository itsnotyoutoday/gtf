---
title: CMCP keys and commands
topics: cmcp-keys, /cmcp, cmcp-commands
section: Console protocol
order: 20
---
# CMCP keys and commands

Everything on a console screen can be done from the keyboard, and the input line always keeps working. gtf's status line (the row above the world name and clock) always names the area you are in and the keys that work there. `/cmcp keys` prints the table below.

## Moving around

The screen has **areas**, in reading order: the screen menu on the left, then the screen's panels. The input line is outside them.

| where | key | does |
|---|---|---|
| input line | Tab (on an empty line) or F2 | go to the screen menu; with text typed, Tab keeps its usual meaning |
| input line | Shift-Tab | go to the last panel |
| anywhere | Tab, Shift-Tab | next and previous area or panel |
| anywhere | Esc | back to the input line (so does a printable key the area does not use, which is then typed) |
| menu | Up, Down, Home, End, PgUp, PgDn | move the highlight |
| menu | Enter | open the highlighted screen; one that needs another console says `man <console> to use` |
| panel list or table | Up, Down | move the row |
| panel | Left, Right | switch tabs, move between buttons, move sliders (and radar contacts, helm values) |
| panel | Enter | press the button or select the row |
| panel | an underlined letter | press that button or toggle |
| in focus | `[`, `]`, `1` to `9` | previous, next and numbered screen |

## Other keys

| key | does |
|---|---|
| F3 | log view: the output grows to about 80% of the height and every other panel shrinks to one summary line; again to go back |
| F4 | hide the screen: the text gets the whole area; again to bring it back |
| Page Up, Page Down, mouse wheel | scroll the game output; the frame stays put and the hint row says `SCROLLED BACK n lines`; new text does not pull the view down, typing or scrolling back down returns to live |
| F5 or Alt-A | open the alert chooser at the next level (green, yellow, red): Left and Right choose, Enter sends it, Esc cancels |
| F6 | close the console: gtf asks the game, and the console is unmanned or the room screen hidden once the game agrees |
| F5 and F6 | while a pack question is pending, allow and refuse it |
| Alt-, and Alt-. | make the game output narrower or wider by 4 columns (side layout) |

A control that needs you to hold it (for example the transporter's energize) takes its key **three times within two seconds**, because a terminal cannot tell when a key is released. Shift-Arrow and Alt-Arrow are never used, since terminals and tmux often swallow them.

To change the key that enters the screen, set `%cmcp_key_focus` (`^I` by default, or `F2`, `^[OQ`, a control letter `^X`), or bind your own: `/bind ^[OS = /cmcp focus`. The keys for F3, F4, F6 and the width have `%cmcp_key_log`, `%cmcp_key_hide`, `%cmcp_key_close`, `%cmcp_key_narrow` and `%cmcp_key_widen`.

## The /cmcp command

```
/cmcp [status]           handshake, ship, console, screen
/cmcp keys               the key table
/cmcp screens            every screen, and which console provides it
/cmcp screen [id]        which screen is shown; with an id,
                         show that one locally
/cmcp send VERB k=v ...  send a command, as a button would
/cmcp focus              enter the screen's keyboard focus (like F2)
/cmcp log, hide, show    F3 and F4 by name
/cmcp close              F6 by name
/cmcp width COLS|auto|+N|-N   width of the game output
/cmcp themes             the themes gtf has and the one in use
/cmcp packs              layout packs known for this world
/cmcp pack allow|deny [id]    answer a pack question
/cmcp on, off            the %cmcp setting
```

Every action is also a typeable command and echoes itself, so a macro or key binding can do what a button does. The choice of screen in the menu is local: the game is not told, and it ends when you change console, hop to another ship or disconnect.

See also: [[CMCP console protocol]], [[CMCP settings]], [[Key bindings]].
