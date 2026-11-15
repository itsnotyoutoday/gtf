---
title: History and recall
topics: gtf-history, recall-history
section: Using gtf
order: 30
---
# History and recall

gtf remembers two kinds of history: **what you typed** (input history) and **what each world sent** (output history, one per world, plus one for gtf's own messages).

## Lines you typed

Up and Ctrl-P bring back earlier lines, Down and Ctrl-N later ones. Esc p and Esc n search your history for a line that starts like what you have typed so far. `Esc <` and `Esc >` jump to the oldest and newest. A line of the form `^old^new` repeats the last line that contained `old` with `new` in its place. The size of the history is set with `/histsize`.

## Lines the game sent

`/recall` prints lines back into the window. A number on its own means "the last that many lines":

```
/recall 20                  the last 20 lines from this world
/recall 200 treasure        of the last 200, those that match
/recall -w otherworld 30    another world's history
/recall -i 10               your last 10 input lines
/recall -g 50               everything, all worlds and gtf messages
```

Recalled lines are shown between a "Recall start" and a "Recall end" marker so you can tell them from new text. `-t` adds time stamps; `-A`, `-B` and `-C` add context lines around a match. The options and the exact meaning of ranges are in the TinyFugue manual: `/help tf /recall` and `/help tf history`.

## Filtering what you see

`/limit` hides the lines that do not match a pattern, `/unlimit` shows everything again, and `/relimit` applies the limit to new text as well. gtf redraws the window when you change it. `Esc L` toggles the limit from the keyboard. See `/help tf /limit`.

See also: [[Logging]], [[Getting around]].
