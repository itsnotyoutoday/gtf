---
title: Key bindings
topics: gtf-keys, key-bindings, keybindings-gtf
section: Using gtf
order: 90
---
# Key bindings

gtf's keys come from a script, the TinyFugue key library, which runs unchanged. That means every binding is an ordinary macro you can list, replace or remove, and keys you add yourself work the same way as the built-in ones.

## The default keys

| key | action |
|---|---|
| Left, Right | move the cursor |
| Ctrl-A, Home / Ctrl-E, End | start and end of the line |
| Ctrl-Left, Ctrl-Right | move by a word (so do Esc b and Esc f) |
| Backspace, Ctrl-D, Delete | delete backward and forward |
| Ctrl-K | delete to the end of the line |
| Ctrl-U | delete to the start of the line |
| Ctrl-W, Esc Backspace | delete the previous word |
| Esc d | delete the next word |
| Esc u, Esc l, Esc c | upper-case, lower-case and capitalise the word |
| Ctrl-T | swap the two characters around the cursor |
| Ctrl-V, Ctrl-Q | insert the next key literally |
| Up, Down, Ctrl-P, Ctrl-N | previous and next typed line (Up and Down move inside a wrapped multi-line entry first) |
| Ctrl-Up, Ctrl-Down | previous and next typed line, always |
| Esc p, Esc n | search backward and forward through typed lines |
| Esc <, Esc > | oldest and newest typed line |
| Page Up, Page Down | scroll the output window back and forward |
| Tab | show the next page when output is held by the pager |
| Esc h | half a page forward |
| Esc j | throw away the text held by the pager |
| Esc Ctrl-N, Esc Ctrl-P | scroll one line |
| Ctrl-L | redraw the screen |
| Ctrl-S | pause output |
| Esc `{`, Esc `}`, Esc Left, Esc Right | previous and next world |
| Ctrl-] | send the current world to the background |
| Ctrl-X Ctrl-R | read `~/.tfrc` again |
| Ctrl-X Ctrl-V | show the version |
| Esc L | toggle the output limit |
| F1 | `/help` |

Esc means the Escape key pressed before the next key. Many terminals can be set to send Escape for their Alt or Option key, which makes Esc b the same as Alt-B. The exact sequences gtf understands for arrow keys, Home, End and the function keys are xterm's, together with the VT220 and application-mode forms; gtf does not read your terminal's termcap entry.

A number typed with Esc and a digit (Esc 5) repeats the next command that many times for keys that support it.

## Changing and adding keys

```
/bind ^Xh = /echo hello       bind Ctrl-X then h
/bind ^[OS = /cmcp focus      bind a function key's escape sequence
/listbind                     list your bindings
/unbind ^Xh                   remove one
/dokey recallb                run a built-in key action by name
```

`^X` means Ctrl-X and `^[` is the Escape key. `/dokey` has a list of built-in actions (`/help tf /dokey`), and `/help tf /bind` describes the notation, including `\27` and `\033` for arbitrary characters. A binding's command is a macro body, so it may do anything a macro can. Put the bindings you want in your startup file — [[Configuration and startup files]].

If a key does nothing or types odd characters, your terminal probably sends a sequence gtf does not know. Bind that sequence to the action you want, or see `/help tf kbbind`.

## Keys that belong to other features

* CMCP console screens add their own keys (Tab and F2 into the screen, F3, F4, F5, F6, Alt-A): [[CMCP keys and commands]].
* A remote client attached to the [[Squatter]] does not use these keys at all.

See also: [[Getting around]].
